package goose

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// UpdateOne updates a single document. Auto-injects updatedAt into $set.
// Equivalent to: await MediaModel.updateOne(filter, { $set: {...} })
func (m *Model[T]) UpdateOne(ctx context.Context, filter interface{}, update bson.M) (*mongo.UpdateResult, error) {
	injectUpdatedAt(update)
	return m.Col().UpdateOne(ctx, filter, update)
}

// UpdateByID updates a document by _id. Auto-injects updatedAt.
func (m *Model[T]) UpdateByID(ctx context.Context, id string, update bson.M) (*mongo.UpdateResult, error) {
	return m.UpdateOne(ctx, bson.M{"_id": id}, update)
}

// UpdateMany updates multiple documents. Auto-injects updatedAt.
// Equivalent to: await MediaModel.updateMany(filter, { $set: {...} })
func (m *Model[T]) UpdateMany(ctx context.Context, filter interface{}, update bson.M) (*mongo.UpdateResult, error) {
	injectUpdatedAt(update)
	return m.Col().UpdateMany(ctx, filter, update)
}

// UpdateOneRaw updates without auto-injecting updatedAt (for $unset, $push, etc).
func (m *Model[T]) UpdateOneRaw(ctx context.Context, filter interface{}, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	return m.Col().UpdateOne(ctx, filter, update, opts...)
}

// injectUpdatedAt auto-sets updatedAt in $set operator.
func injectUpdatedAt(update bson.M) {
	if setVal, ok := update["$set"]; ok {
		if setMap, ok := setVal.(bson.M); ok {
			setMap["updatedAt"] = time.Now()
		}
	} else {
		// If no $set exists, add one with updatedAt
		update["$set"] = bson.M{"updatedAt": time.Now()}
	}
}
