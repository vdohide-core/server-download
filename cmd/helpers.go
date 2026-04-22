package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"server-download/internal/db/database"
	"server-download/internal/db/models"
	"server-download/internal/utils"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ─── Error Categorization ────────────────────────────────────

func categorizeError(errMsg string) string {
	e := strings.ToLower(errMsg)
	switch {
	case strings.Contains(e, "failed to download") || strings.Contains(e, "timeout") || strings.Contains(e, "connection"):
		return "network"
	case strings.Contains(e, "codec") || strings.Contains(e, "webp"):
		return "codec"
	case strings.Contains(e, "dts") || strings.Contains(e, "corrupt") || strings.Contains(e, "validation"):
		return "corruption"
	case strings.Contains(e, "merge") || strings.Contains(e, "ffmpeg"):
		return "merge"
	case strings.Contains(e, "upload") || strings.Contains(e, "ssh") || strings.Contains(e, "sftp") || strings.Contains(e, "scp"):
		return "upload"
	case strings.Contains(e, "scraper"):
		return "scraper"
	case strings.Contains(e, "gdrive") || strings.Contains(e, "drive"):
		return "gdrive"
	case strings.Contains(e, "s3") || strings.Contains(e, "ingest"):
		return "ingest"
	default:
		return "unknown"
	}
}

// ─── isCancelled ─────────────────────────────────────────────

func isCancelled(ctx context.Context, processID string) bool {
	var p models.VideoProcess
	err := database.VideoProcess().FindOne(ctx, bson.M{"_id": processID}).Decode(&p)
	return err != nil || derefStr(p.Status) == models.ProcessStatusCancelled
}

// ─── failProcess (reference-compatible) ──────────────────────
// Increments retryCount in DB. Resets file → waiting (not error)
// until retryCount >= 3, then marks file → error and deletes process.

func failProcess(ctx context.Context, processID, fileID, slug, errMsg string) {
	category := categorizeError(errMsg)
	now := time.Now()

	var updated models.VideoProcess
	err := database.VideoProcess().FindOneAndUpdate(ctx,
		bson.M{"_id": processID},
		bson.M{
			"$set": bson.M{
				"status": models.ProcessStatusFailed, "error": errMsg,
				"errorCategory": category, "updatedAt": now,
			},
			"$inc": bson.M{"retryCount": 1},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&updated)

	if err != nil {
		// Process not found — still mark file error
		log.Printf("❌ [%s] Process failed (not found): %s [%s]", slug, errMsg, category)
		database.Files().UpdateOne(ctx, bson.M{"_id": fileID}, bson.M{"$set": bson.M{
			"status": models.FileStatusError, "updatedAt": now,
		}})
		failClonedFiles(ctx, fileID, now)
		return
	}

	rc := 0
	if updated.RetryCount != nil {
		rc = *updated.RetryCount
	}

	if rc >= 3 {
		log.Printf("❌ [%s] Failed (attempt %d/3 → file error): %s [%s]", slug, rc, errMsg, category)
		database.Files().UpdateOne(ctx, bson.M{"_id": fileID}, bson.M{"$set": bson.M{
			"status": models.FileStatusError, "updatedAt": now,
		}})
		database.VideoProcess().DeleteOne(ctx, bson.M{"_id": processID})
		failClonedFiles(ctx, fileID, now)
	} else {
		log.Printf("❌ [%s] Failed (attempt %d/3 → retry): %s [%s]", slug, rc, errMsg, category)
		database.Files().UpdateOne(ctx, bson.M{"_id": fileID}, bson.M{"$set": bson.M{
			"status": models.FileStatusWaiting, "updatedAt": now,
		}})
	}
}

// failClonedFiles marks all cloned files of a source file as error.
func failClonedFiles(ctx context.Context, sourceFileID string, now time.Time) {
	result, _ := database.Files().UpdateMany(ctx, bson.M{
		"clonedFrom":         sourceFileID,
		"type":               models.FileTypeVideo,
		"status":             bson.M{"$in": []string{models.FileStatusWaiting, models.FileStatusProcessing}},
		"metadata.trashedAt": bson.M{"$exists": false},
		"metadata.deletedAt": bson.M{"$exists": false},
	}, bson.M{"$set": bson.M{"status": models.FileStatusError, "updatedAt": now}})
	if result != nil && result.ModifiedCount > 0 {
		log.Printf("📋 Updated %d cloned files → error", result.ModifiedCount)
	}
}

// ─── Progress update helpers ─────────────────────────────────

func updateDownloadProgress(ctx context.Context, processID string, current, total int) {
	pct := float64(current) / float64(total) * 100
	database.VideoProcess().UpdateOne(ctx, bson.M{"_id": processID}, bson.M{"$set": bson.M{
		"timeline.download.status":  models.StepStatusProcessing,
		"timeline.download.percent": pct,
		"timeline.download.current": current,
		"timeline.download.total":   total,
		"overallPercent":            pct * 0.33,
		"updatedAt":                 time.Now(),
	}})
}

func updateUploadProgress(ctx context.Context, processID string, current, total int64) {
	pct := float64(current) / float64(total) * 100
	database.VideoProcess().UpdateOne(ctx, bson.M{"_id": processID}, bson.M{"$set": bson.M{
		"timeline.upload.status":  models.StepStatusProcessing,
		"timeline.upload.percent": pct,
		"overallPercent":          66 + pct*0.34,
		"updatedAt":               time.Now(),
	}})
}

func startStep(ctx context.Context, processID, step string) {
	now := time.Now()
	database.VideoProcess().UpdateOne(ctx, bson.M{"_id": processID}, bson.M{"$set": bson.M{
		fmt.Sprintf("timeline.%s.status", step):    models.StepStatusProcessing,
		fmt.Sprintf("timeline.%s.percent", step):   0,
		fmt.Sprintf("timeline.%s.startedAt", step): now,
		"updatedAt": now,
	}})
}

func completeStep(ctx context.Context, processID, step string) {
	now := time.Now()
	var overall float64
	switch step {
	case "download":
		overall = 33
	case "merge":
		overall = 66
	case "upload":
		overall = 100
	}
	database.VideoProcess().UpdateOne(ctx, bson.M{"_id": processID}, bson.M{"$set": bson.M{
		fmt.Sprintf("timeline.%s.status", step):  models.StepStatusCompleted,
		fmt.Sprintf("timeline.%s.percent", step): 100,
		fmt.Sprintf("timeline.%s.endedAt", step): now,
		"overallPercent": overall, "updatedAt": now,
	}})
}

// ─── Clone media to cloned files ─────────────────────────────

func cloneMediaToClonedFiles(ctx context.Context, sourceFileID string, media models.Media, slug string) {
	cursor, err := database.Files().Find(ctx, bson.M{
		"clonedFrom":         sourceFileID,
		"type":               models.FileTypeVideo,
		"metadata.trashedAt": bson.M{"$exists": false},
		"metadata.deletedAt": bson.M{"$exists": false},
	})
	if err != nil {
		return
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var clonedFile models.File
		if err := cursor.Decode(&clonedFile); err != nil {
			continue
		}

		filter := bson.M{"fileId": clonedFile.ID, "type": media.Type}
		if media.Resolution != nil {
			filter["resolution"] = *media.Resolution
		}
		existCount, _ := database.Medias().CountDocuments(ctx, filter)
		if existCount > 0 {
			continue
		}

		now := time.Now()
		slug11 := utils.RandomString(11, false)
		clonedMedia := models.Media{
			ID:         uuid.New().String(),
			Type:       media.Type,
			FileName:   media.FileName,
			MimeType:   media.MimeType,
			Resolution: media.Resolution,
			StorageID:  media.StorageID,
			Slug:       slug11,
			FileID:     &clonedFile.ID,
			Metadata:   media.Metadata,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		clonedFrom := sourceFileID
		clonedMedia.ClonedFrom = &clonedFrom

		if _, err := database.Medias().InsertOne(ctx, clonedMedia); err != nil {
			log.Printf("⚠️  [%s] Failed to clone media to %s: %v", slug, clonedFile.ID, err)
			continue
		}
		log.Printf("📋 [%s] Cloned media → file %s", slug, clonedFile.ID)
	}
}

// ─── DetermineHighestResolution ───────────────────────────────

// DetermineHighestResolution maps pixel height to the highest standard tier (95% tolerance).
func DetermineHighestResolution(height int) int {
	threshold := func(t int) int { return t * 95 / 100 }
	if height >= threshold(1080) {
		return 1080
	}
	if height >= threshold(720) {
		return 720
	}
	if height >= threshold(480) {
		return 480
	}
	return 360
}
