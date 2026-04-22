package goose

import (
	"context"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// EnsureIndexes reads goose struct tags (unique, index) and creates
// MongoDB indexes automatically. Call once at startup after Connect().
//
// Equivalent to Mongoose autoIndex: true
//
// Example:
//
//	FileModel.EnsureIndexes(ctx)
//	// Creates:
//	//   { slug: 1 } unique
//	//   { ownerId: 1 }
//	//   { parentId: 1 }
func (m *Model[T]) EnsureIndexes(ctx context.Context) error {
	schemas := GetSchema[T]()
	var indexes []mongo.IndexModel

	for _, s := range schemas {
		if s.BsonName == "_id" {
			continue // MongoDB auto-creates _id index
		}

		if s.Unique {
			indexes = append(indexes, mongo.IndexModel{
				Keys: bson.D{{Key: s.BsonName, Value: 1}},
				Options: options.Index().
					SetUnique(true).
					SetBackground(true).
					SetSparse(true), // sparse: don't index null/missing fields
			})
		} else if s.Index {
			indexes = append(indexes, mongo.IndexModel{
				Keys: bson.D{{Key: s.BsonName, Value: 1}},
				Options: options.Index().
					SetBackground(true),
			})
		}
	}

	if len(indexes) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, err := m.Col().Indexes().CreateMany(ctx, indexes)
	if err != nil {
		log.Printf("⚠️ goose: failed to create indexes for %s: %v", m.collName, err)
		return err
	}

	log.Printf("✅ goose: ensured %d indexes for %s", len(indexes), m.collName)
	return nil
}

// EnsureCompoundIndex creates a compound index on multiple fields.
//
// Example:
//
//	FileModel.EnsureCompoundIndex(ctx, []string{"ownerId", "parentId"}, false)
//	FileModel.EnsureCompoundIndex(ctx, []string{"hash", "ownerId"}, true) // unique compound
func (m *Model[T]) EnsureCompoundIndex(ctx context.Context, fields []string, unique bool) error {
	keys := bson.D{}
	for _, f := range fields {
		keys = append(keys, bson.E{Key: f, Value: 1})
	}

	opts := options.Index().SetBackground(true)
	if unique {
		opts.SetUnique(true)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	_, err := m.Col().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    keys,
		Options: opts,
	})
	if err != nil {
		log.Printf("⚠️ goose: failed to create compound index for %s: %v", m.collName, err)
		return err
	}
	return nil
}
