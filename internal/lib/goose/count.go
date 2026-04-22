package goose

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CountDocuments counts documents matching the filter.
// Equivalent to: await MediaModel.countDocuments(filter)
func (m *Model[T]) CountDocuments(ctx context.Context, filter interface{}) (int64, error) {
	return m.Col().CountDocuments(ctx, filter)
}

// Exists checks if at least one document exists matching the filter.
func (m *Model[T]) Exists(ctx context.Context, filter interface{}) (bool, error) {
	count, err := m.Col().CountDocuments(ctx, filter)
	return count > 0, err
}

// Aggregate runs an aggregation pipeline.
func (m *Model[T]) Aggregate(ctx context.Context, pipeline interface{}, opts ...*options.AggregateOptions) (*mongo.Cursor, error) {
	return m.Col().Aggregate(ctx, pipeline, opts...)
}
