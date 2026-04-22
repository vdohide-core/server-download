package goose

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"
)

// Create inserts a new document, auto-setting defaults from goose tags + BaseModel.
// Equivalent to: await MediaModel.create({...})
func (m *Model[T]) Create(ctx context.Context, doc *T) (*mongo.InsertOneResult, error) {
	// Apply goose struct tag defaults
	applyDefaults(doc)
	// Also init BaseModel if embedded
	if base := getBaseModel(doc); base != nil {
		base.InitDefaults()
	}
	return m.Col().InsertOne(ctx, doc)
}

// Save is an alias for Create (insert only).
// Equivalent to: await doc.save()
func (m *Model[T]) Save(ctx context.Context, doc *T) (*mongo.InsertOneResult, error) {
	return m.Create(ctx, doc)
}

// InsertMany inserts multiple documents, auto-setting defaults.
// Equivalent to: await MediaModel.insertMany([...])
func (m *Model[T]) InsertMany(ctx context.Context, docs []*T) (*mongo.InsertManyResult, error) {
	iDocs := make([]interface{}, len(docs))
	for i, doc := range docs {
		applyDefaults(doc)
		if base := getBaseModel(doc); base != nil {
			base.InitDefaults()
		}
		iDocs[i] = doc
	}
	return m.Col().InsertMany(ctx, iDocs)
}
