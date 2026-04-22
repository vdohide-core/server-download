package goose

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Query[T] is a chainable query builder for MongoDB find operations.
//
// Equivalent to Mongoose:
//
//	Model.find(filter).sort({createdAt: -1}).limit(10).skip(20).select("name status")
//
// Usage:
//
//	results, err := FileModel.Query(bson.M{"type": "video"}).
//	    Sort("createdAt", -1).
//	    Limit(10).
//	    Skip(20).
//	    Select("name", "status", "createdAt").
//	    Exec(ctx)
//
//	// Single result
//	file, err := FileModel.Query(bson.M{"slug": "abc"}).One(ctx)
type Query[T any] struct {
	model      *Model[T]
	filter     interface{}
	sort       bson.D
	limit      *int64
	skip       *int64
	projection bson.M
}

// Query starts a chainable query with the given filter.
//
//	FileModel.Query(bson.M{"status": "active"})
func (m *Model[T]) Query(filter interface{}) *Query[T] {
	if filter == nil {
		filter = bson.M{}
	}
	return &Query[T]{
		model:  m,
		filter: filter,
	}
}

// Sort adds a sort field. Use 1 for ascending, -1 for descending.
// Can be called multiple times for compound sort.
//
//	.Sort("createdAt", -1)                           // single sort
//	.Sort("status", 1).Sort("createdAt", -1)         // compound sort
func (q *Query[T]) Sort(field string, order int) *Query[T] {
	q.sort = append(q.sort, bson.E{Key: field, Value: order})
	return q
}

// SortDesc is a shorthand for .Sort(field, -1)
//
//	.SortDesc("createdAt")
func (q *Query[T]) SortDesc(field string) *Query[T] {
	return q.Sort(field, -1)
}

// SortAsc is a shorthand for .Sort(field, 1)
//
//	.SortAsc("name")
func (q *Query[T]) SortAsc(field string) *Query[T] {
	return q.Sort(field, 1)
}

// Limit sets the maximum number of results to return.
//
//	.Limit(10)
func (q *Query[T]) Limit(n int64) *Query[T] {
	q.limit = &n
	return q
}

// Skip sets the number of results to skip (for pagination).
//
//	.Skip(20)
func (q *Query[T]) Skip(n int64) *Query[T] {
	q.skip = &n
	return q
}

// Select specifies which fields to include in the result (projection).
// Pass field names to include. _id is always included unless explicitly excluded.
//
//	.Select("name", "status", "createdAt")
func (q *Query[T]) Select(fields ...string) *Query[T] {
	if q.projection == nil {
		q.projection = bson.M{}
	}
	for _, f := range fields {
		q.projection[f] = 1
	}
	return q
}

// Exclude specifies which fields to exclude from the result.
//
//	.Exclude("password", "token")
func (q *Query[T]) Exclude(fields ...string) *Query[T] {
	if q.projection == nil {
		q.projection = bson.M{}
	}
	for _, f := range fields {
		q.projection[f] = 0
	}
	return q
}

// Page is a pagination helper. Sets skip and limit from page number and page size.
// Page is 1-indexed (page 1 = first page).
//
//	.Page(2, 20)  // page 2, 20 items per page → skip 20, limit 20
func (q *Query[T]) Page(page, pageSize int64) *Query[T] {
	if page < 1 {
		page = 1
	}
	skip := (page - 1) * pageSize
	q.skip = &skip
	q.limit = &pageSize
	return q
}

// Exec executes the query and returns all matching documents.
//
//	results, err := FileModel.Query(filter).Sort("createdAt", -1).Limit(10).Exec(ctx)
func (q *Query[T]) Exec(ctx context.Context) ([]*T, error) {
	opts := options.Find()

	if len(q.sort) > 0 {
		opts.SetSort(q.sort)
	}
	if q.limit != nil {
		opts.SetLimit(*q.limit)
	}
	if q.skip != nil {
		opts.SetSkip(*q.skip)
	}
	if len(q.projection) > 0 {
		opts.SetProjection(q.projection)
	}

	return q.model.Find(ctx, q.filter, opts)
}

// One executes the query and returns the first matching document.
//
//	file, err := FileModel.Query(bson.M{"slug": "abc"}).One(ctx)
func (q *Query[T]) One(ctx context.Context) (*T, error) {
	opts := options.FindOne()

	if len(q.sort) > 0 {
		opts.SetSort(q.sort)
	}
	if q.skip != nil {
		opts.SetSkip(*q.skip)
	}
	if len(q.projection) > 0 {
		opts.SetProjection(q.projection)
	}

	return q.model.FindOne(ctx, q.filter, opts)
}

// Count returns the number of documents matching the query filter.
//
//	count, err := FileModel.Query(filter).Count(ctx)
func (q *Query[T]) Count(ctx context.Context) (int64, error) {
	return q.model.CountDocuments(ctx, q.filter)
}
