package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	// DefaultPageSize is used by FindPage when the caller requests a page
	// size of zero or less.
	DefaultPageSize = 50

	// MaxPageSize is the hard upper bound on documents returned by a
	// single FindPage call, regardless of what the caller requests. This
	// exists so a buggy or malicious client cannot force an unbounded
	// paginated request.
	MaxPageSize = 200

	// DefaultOperationTimeout bounds an individual Mongo operation. An
	// HTTP request timeout and a database operation timeout are
	// different concerns — a slow query should not be able to hold a
	// connection open for the full request lifetime.
	DefaultOperationTimeout = 5 * time.Second
)

// Document is the minimum contract required by Repository. The repository
// owns Mongo IDs, while the document owns the way that ID is represented.
type Document interface {
	SetID(bson.ObjectID)
	GetID() bson.ObjectID
}

// Auditable is implemented by documents that support repository-managed audit
// timestamps. It embeds Document, so every auditable document also satisfies
// the ID contract required by repository operations.
type Auditable interface {
	Document
	SetCreatedAt(time.Time)
	SetUpdatedAt(time.Time)
}

// AuditedDocument is a descriptive alias for Auditable.
type AuditedDocument = Auditable

type Base struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	CreatedAt time.Time     `bson:"created_at"`
	UpdatedAt time.Time     `bson:"updated_at"`
	DeletedAt *time.Time    `bson:"deleted_at,omitempty"`
}

func (b *Base) SetID(id bson.ObjectID)   { b.ID = id }
func (b *Base) GetID() bson.ObjectID     { return b.ID }
func (b *Base) SetCreatedAt(t time.Time) { b.CreatedAt = t }
func (b *Base) SetUpdatedAt(t time.Time) { b.UpdatedAt = t }

// repository contains the CRUD mechanics shared by Repository and
// AuditedRepository. The exported wrappers choose whether filters are scoped
// to non-deleted documents and whether writes manage audit timestamps.
type repository[T any, PT interface {
	*T
	Document
}] struct {
	collection *mongo.Collection
	timeout    time.Duration
}

func newRepository[T any, PT interface {
	*T
	Document
}](collection *mongo.Collection, timeout time.Duration) *repository[T, PT] {
	if timeout <= 0 {
		timeout = DefaultOperationTimeout
	}
	return &repository[T, PT]{collection: collection, timeout: timeout}
}

func (r *repository[T, PT]) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.timeout)
}

type filterScope func(bson.M) bson.M

func unscoped(filter bson.M) bson.M {
	q := make(bson.M, len(filter))
	for k, v := range filter {
		q[k] = v
	}
	return q
}

// deletedAtUnset is the filter fragment excluding soft-deleted
// documents. It is composed into every read and update the audited
// repository performs by default.
var deletedAtUnset = bson.M{"$exists": false}

func scoped(filter bson.M) bson.M {
	q := unscoped(filter)
	q["deleted_at"] = deletedAtUnset
	return q
}

func (r *repository[T, PT]) insertOne(ctx context.Context, doc PT, initialize func(PT, time.Time)) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	doc.SetID(bson.NewObjectID())
	if initialize != nil {
		initialize(doc, time.Now().UTC())
	}

	if _, err := r.collection.InsertOne(ctx, doc); err != nil {
		return esmongo.TranslateError(err)
	}
	return nil
}

func (r *repository[T, PT]) findByID(ctx context.Context, id string, scope filterScope) (PT, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, esmongo.ErrNotFound
	}

	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	filter := scope(bson.M{"_id": oid})

	var doc T
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		return nil, esmongo.TranslateError(err)
	}
	return PT(&doc), nil
}

func (r *repository[T, PT]) find(ctx context.Context, filter bson.M, scope filterScope) ([]PT, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	cursor, err := r.collection.Find(ctx, scope(filter))
	if err != nil {
		return nil, esmongo.TranslateError(err)
	}
	items := make([]PT, 0)
	if err := cursor.All(ctx, &items); err != nil {
		return nil, esmongo.TranslateError(err)
	}
	return items, nil
}

// FindResult is the page returned by FindPage: the matched documents plus
// a cursor for fetching the next page, if any.
type FindResult[PT any] struct {
	Items []PT

	// NextCursor is the hex ObjectID to pass as afterID to fetch the
	// next page. It is empty when there is no further page.
	NextCursor string
}

func (r *repository[T, PT]) findPage(ctx context.Context, filter bson.M, afterID string, pageSize int, scope filterScope) (FindResult[PT], error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	switch {
	case pageSize <= 0:
		pageSize = DefaultPageSize
	case pageSize > MaxPageSize:
		pageSize = MaxPageSize
	}

	q := scope(filter)
	if afterID != "" {
		oid, err := bson.ObjectIDFromHex(afterID)
		if err != nil {
			return FindResult[PT]{}, fmt.Errorf("repository: invalid cursor %q: %w", afterID, err)
		}
		q["_id"] = bson.M{"$gt": oid}
	}

	findOpts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: 1}}).
		SetLimit(int64(pageSize))

	cursor, err := r.collection.Find(ctx, q, findOpts)
	if err != nil {
		return FindResult[PT]{}, esmongo.TranslateError(err)
	}
	defer cursor.Close(ctx)

	items := make([]PT, 0, pageSize)
	for cursor.Next(ctx) {
		var doc T
		if err := cursor.Decode(&doc); err != nil {
			return FindResult[PT]{}, fmt.Errorf("repository: decoding document: %w", err)
		}
		items = append(items, PT(&doc))
	}
	if err := cursor.Err(); err != nil {
		return FindResult[PT]{}, esmongo.TranslateError(err)
	}

	result := FindResult[PT]{Items: items}
	if len(items) == pageSize {
		result.NextCursor = items[len(items)-1].GetID().Hex()
	}
	return result, nil
}

func (r *repository[T, PT]) updateOne(ctx context.Context, id string, fields bson.M, scope filterScope, updateFields func(bson.M)) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return esmongo.ErrNotFound
	}

	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	set := make(bson.M, len(fields)+1)
	for k, v := range fields {
		set[k] = v
	}
	if updateFields != nil {
		updateFields(set)
	}

	filter := scope(bson.M{"_id": oid})
	res, err := r.collection.UpdateOne(ctx, filter, bson.M{"$set": set})
	if err != nil {
		return esmongo.TranslateError(err)
	}
	if res.MatchedCount == 0 {
		return esmongo.ErrNotFound
	}
	return nil
}

func (r *repository[T, PT]) deleteOne(ctx context.Context, id string) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return esmongo.ErrNotFound
	}

	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	res, err := r.collection.DeleteOne(ctx, bson.M{"_id": oid})
	if err != nil {
		return esmongo.TranslateError(err)
	}
	if res.DeletedCount == 0 {
		return esmongo.ErrNotFound
	}
	return nil
}

func (r *repository[T, PT]) softDeleteOne(ctx context.Context, id string, scope filterScope) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return esmongo.ErrNotFound
	}

	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	now := time.Now().UTC()
	filter := scope(bson.M{"_id": oid})
	update := bson.M{"$set": bson.M{"deleted_at": now, "updated_at": now}}

	res, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return esmongo.TranslateError(err)
	}
	if res.MatchedCount == 0 {
		return esmongo.ErrNotFound
	}
	return nil
}

func (r *repository[T, PT]) hardDelete(ctx context.Context, id string) error {
	return r.deleteOne(ctx, id)
}

func (r *repository[T, PT]) count(ctx context.Context, filter bson.M, scope filterScope) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	count, err := r.collection.CountDocuments(ctx, scope(filter))
	if err != nil {
		return 0, esmongo.TranslateError(err)
	}
	return count, nil
}

func (r *repository[T, PT]) exists(ctx context.Context, filter bson.M, scope filterScope) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	findOneOpts := options.FindOne().SetProjection(bson.M{"_id": 1})
	err := r.collection.FindOne(ctx, scope(filter), findOneOpts).Err()
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return false, nil
		}
		return false, esmongo.TranslateError(err)
	}
	return true, nil
}

// Repository is the lightweight generic Mongo repository. It requires only an
// ID contract: InsertOne sets _id, and it does not write created_at,
// updated_at, or deleted_at. DeleteOne permanently removes a document.
//
// Use NewAudited when a resource embeds Base or otherwise implements
// AuditedDocument and should receive repository-managed audit fields and soft
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

// AuditedRepository is the audit-aware generic Mongo repository. It requires
// an AuditedDocument and manages created_at and updated_at on inserts,
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
	AuditedDocument
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
