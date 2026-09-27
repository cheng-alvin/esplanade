package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Repository is the lightweight generic Mongo repository. It requires only an
// ID contract: InsertOne sets _id, and it does not write created_at,
// updated_at, or deleted_at. DeleteOne permanently removes a document.
//
// Use NewAudited when a resource embeds Base or otherwise implements
// Auditable and should receive repository-managed audit fields and soft
// deletes.
type Repository[T any, PT interface {
	*T
	Document
}] struct {
	base *repository[T, PT]
}

// New returns a lightweight Repository backed by collection. timeout bounds
// every individual operation issued through it; pass 0 to use
// DefaultOperationTimeout.
func New[T any, PT interface {
	*T
	Document
}](collection *mongo.Collection, timeout time.Duration) *Repository[T, PT] {
	return &Repository[T, PT]{base: newRepository[T, PT](collection, timeout)}
}

// InsertOne inserts doc and assigns it a new Mongo ObjectID.
func (r *Repository[T, PT]) InsertOne(ctx context.Context, doc PT) error {
	return r.base.insertOne(ctx, doc, nil)
}

// FindByID looks up a document by its hex-encoded ObjectID. It returns
// esmongo.ErrNotFound both when id is malformed and when no document matches.
func (r *Repository[T, PT]) FindByID(ctx context.Context, id string) (PT, error) {
	return r.base.findByID(ctx, id, unscoped)
}

// Find returns all documents matching filter.
//
// filter must be built by the resource-specific package from typed parameters
// — Find never accepts a caller-supplied arbitrary filter map, since that's
// the main NoSQL-injection vector in Go Mongo code.
func (r *Repository[T, PT]) Find(ctx context.Context, filter bson.M) ([]PT, error) {
	return r.base.find(ctx, filter, unscoped)
}

// FindPage returns a page of documents matching filter, ordered by _id
// ascending. Pagination is cursor-based and pageSize is capped at MaxPageSize.
func (r *Repository[T, PT]) FindPage(ctx context.Context, filter bson.M, afterID string, pageSize int) (FindResult[PT], error) {
	return r.base.findPage(ctx, filter, afterID, pageSize, unscoped)
}

// UpdateOne applies fields to the document with the given id via a $set
// update. It does not add or modify audit fields.
func (r *Repository[T, PT]) UpdateOne(ctx context.Context, id string, fields bson.M) error {
	return r.base.updateOne(ctx, id, fields, unscoped, nil)
}

// DeleteOne permanently removes the document with the given id.
func (r *Repository[T, PT]) DeleteOne(ctx context.Context, id string) error {
	return r.base.deleteOne(ctx, id)
}

// HardDelete is retained for API symmetry with AuditedRepository. For the
// lightweight repository, DeleteOne is already a permanent delete.
func (r *Repository[T, PT]) HardDelete(ctx context.Context, id string) error {
	return r.base.hardDelete(ctx, id)
}

// Count returns the number of documents matching filter.
func (r *Repository[T, PT]) Count(ctx context.Context, filter bson.M) (int64, error) {
	return r.base.count(ctx, filter, unscoped)
}

// Exists reports whether at least one document matches filter.
func (r *Repository[T, PT]) Exists(ctx context.Context, filter bson.M) (bool, error) {
	return r.base.exists(ctx, filter, unscoped)
}
