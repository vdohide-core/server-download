package database

import (
	"context"
	"log"
	"time"

	"server-download/internal/config"
	"server-download/internal/lib/goose"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Connect establishes a connection to MongoDB via goose ODM.
func Connect() error {
	uri := config.AppConfig.MongoURI
	if err := goose.Connect(uri); err != nil {
		return err
	}
	EnsureIndexes()
	return nil
}

// Disconnect closes the MongoDB connection.
func Disconnect() {
	if goose.Client() != nil {
		if err := goose.Close(); err != nil {
			log.Printf("⚠️ Error disconnecting from MongoDB: %v", err)
		} else {
			log.Println("🔌 Disconnected from MongoDB")
		}
	}
}

// DB returns the database instance (delegates to goose).
func DB() *mongo.Database {
	return goose.DB()
}

// Collection returns a collection by name (delegates to goose).
func Collection(name string) *mongo.Collection {
	return goose.Collection(name)
}

// ─── Collection Accessors ─────────────────────────────────────

func Files() *mongo.Collection        { return goose.Collection("files") }
func Medias() *mongo.Collection       { return goose.Collection("medias") }
func Storages() *mongo.Collection     { return goose.Collection("storages") }
func Ingests() *mongo.Collection      { return goose.Collection("ingests") }
func Settings() *mongo.Collection     { return goose.Collection("settings") }
func VideoProcess() *mongo.Collection { return goose.Collection("video_process") }
func Oauths() *mongo.Collection       { return goose.Collection("oauths") }

// ─── Indexes ──────────────────────────────────────────────────

// EnsureIndexes creates required indexes for concurrency safety.
func EnsureIndexes() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Drop stale indexes
	VideoProcess().Indexes().DropOne(ctx, "postId_1")
	VideoProcess().Indexes().DropOne(ctx, "fileId_1")

	// Clean up duplicate fileId records before creating unique index
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$fileId"},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "ids", Value: bson.D{{Key: "$push", Value: "$_id"}}},
		}}},
		{{Key: "$match", Value: bson.D{{Key: "count", Value: bson.D{{Key: "$gt", Value: 1}}}}}},
	}
	cursor, err := VideoProcess().Aggregate(ctx, pipeline)
	if err == nil {
		type DupResult struct {
			FileID string   `bson:"_id"`
			Count  int      `bson:"count"`
			IDs    []string `bson:"ids"`
		}
		for cursor.Next(ctx) {
			var dup DupResult
			if cursor.Decode(&dup) == nil && len(dup.IDs) > 1 {
				deleteIDs := dup.IDs[1:]
				VideoProcess().DeleteMany(ctx, bson.M{"_id": bson.M{"$in": deleteIDs}})
				log.Printf("🧹 Removed %d duplicate video_process for fileId %s", len(deleteIDs), dup.FileID)
			}
		}
		cursor.Close(ctx)
	}

	// Unique index on video_process.fileId
	_, err = VideoProcess().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "fileId", Value: 1}},
		Options: options.Index().SetUnique(true).SetSparse(true),
	})
	if err != nil {
		log.Printf("⚠️  Index creation warning: %v", err)
	} else {
		log.Printf("✅ Unique index on video_process.fileId ensured")
	}
}
