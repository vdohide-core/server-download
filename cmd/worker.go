package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"server-download/internal/config"
	"server-download/internal/db/database"
	"server-download/internal/db/models"
	"server-download/internal/downloader"
	"server-download/internal/uploader"
	"server-download/internal/utils"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func newUUID() string { return uuid.New().String() }

// ─── Storage Resolution ───────────────────────────────────────

func resolveStorage(ctx context.Context) (*models.Storage, error) {
	localPath := config.AppConfig.StoragePath
	localID := config.AppConfig.StorageId

	if localPath != "" && localID != "" {
		var dbStorage models.Storage
		err := database.Storages().FindOne(ctx, bson.M{"_id": localID}).Decode(&dbStorage)
		if err == nil {
			if !dbStorage.Enable {
				return nil, fmt.Errorf("local storage %s is disabled", localID)
			}
		}
		return &models.Storage{ID: localID}, nil
	}

	// Find storage with SSH credentials
	filter := bson.M{
		"enable":              true,
		"status":              "online",
		"type":                "local",
		"local.ssh.username":  bson.M{"$exists": true, "$ne": ""},
		"local.ssh.password":  bson.M{"$exists": true, "$ne": ""},
		"local.ssh.port":      bson.M{"$gt": 0},
		"capacity.percentage": bson.M{"$lt": 95},
		"accepts":             bson.M{"$in": []string{"video"}},
	}
	var storage models.Storage
	err := database.Storages().FindOne(ctx, filter, options.FindOne().SetSort(bson.M{"capacity.percentage": 1})).Decode(&storage)
	if err != nil {
		return nil, fmt.Errorf("no storage with SSH credentials available")
	}
	return &storage, nil
}

// ─── Main Process ─────────────────────────────────────────────

func runProcess(ctx context.Context, process *models.VideoProcess, storage *models.Storage) error {
	fileID := derefStr(process.FileID)
	slug := derefStr(process.Slug)

	// Use exe dir for download temp files
	exePath, _ := os.Executable()
	baseDir := filepath.Dir(exePath)
	if strings.Contains(exePath, "go-build") {
		baseDir, _ = os.Getwd()
	}
	downloadDir := filepath.Join(baseDir, "download", slug)

	processLogger := utils.NewProcessLogger(slug)
	defer processLogger.Close()

	var file models.File
	if err := database.Files().FindOne(ctx, bson.M{"_id": fileID}).Decode(&file); err != nil {
		log.Printf("🧹 [%s] File not found — removing orphaned process", slug)
		database.VideoProcess().DeleteOne(ctx, bson.M{"_id": process.ID})
		return fmt.Errorf("file not found: %w", err)
	}

	// sourceType: from process record first, then file metadata, then auto-detect
	sourceType := derefStr(process.SourceType)
	if sourceType == "" && file.Metadata != nil && file.Metadata.SourceType != nil {
		sourceType = *file.Metadata.SourceType
	}
	if sourceType == "" {
		ingestCount, _ := database.Ingests().CountDocuments(ctx, bson.M{"fileId": file.ID})
		if ingestCount > 0 {
			sourceType = "upload"
		}
	}

	log.Printf("📋 [%s] Source type: %s", slug, sourceType)

	fileName := models.FileNameOriginal
	resolution := models.ResolutionOriginal

	var mp4Path string
	var fileSize int64
	isDirectMP4 := false

	// ─── STEP 1: DOWNLOAD ─────────────────────────────────────

	switch sourceType {
	case "upload":
		isDirectMP4 = true
		startStep(ctx, process.ID, "download")
		os.MkdirAll(downloadDir, 0755)
		mp4Path = filepath.Join(downloadDir, "source.mp4")

		// Find ingest record
		var ingest models.Ingest
		if err := database.Ingests().FindOne(ctx, bson.M{"fileId": file.ID}).Decode(&ingest); err != nil {
			failProcess(ctx, process.ID, fileID, slug, "ingest record not found")
			return fmt.Errorf("ingest not found")
		}

		ingestPathVal := derefStr(ingest.Path)
		if ingestPathVal == "" {
			failProcess(ctx, process.ID, fileID, slug, "ingest has no path")
			return fmt.Errorf("ingest has no path")
		}

		// Find ingest storage
		var ingestStorage models.Storage
		storageLoaded := false
		if ingest.StorageID != nil && *ingest.StorageID != "" {
			if err := database.Storages().FindOne(ctx, bson.M{"_id": *ingest.StorageID}).Decode(&ingestStorage); err == nil {
				storageLoaded = true
			}
		}

		if storageLoaded && ingestStorage.Type == "s3" {
			// Download from S3
			if err := downloader.DownloadFromS3(&ingestStorage, ingestPathVal, mp4Path, func(done, total int64) {
				if total > 0 {
					pct := float64(done) / float64(total) * 100
					database.VideoProcess().UpdateOne(ctx, bson.M{"_id": process.ID}, bson.M{"$set": bson.M{
						"timeline.download.percent": pct, "overallPercent": pct * 0.33, "updatedAt": time.Now(),
					}})
				}
			}); err != nil {
				failProcess(ctx, process.ID, fileID, slug, fmt.Sprintf("S3 download: %v", err))
				return err
			}
		} else {
			// Local filesystem
			basePath := ""
			if storageLoaded && ingestStorage.Local != nil && ingestStorage.Local.Path != "" {
				basePath = ingestStorage.Local.Path
			} else {
				basePath = config.AppConfig.StoragePath
			}

			localSrc := ingestPathVal
			if basePath != "" {
				localSrc = filepath.Join(basePath, ingestPathVal)
			}
			log.Printf("📂 [%s] Local ingest file: %s", slug, localSrc)
			if _, err := os.Stat(localSrc); err != nil {
				failProcess(ctx, process.ID, fileID, slug, fmt.Sprintf("ingest file not found: %s", localSrc))
				return fmt.Errorf("ingest file not found")
			}
			// Copy to download dir so we can work on it
			if err := copyFileLocal(localSrc, mp4Path); err != nil {
				failProcess(ctx, process.ID, fileID, slug, fmt.Sprintf("copy ingest: %v", err))
				return err
			}
		}

	case "gdrive":
		isDirectMP4 = true
		startStep(ctx, process.ID, "download")
		os.MkdirAll(downloadDir, 0755)
		mp4Path = filepath.Join(downloadDir, "source.mp4")

		source := ""
		if file.Metadata != nil && file.Metadata.Source != nil {
			source = *file.Metadata.Source
		}
		if source == "" {
			failProcess(ctx, process.ID, fileID, slug, "no Google Drive file ID")
			return fmt.Errorf("no gdrive source")
		}

		if err := downloader.DownloadFromGDrive(source, mp4Path, database.Oauths(), func(done, total int64) {
			if total > 0 {
				pct := float64(done) / float64(total) * 100
				database.VideoProcess().UpdateOne(ctx, bson.M{"_id": process.ID}, bson.M{"$set": bson.M{
					"timeline.download.percent": pct, "overallPercent": pct * 0.33, "updatedAt": time.Now(),
				}})
			}
		}); err != nil {
			failProcess(ctx, process.ID, fileID, slug, fmt.Sprintf("GDrive download: %v", err))
			return err
		}

	case "direct":
		source := ""
		if file.Metadata != nil && file.Metadata.Source != nil {
			source = *file.Metadata.Source
		}
		if source == "" {
			failProcess(ctx, process.ID, fileID, slug, "no direct URL source")
			return fmt.Errorf("no direct URL source")
		}

		startStep(ctx, process.ID, "download")
		os.MkdirAll(downloadDir, 0755)

		// ถ้าเป็น m3u8 → ใช้ HLS flow เหมือน default (fall-through ด้วย goto ไม่ได้ใน Go)
		// จึงแยก branch ชัดเจน
		if !downloader.IsDirectVideoURL(source) {
			// HLS / m3u8 path
			result, err := downloader.DownloadHLSSegments(source, downloadDir, &downloader.DownloadProgress{
				OnProgress: func(current, total int) {
					updateDownloadProgress(ctx, process.ID, current, total)
				},
			})
			if err != nil {
				failProcess(ctx, process.ID, fileID, slug, fmt.Sprintf("download: %v", err))
				return err
			}
			database.VideoProcess().UpdateOne(ctx, bson.M{"_id": process.ID}, bson.M{"$set": bson.M{"resolution": result.ResolutionFull}})
			completeStep(ctx, process.ID, "download")
			log.Printf("✅ [%s] HLS download complete (%d segments)", slug, result.SegmentCount)

			if isCancelled(ctx, process.ID) {
				downloader.Cleanup(downloadDir)
				return nil
			}

			log.Printf("🔒 [%s] Waiting for processing lock...", slug)
			procLock := utils.AcquireProcessingLock("processing")
			defer procLock.Release()

			startStep(ctx, process.ID, "merge")
			mp4Path = filepath.Join(downloadDir, fileName)
			mergeRes, err := downloader.MergeToMP4(result.SegmentFiles, mp4Path, func(pct int) {
				database.VideoProcess().UpdateOne(ctx, bson.M{"_id": process.ID}, bson.M{"$set": bson.M{
					"timeline.merge.percent": float64(pct), "overallPercent": 33 + float64(pct)*0.33, "updatedAt": time.Now(),
				}})
			})
			if err != nil {
				if downloader.IsDiskFullError(err) {
					database.VideoProcess().DeleteOne(ctx, bson.M{"_id": process.ID})
					database.Files().UpdateOne(ctx, bson.M{"_id": fileID}, bson.M{"$set": bson.M{"status": models.FileStatusWaiting, "updatedAt": time.Now()}})
					downloader.Cleanup(downloadDir)
					return fmt.Errorf("disk full: %w", err)
				}
				downloader.Cleanup(downloadDir)
				failProcess(ctx, process.ID, fileID, slug, fmt.Sprintf("merge: %v", err))
				return err
			}
			completeStep(ctx, process.ID, "merge")
			fileSize = mergeRes.FileSize
			log.Printf("✅ [%s] Merge complete (%.2f MB)", slug, float64(fileSize)/1024/1024)
		} else {
			// Direct video file path (mp4, mkv, webm, etc.)
			isDirectMP4 = true
			mp4Path = filepath.Join(downloadDir, "source.mp4")

			if err := downloader.DownloadDirectFile(source, mp4Path, func(done, total int64) {
				if total > 0 {
					pct := float64(done) / float64(total) * 100
					database.VideoProcess().UpdateOne(ctx, bson.M{"_id": process.ID}, bson.M{"$set": bson.M{
						"timeline.download.percent": pct, "overallPercent": pct * 0.33, "updatedAt": time.Now(),
					}})
				}
			}); err != nil {
				failProcess(ctx, process.ID, fileID, slug, fmt.Sprintf("direct download: %v", err))
				return err
			}
		}

	default: // HLS / remote / missav / xvideos / pornhub
		m3u8URL := ""
		if file.Metadata != nil && file.Metadata.Playlist != nil {
			m3u8URL = *file.Metadata.Playlist
		}
		if m3u8URL == "" {
			source := ""
			if file.Metadata != nil && file.Metadata.Source != nil {
				source = *file.Metadata.Source
			}
			if source == "" {
				failProcess(ctx, process.ID, fileID, slug, "no m3u8 URL or source")
				return fmt.Errorf("no m3u8 URL")
			}
			scraperURL := getScraperURL(ctx)
			if scraperURL == "" {
				failProcess(ctx, process.ID, fileID, slug, "no scraper URL configured")
				return fmt.Errorf("no scraper URL")
			}
			var err error
			m3u8URL, _, err = downloader.FetchM3U8FromScraper(scraperURL, source)
			if err != nil {
				failProcess(ctx, process.ID, fileID, slug, fmt.Sprintf("scraper: %v", err))
				return err
			}
		}

		startStep(ctx, process.ID, "download")
		os.MkdirAll(downloadDir, 0755)

		result, err := downloader.DownloadHLSSegments(m3u8URL, downloadDir, &downloader.DownloadProgress{
			OnProgress: func(current, total int) {
				updateDownloadProgress(ctx, process.ID, current, total)
			},
		})
		if err != nil {
			failProcess(ctx, process.ID, fileID, slug, fmt.Sprintf("download: %v", err))
			return err
		}
		database.VideoProcess().UpdateOne(ctx, bson.M{"_id": process.ID}, bson.M{"$set": bson.M{"resolution": result.ResolutionFull}})
		completeStep(ctx, process.ID, "download")
		log.Printf("✅ [%s] Download complete (%d segments)", slug, result.SegmentCount)

		if isCancelled(ctx, process.ID) {
			downloader.Cleanup(downloadDir)
			return nil
		}

		// MERGE
		log.Printf("🔒 [%s] Waiting for processing lock...", slug)
		procLock := utils.AcquireProcessingLock("processing")
		defer procLock.Release()

		startStep(ctx, process.ID, "merge")
		mp4Path = filepath.Join(downloadDir, fileName)
		mergeRes, err := downloader.MergeToMP4(result.SegmentFiles, mp4Path, func(pct int) {
			database.VideoProcess().UpdateOne(ctx, bson.M{"_id": process.ID}, bson.M{"$set": bson.M{
				"timeline.merge.percent": float64(pct), "overallPercent": 33 + float64(pct)*0.33, "updatedAt": time.Now(),
			}})
		})
		if err != nil {
			if downloader.IsDiskFullError(err) {
				database.VideoProcess().DeleteOne(ctx, bson.M{"_id": process.ID})
				database.Files().UpdateOne(ctx, bson.M{"_id": fileID}, bson.M{"$set": bson.M{"status": models.FileStatusWaiting, "updatedAt": time.Now()}})
				downloader.Cleanup(downloadDir)
				return fmt.Errorf("disk full: %w", err)
			}
			downloader.Cleanup(downloadDir)
			failProcess(ctx, process.ID, fileID, slug, fmt.Sprintf("merge: %v", err))
			return err
		}
		completeStep(ctx, process.ID, "merge")
		fileSize = mergeRes.FileSize
		log.Printf("✅ [%s] Merge complete (%.2f MB)", slug, float64(fileSize)/1024/1024)
	}

	// For direct MP4: ensure h264 + faststart
	if isDirectMP4 {
		log.Printf("🔒 [%s] Waiting for processing lock...", slug)
		procLock := utils.AcquireProcessingLock("processing")
		defer procLock.Release()

		startStep(ctx, process.ID, "merge")
		faststartPath := filepath.Join(downloadDir, fileName)
		if err := downloader.EnsureH264Faststart(mp4Path, faststartPath, func(pct int) {
			database.VideoProcess().UpdateOne(ctx, bson.M{"_id": process.ID}, bson.M{"$set": bson.M{
				"timeline.merge.percent": float64(pct), "overallPercent": 33 + float64(pct)*0.33, "updatedAt": time.Now(),
			}})
		}); err != nil {
			log.Printf("⚠️ [%s] H264 faststart failed: %v — using original", slug, err)
			if mp4Path != faststartPath {
				os.Rename(mp4Path, faststartPath)
			}
		} else {
			if mp4Path != faststartPath {
				os.Remove(mp4Path)
			}
		}
		mp4Path = faststartPath
		if info, err := os.Stat(mp4Path); err == nil {
			fileSize = info.Size()
		}
		completeStep(ctx, process.ID, "download")
		completeStep(ctx, process.ID, "merge")
	}

	if isCancelled(ctx, process.ID) {
		downloader.Cleanup(downloadDir)
		return nil
	}

	// ─── Probe video info ──────────────────────────────────────
	var videoWidth, videoHeight, videoDuration int64
	if vi, err := downloader.ProbeVideoInfo(mp4Path); err == nil {
		videoWidth, videoHeight, videoDuration = vi.Width, vi.Height, vi.Duration
		log.Printf("📐 [%s] Probed: %dx%d, dur=%ds", slug, videoWidth, videoHeight, videoDuration)
	}

	// ─── STEP 3: UPLOAD ───────────────────────────────────────
	localStoragePath := config.AppConfig.StoragePath
	localStorageID := config.AppConfig.StorageId

	startStep(ctx, process.ID, "upload")

	if localStoragePath != "" && localStorageID != "" {
		log.Printf("📤 [%s] Moving to local storage...", slug)
		if _, err := uploader.MoveFilesLocal(localStoragePath, file.ID, mp4Path, fileName, &uploader.LocalUploadProgress{
			OnProgress: func(done, total int64) { updateUploadProgress(ctx, process.ID, done, total) },
		}); err != nil {
			failProcess(ctx, process.ID, fileID, slug, fmt.Sprintf("local move: %v", err))
			return err
		}
		storage = &models.Storage{ID: localStorageID}
	} else if storage.HasSSHCredentials() {
		// Upload via Node.js SCP script (reference implementation)
		log.Printf("📤 [%s] Uploading via SCP to %s...", slug, storage.Name)
		scpConfig := downloader.SCPConfig{
			LocalPath:  mp4Path,
			Host:       storage.GetHost(),
			Port:       storage.Local.SSH.Port,
			Username:   storage.Local.SSH.Username,
			Password:   storage.Local.SSH.Password,
			RemotePath: fmt.Sprintf("%s/%s", storage.GetPath(), file.ID),
			FileName:   fileName,
		}
		if scpConfig.Port == 0 {
			scpConfig.Port = 22
		}
		if err := downloader.UploadViaSCP(scpConfig, func(p downloader.SCPProgress) {
			if p.Type == "progress" && p.Total > 0 {
				updateUploadProgress(ctx, process.ID, p.Transferred, p.Total)
			}
		}); err != nil {
			failProcess(ctx, process.ID, fileID, slug, fmt.Sprintf("SCP upload: %v", err))
			return err
		}
		log.Printf("✅ [%s] SCP upload complete", slug)
	} else {
		log.Printf("⚠️  [%s] No upload target — skipping upload (file at %s)", slug, mp4Path)
	}

	completeStep(ctx, process.ID, "upload")
	log.Printf("✅ [%s] Upload complete", slug)

	// ─── COMPLETE ─────────────────────────────────────────────
	now := time.Now()
	mediaSlug := utils.RandomString(11, true)
	mediaType := models.MediaTypeVideo
	mimeType := "video/mp4"
	resPtr := &resolution

	media := models.Media{
		ID:         newUUID(),
		Type:       mediaType,
		FileName:   &fileName,
		MimeType:   &mimeType,
		Resolution: resPtr,
		StorageID:  &storage.ID,
		Slug:       mediaSlug,
		FileID:     &file.ID,
		Metadata: &models.MediaMetadata{
			Size:     fileSize,
			Width:    int(videoWidth),
			Height:   int(videoHeight),
			Duration: float64(videoDuration),
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	database.Medias().InsertOne(ctx, media)
	log.Printf("✅ [%s] Created media record", slug)

	cloneMediaToClonedFiles(ctx, file.ID, media, slug)

	// Update file metadata
	updateFields := bson.M{"status": models.FileStatusReady, "updatedAt": now}
	shortSide := videoHeight
	if videoWidth > 0 && videoWidth < videoHeight {
		shortSide = videoWidth
	}
	if shortSide > 0 {
		updateFields["metadata.highest"] = DetermineHighestResolution(int(shortSide))
	}
	if videoDuration > 0 {
		updateFields["metadata.duration"] = videoDuration
	}
	if fileSize > 0 {
		updateFields["metadata.size"] = fileSize
	}
	database.Files().UpdateOne(ctx, bson.M{"_id": file.ID}, bson.M{"$set": updateFields})

	// Update cloned files → ready
	cloneUpdate := bson.M{"status": models.FileStatusReady, "updatedAt": now}
	if shortSide > 0 {
		cloneUpdate["metadata.highest"] = int(shortSide)
	}
	if videoDuration > 0 {
		cloneUpdate["metadata.duration"] = videoDuration
	}
	if fileSize > 0 {
		cloneUpdate["metadata.size"] = fileSize
	}
	cloneResult, _ := database.Files().UpdateMany(ctx, bson.M{
		"clonedFrom":         file.ID,
		"type":               models.FileTypeVideo,
		"status":             bson.M{"$in": []string{models.FileStatusWaiting, models.FileStatusProcessing, models.FileStatusError}},
		"metadata.trashedAt": bson.M{"$exists": false},
		"metadata.deletedAt": bson.M{"$exists": false},
	}, bson.M{"$set": cloneUpdate})
	if cloneResult != nil && cloneResult.ModifiedCount > 0 {
		log.Printf("📋 [%s] Updated %d cloned files → ready", slug, cloneResult.ModifiedCount)
	}

	// Cleanup S3 ingest if upload sourceType
	if sourceType == "upload" {
		var ingest models.Ingest
		if err := database.Ingests().FindOne(ctx, bson.M{"fileId": file.ID}).Decode(&ingest); err == nil {
			if ingest.StorageID != nil {
				var iStor models.Storage
				if err := database.Storages().FindOne(ctx, bson.M{"_id": *ingest.StorageID}).Decode(&iStor); err == nil && iStor.Type == "s3" {
					if delErr := downloader.DeleteFromS3(&iStor, derefStr(ingest.Path)); delErr != nil {
						log.Printf("⚠️  [%s] Failed to delete S3 ingest: %v", slug, delErr)
					} else {
						log.Printf("🗑️  [%s] Deleted S3 ingest: %s", slug, derefStr(ingest.Path))
					}
				}
			}
			database.Ingests().DeleteOne(ctx, bson.M{"_id": ingest.ID})
		}
	}

	// Cleanup
	database.VideoProcess().DeleteOne(ctx, bson.M{"_id": process.ID})
	downloader.Cleanup(downloadDir)

	log.Printf("✅ [%s] COMPLETE!", slug)
	return nil
}

// copyFileLocal copies src to dst using OS copy
func copyFileLocal(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = out.ReadFrom(in)
	return err
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
