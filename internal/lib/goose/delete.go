package goose

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// DeleteOne deletes a single document.
// Equivalent to: await MediaModel.deleteOne(filter)
func (m *Model[T]) DeleteOne(ctx context.Context, filter interface{}) (*mongo.DeleteResult, error) {
	return m.Col().DeleteOne(ctx, filter)
}

// DeleteByID deletes a document by _id.
func (m *Model[T]) DeleteByID(ctx context.Context, id string) (*mongo.DeleteResult, error) {
	return m.DeleteOne(ctx, bson.M{"_id": id})
}

// DeleteMany deletes multiple documents.
// Equivalent to: await MediaModel.deleteMany(filter)
func (m *Model[T]) DeleteMany(ctx context.Context, filter interface{}) (*mongo.DeleteResult, error) {
	return m.Col().DeleteMany(ctx, filter)
}
