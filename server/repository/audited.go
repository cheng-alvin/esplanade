package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// AuditedRepository is the audit-aware generic Mongo repository. It requires
// an Auditable document and manages created_at and updated_at on inserts,
// updated_at on updates, and deleted_at on soft deletes. Reads and updates
// exclude soft-deleted documents automatically.
type AuditedRepository[T any, PT interface {
	*T
	Auditable
}] struct {
	base *repository[T, PT]
}

// NewAudited returns an audit-aware repository backed by collection. timeout
// bounds every individual operation issued through it; pass 0 to use
// DefaultOperationTimeout.
func NewAudited[T any, PT interface {
	*T
	Auditable
}](collection *mongo.Collection, timeout time.Duration) *AuditedRepository[T, PT] {
	return &AuditedRepository[T, PT]{base: newRepository[T, PT](collection, timeout)}
}

// InsertOne inserts doc, populating its ID, CreatedAt, and UpdatedAt before
// the write so every resource gets consistent audit fields.
func (r *AuditedRepository[T, PT]) InsertOne(ctx context.Context, doc PT) error {
	return r.base.insertOne(ctx, doc, func(doc PT, now time.Time) {
		doc.SetCreatedAt(now)
		doc.SetUpdatedAt(now)
	})
}

// FindByID looks up a single, non-deleted document by its hex-encoded
// ObjectID. It returns esmongo.ErrNotFound both when id is malformed and when
// no document matches.
func (r *AuditedRepository[T, PT]) FindByID(ctx context.Context, id string) (PT, error) {
	return r.base.findByID(ctx, id, scoped)
}

// Find returns all non-deleted documents matching filter.
//
// filter must be built by the resource-specific package from typed parameters
// — Find never accepts a caller-supplied arbitrary filter map, since that's
// the main NoSQL-injection vector in Go Mongo code.
func (r *AuditedRepository[T, PT]) Find(ctx context.Context, filter bson.M) ([]PT, error) {
	return r.base.find(ctx, filter, scoped)
}

// FindPage returns a page of non-deleted documents matching filter, ordered by
// _id ascending. Pagination is cursor-based and pageSize is capped at
// MaxPageSize.
func (r *AuditedRepository[T, PT]) FindPage(ctx context.Context, filter bson.M, afterID string, pageSize int) (FindResult[PT], error) {
	return r.base.findPage(ctx, filter, afterID, pageSize, scoped)
}

// UpdateOne applies fields to the document with the given id via a $set
// update, then bumps updated_at. It returns esmongo.ErrNotFound if no
// non-deleted document matched.
func (r *AuditedRepository[T, PT]) UpdateOne(ctx context.Context, id string, fields bson.M) error {
	return r.base.updateOne(ctx, id, fields, scoped, func(set bson.M) {
		set["updated_at"] = time.Now().UTC()
	})
}

// DeleteOne soft-deletes the document with the given id by setting deleted_at
// and updated_at. It returns esmongo.ErrNotFound if no non-deleted document
// matched.
func (r *AuditedRepository[T, PT]) DeleteOne(ctx context.Context, id string) error {
	return r.base.softDeleteOne(ctx, id, scoped)
}

// HardDelete permanently removes the document with the given id, bypassing
// the soft-delete marker entirely. Prefer DeleteOne unless permanent erasure
// is actually required.
func (r *AuditedRepository[T, PT]) HardDelete(ctx context.Context, id string) error {
	return r.base.hardDelete(ctx, id)
}

// Count returns the number of non-deleted documents matching filter.
func (r *AuditedRepository[T, PT]) Count(ctx context.Context, filter bson.M) (int64, error) {
	return r.base.count(ctx, filter, scoped)
}

// Exists reports whether at least one non-deleted document matches filter.
func (r *AuditedRepository[T, PT]) Exists(ctx context.Context, filter bson.M) (bool, error) {
	return r.base.exists(ctx, filter, scoped)
}
