package goose

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// FindOne finds a single document matching the filter.
// Equivalent to: await MediaModel.findOne({ _id: id })
func (m *Model[T]) FindOne(ctx context.Context, filter interface{}, opts ...*options.FindOneOptions) (*T, error) {
	doc := new(T)
	err := m.Col().FindOne(ctx, filter, opts...).Decode(doc)
	if err != nil {
		return nil, err
	}
	return doc, nil
}

// FindByID finds a document by its _id.
// Equivalent to: await MediaModel.findById(id)
func (m *Model[T]) FindByID(ctx context.Context, id string) (*T, error) {
	return m.FindOne(ctx, bson.M{"_id": id})
}

// FindBySlug finds a document by its slug.
func (m *Model[T]) FindBySlug(ctx context.Context, slug string) (*T, error) {
	return m.FindOne(ctx, bson.M{"slug": slug})
}

// Find returns all documents matching the filter.
// Equivalent to: await MediaModel.find({ fileId: id })
func (m *Model[T]) Find(ctx context.Context, filter interface{}, opts ...*options.FindOptions) ([]*T, error) {
	cursor, err := m.Col().Find(ctx, filter, opts...)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []*T
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// FindRaw returns a raw mongo cursor for advanced use (aggregations, etc).
func (m *Model[T]) FindRaw(ctx context.Context, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error) {
	return m.Col().Find(ctx, filter, opts...)
}
