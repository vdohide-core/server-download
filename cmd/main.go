package main

import (
	"context"
	"log"
	"os"
	"time"

	"server-download/internal/config"
	"server-download/internal/db/database"
	"server-download/internal/db/models"
	"server-download/internal/utils"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var workerID string

func main() {
	config.Load()
	workerID = utils.GenerateWorkerID()
	log.Printf("Starting Server Download [Worker: %s]", workerID)

	if err := database.Connect(); err != nil {
		log.Printf("ERROR: Failed to connect to MongoDB: %v", err)
		time.Sleep(5 * time.Second)
		os.Exit(1)
	}
	defer database.Disconnect()

	startWorkerLoop()
}

// ─── Worker Loop ──────────────────────────────────────────────

func startWorkerLoop() {
	log.Println("⚡ Worker Mode: Polling for jobs...")
	log.Printf("🆔 Worker ID: %s", workerID)

	utils.CleanOldLogs()

	ctx := context.Background()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	total, _ := database.Files().CountDocuments(ctx, bson.M{})
	waiting, _ := database.Files().CountDocuments(ctx, bson.M{
		"status": models.FileStatusWaiting,
		"type":   models.FileTypeVideo,
		"clonedFrom": bson.M{"$exists": false},
	})
	log.Printf("📊 DB Stats: Total Files: %d, Waiting Videos: %d", total, waiting)

	if isDownloadEnabled(ctx) {
		processNextJob(ctx)
	}

	for range ticker.C {
		if !isDownloadEnabled(ctx) {
			continue
		}
		processNextJob(ctx)
	}
}

// ─── Settings ─────────────────────────────────────────────────

func isDownloadEnabled(ctx context.Context) bool {
	var setting models.Setting
	err := database.Settings().FindOne(ctx, bson.M{"name": "download_enabled"}).Decode(&setting)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			now := time.Now()
			database.Settings().InsertOne(ctx, models.Setting{
				Name: "download_enabled", Value: false,
				CreatedAt: now, UpdatedAt: now,
			})
			log.Println("⚙️  Created 'download_enabled' = false")
		}
		return false
	}
	if v, ok := setting.Value.(bool); ok {
		return v
	}
	return false
}

func getScraperURL(ctx context.Context) string {
	if url := config.AppConfig.ScraperURL; url != "" {
		return url
	}
	var setting models.Setting
	if err := database.Settings().FindOne(ctx, bson.M{"name": "url_scraping"}).Decode(&setting); err == nil {
		if v, ok := setting.Value.(string); ok {
			return v
		}
	}
	return ""
}

// ─── Job Discovery ────────────────────────────────────────────

func processNextJob(ctx context.Context) {
	if process := resumeOrClaimProcess(ctx); process != nil {
		slug := ""
		if process.Slug != nil {
			slug = *process.Slug
		}
		if err := runProcess(ctx, process); err != nil {
			log.Printf("❌ Resume failed: %s - %v", slug, err)
		}
		return
	}

	process, file, err := findAndClaimFile(ctx)
	if err == nil && process != nil {
		slug := ""
		if process.Slug != nil {
			slug = *process.Slug
		}
		src := ""
		if file.Metadata != nil {
			if file.Metadata.SourceType != nil {
				src = *file.Metadata.SourceType
			}
		}
		log.Printf("📥 New: [%s] %s (sourceType: %s)", slug, file.Name, src)
		if err := runProcess(ctx, process); err != nil {
			log.Printf("❌ Failed: %s - %v", slug, err)
		}
		return
	}

	if retryProcess := findAndClaimFailedProcess(ctx); retryProcess != nil {
		slug := ""
		if retryProcess.Slug != nil {
			slug = *retryProcess.Slug
		}
		rc := 0
		if retryProcess.RetryCount != nil {
			rc = *retryProcess.RetryCount
		}
		log.Printf("🔄 Retry: [%s] (attempt %d)", slug, rc+1)
		if err := runProcess(ctx, retryProcess); err != nil {
			log.Printf("❌ Retry failed: %s - %v", slug, err)
		}
	}
}

func resumeOrClaimProcess(ctx context.Context) *models.VideoProcess {
	processing := models.ProcessStatusProcessing
	var process models.VideoProcess
	err := database.VideoProcess().FindOne(ctx, bson.M{
		"workerId": workerID,
		"status":   processing,
	}).Decode(&process)
	if err == nil {
		slug := ""
		if process.Slug != nil {
			slug = *process.Slug
		}
		log.Printf("🔄 [%s] Resuming own process", slug)
		return &process
	}
	return nil
}

func findAndClaimFile(ctx context.Context) (*models.VideoProcess, *models.File, error) {
	filter := bson.M{
		"status":     models.FileStatusWaiting,
		"type":       models.FileTypeVideo,
		"clonedFrom": bson.M{"$exists": false},
	}
	opts := options.Find().SetSort(bson.M{"createdAt": 1}).SetLimit(20)

	cursor, err := database.Files().Find(ctx, filter, opts)
	if err != nil {
		return nil, nil, err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var file models.File
		if err := cursor.Decode(&file); err != nil {
			continue
		}

		sourceType := ""
		if file.Metadata != nil && file.Metadata.SourceType != nil {
			sourceType = *file.Metadata.SourceType
		}

		if sourceType == "" {
			ingestCount, _ := database.Ingests().CountDocuments(ctx, bson.M{"fileId": file.ID})
			if ingestCount > 0 {
				st := "upload"
				if file.Metadata == nil {
					file.Metadata = &models.FileMetadata{}
				}
				file.Metadata.SourceType = &st
				sourceType = st
				log.Printf("🔍 [%s] Auto-detected sourceType: upload", file.Slug)
			} else {
				log.Printf("⏭️  [%s] Skip: no sourceType and no ingest", file.Slug)
				continue
			}
		}

		if file.Metadata != nil && file.Metadata.DeletedAt != nil {
			continue
		}

		activeCount, _ := database.VideoProcess().CountDocuments(ctx, bson.M{
			"fileId": file.ID,
			"status": models.ProcessStatusProcessing,
		})
		if activeCount > 0 {
			log.Printf("⏭️  [%s] Skip: already processing", file.Slug)
			continue
		}

		mediaCount, _ := database.Medias().CountDocuments(ctx, bson.M{
			"fileId": file.ID,
			"type":   models.MediaTypeVideo,
		})
		if mediaCount > 0 {
			log.Printf("⏭️  [%s] Skip: already has video media", file.Slug)
			continue
		}

		src := ""
		if file.Metadata != nil && file.Metadata.Source != nil {
			src = *file.Metadata.Source
		}
		log.Printf("✅ [%s] Eligible: sourceType=%s, source=%s", file.Slug, sourceType, src)

		process, err := tryClaimFile(ctx, &file)
		if err != nil {
			log.Printf("⚠️  [%s] Claim failed: %v", file.Slug, err)
			continue
		}
		return process, &file, nil
	}

	return nil, nil, nil
}

func tryClaimFile(ctx context.Context, file *models.File) (*models.VideoProcess, error) {
	now := time.Now()

	existing, _ := database.VideoProcess().CountDocuments(ctx, bson.M{
		"fileId": file.ID, "status": models.ProcessStatusProcessing,
	})
	if existing > 0 {
		return nil, nil
	}

	var existingProcess models.VideoProcess
	err := database.VideoProcess().FindOne(ctx, bson.M{
		"fileId": file.ID, "status": models.ProcessStatusFailed,
	}).Decode(&existingProcess)
	if err == nil && existingProcess.RetryCount != nil && *existingProcess.RetryCount >= 3 {
		database.Files().UpdateOne(ctx, bson.M{"_id": file.ID}, bson.M{"$set": bson.M{
			"status": models.FileStatusError, "updatedAt": now,
		}})
		return nil, nil
	}

	processing := models.ProcessStatusProcessing
	pending := models.StepStatusPending
	zero := 0.0

	var reusedProcess models.VideoProcess
	err = database.VideoProcess().FindOneAndUpdate(ctx,
		bson.M{"fileId": file.ID, "status": models.ProcessStatusFailed},
		bson.M{"$set": bson.M{
			"workerId": workerID, "status": processing,
			"error": "", "errorCategory": "",
			"timeline": bson.M{
				"download": bson.M{"status": pending},
				"merge":    bson.M{"status": pending},
				"upload":   bson.M{"status": pending},
			},
			"overallPercent": zero, "updatedAt": now,
		}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&reusedProcess)
	if err == nil {
		rc := 0
		if reusedProcess.RetryCount != nil {
			rc = *reusedProcess.RetryCount
		}
		log.Printf("🔄 [%s] Reusing failed process (retry %d/3)", file.Slug, rc+1)
		database.Files().UpdateOne(ctx,
			bson.M{"_id": file.ID},
			bson.M{"$set": bson.M{"status": models.FileStatusProcessing, "updatedAt": now}},
		)
		return &reusedProcess, nil
	}

	sourceType := ""
	if file.Metadata != nil && file.Metadata.SourceType != nil {
		sourceType = *file.Metadata.SourceType
	}
	process := &models.VideoProcess{
		ID:         newUUID(),
		FileID:     &file.ID,
		Slug:       &file.Slug,
		WorkerID:   &workerID,
		Status:     &processing,
		SourceType: &sourceType,
		SpaceID:    file.SpaceID,
		Timeline: bson.M{
			"download": bson.M{"status": pending},
			"merge":    bson.M{"status": pending},
			"upload":   bson.M{"status": pending},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	database.VideoProcess().DeleteMany(ctx, bson.M{
		"fileId": file.ID, "status": models.ProcessStatusCancelled,
	})

	if _, err = database.VideoProcess().InsertOne(ctx, process); err != nil {
		return nil, err
	}

	database.Files().UpdateOne(ctx,
		bson.M{"_id": file.ID},
		bson.M{"$set": bson.M{"status": models.FileStatusProcessing, "updatedAt": now}},
	)

	return process, nil
}

func findAndClaimFailedProcess(ctx context.Context) *models.VideoProcess {
	filter := bson.M{
		"status": models.ProcessStatusFailed,
		"$or": []bson.M{
			{"retryCount": bson.M{"$exists": false}},
			{"retryCount": bson.M{"$lt": 3}},
		},
	}
	opts := options.FindOneAndUpdate().
		SetSort(bson.M{"createdAt": 1}).
		SetReturnDocument(options.After)

	processing := models.ProcessStatusProcessing
	var process models.VideoProcess
	err := database.VideoProcess().FindOneAndUpdate(ctx, filter,
		bson.M{
			"$set": bson.M{"status": processing, "workerId": workerID, "updatedAt": time.Now()},
			"$inc": bson.M{"retryCount": 1},
		}, opts,
	).Decode(&process)
	if err != nil {
		return nil
	}

	fileID := ""
	if process.FileID != nil {
		fileID = *process.FileID
	}
	mediaCount, _ := database.Medias().CountDocuments(ctx, bson.M{
		"fileId": fileID, "type": models.MediaTypeVideo,
	})
	if mediaCount > 0 {
		slug := ""
		if process.Slug != nil {
			slug = *process.Slug
		}
		log.Printf("🧹 [%s] File already has media — removing stale process", slug)
		database.VideoProcess().DeleteOne(ctx, bson.M{"_id": process.ID})
		return nil
	}

	return &process
}
