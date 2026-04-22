// Package goose provides a Mongoose-like ODM layer for Go + MongoDB.
//
// Usage:
//
//	// Connect
//	goose.Connect(mongoURI)
//
//	// Define model
//	type Media struct {
//	    goose.BaseModel `bson:",inline"`
//	    Type   string   `bson:"type" json:"type"`
//	    FileID string   `bson:"fileId" json:"fileId"`
//	}
//	var MediaModel = goose.NewModel[Media]("medias")
//
//	// Create
//	media := MediaModel.New()
//	media.Type = "video"
//	MediaModel.Create(ctx, media)
//
//	// Find
//	result, _ := MediaModel.FindOne(ctx, bson.M{"_id": id})
//	results, _ := MediaModel.Find(ctx, bson.M{"fileId": id})
//
//	// Update (auto updatedAt)
//	MediaModel.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"type": "audio"})
//
//	// Delete
//	MediaModel.DeleteOne(ctx, bson.M{"_id": id})
package goose

import (
	"context"
	"log"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	client *mongo.Client
	db     *mongo.Database
)

// Connect establishes a MongoDB connection and stores the DB reference.
func Connect(uri string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return err
	}
	if err := c.Ping(ctx, nil); err != nil {
		return err
	}

	client = c
	dbName := parseDBName(uri)
	db = client.Database(dbName)
	log.Printf("✅ goose: connected to MongoDB: %s", dbName)
	return nil
}

// SetDB sets the database instance directly (for existing connections).
func SetDB(database *mongo.Database) {
	db = database
}

// Client returns the underlying mongo.Client.
func Client() *mongo.Client {
	return client
}

// DB returns the current database instance.
func DB() *mongo.Database {
	return db
}

// Collection returns a raw mongo.Collection by name.
func Collection(name string) *mongo.Collection {
	return db.Collection(name)
}

// Close disconnects from MongoDB.
func Close() error {
	if client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return client.Disconnect(ctx)
	}
	return nil
}

// parseDBName extracts DB name from mongodb URI.
func parseDBName(uri string) string {
	// Remove query params
	idx := strings.Index(uri, "?")
	if idx > 0 {
		uri = uri[:idx]
	}
	// Find last /
	lastSlash := strings.LastIndex(uri, "/")
	if lastSlash < 0 || lastSlash == len(uri)-1 {
		return "test"
	}
	name := uri[lastSlash+1:]
	if name == "" {
		return "test"
	}
	return name
}
