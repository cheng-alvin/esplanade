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
	// DefaultPageSize is used by Find when the caller requests a page
	// size of zero or less.
	DefaultPageSize = 50

	// MaxPageSize is the hard upper bound on documents returned by a
	// single Find call, regardless of what the caller requests. This
	// exists so a buggy or malicious client can never force a full
	// collection scan into memory via an unbounded page size.
	MaxPageSize = 200

	// DefaultOperationTimeout bounds an individual Mongo operation. An
	// HTTP request timeout and a database operation timeout are
	// different concerns — a slow query should not be able to hold a
	// connection open for the full request lifetime.
	DefaultOperationTimeout = 5 * time.Second
)

// deletedAtUnset is the filter fragment excluding soft-deleted
// documents. It is composed into every read and update the generic
// layer performs by default.
var deletedAtUnset = bson.M{"$exists": false}

type Document interface {
	SetID(bson.ObjectID)
	GetID() bson.ObjectID
	SetCreatedAt(time.Time)
	SetUpdatedAt(time.Time)
}

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

// Repository is a thin, generic wrapper around a single Mongo
// collection.
//
// T is the resource's struct type; PT is its pointer type, constrained
// to implement Document (satisfied automatically by embedding Base).
// Instantiate as e.g. repository.New[Device](collection, timeout), which
// yields a Repository[Device, *Device].
type Repository[T any, PT interface {
	*T
	Document
}] struct {
	collection *mongo.Collection
	timeout    time.Duration
}

// New returns a Repository backed by collection. timeout bounds every
// individual operation issued through it; pass 0 to use
// DefaultOperationTimeout.
func New[T any, PT interface {
	*T
	Document
}](collection *mongo.Collection, timeout time.Duration) *Repository[T, PT] {
	if timeout <= 0 {
		timeout = DefaultOperationTimeout
	}
	return &Repository[T, PT]{collection: collection, timeout: timeout}
}

func (r *Repository[T, PT]) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.timeout)
}

// scoped returns a copy of filter with the soft-delete exclusion merged
// in. filter is never mutated.
func scoped(filter bson.M) bson.M {
	q := make(bson.M, len(filter)+1)
	for k, v := range filter {
		q[k] = v
	}
	q["deleted_at"] = deletedAtUnset
	return q
}

// InsertOne inserts doc, populating its ID, CreatedAt, and UpdatedAt
// before the write so every resource gets consistent audit fields
// without the caller having to remember to set them.
func (r *Repository[T, PT]) InsertOne(ctx context.Context, doc PT) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	now := time.Now().UTC()
	doc.SetID(bson.NewObjectID())
	doc.SetCreatedAt(now)
	doc.SetUpdatedAt(now)

	if _, err := r.collection.InsertOne(ctx, doc); err != nil {
		return esmongo.TranslateError(err)
	}
	return nil
}

// FindByID looks up a single, non-deleted document by its hex-encoded
// ObjectID. It returns esmongo.ErrNotFound both when id is malformed and
// when no document matches — callers get one clear error either way,
// never a raw driver error or a panic.
func (r *Repository[T, PT]) FindByID(ctx context.Context, id string) (PT, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, esmongo.ErrNotFound
	}

	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	filter := scoped(bson.M{"_id": oid})

	var doc T
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		return nil, esmongo.TranslateError(err)
	}
	return PT(&doc), nil
}

// FindResult is the page returned by Find: the matched documents plus a
// cursor for fetching the next page, if any.
type FindResult[PT any] struct {
	Items []PT

	// NextCursor is the hex ObjectID to pass as afterID to fetch the
	// next page. It is empty when there is no further page.
	NextCursor string
}

// Find returns a page of non-deleted documents matching filter, ordered
// by _id ascending.
//
// filter must be built by the resource-specific package from typed
// parameters — Find never accepts a caller-supplied arbitrary filter
// map, since that's the main NoSQL-injection vector in Go Mongo code.
//
// Pagination is cursor-based (filter on _id > afterID) rather than
// skip/limit, since skip/limit slows down as the offset grows and can
// return inconsistent pages under concurrent inserts. pageSize is capped
// at MaxPageSize regardless of what's requested.
func (r *Repository[T, PT]) Find(ctx context.Context, filter bson.M, afterID string, pageSize int) (FindResult[PT], error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	switch {
	case pageSize <= 0:
		pageSize = DefaultPageSize
	case pageSize > MaxPageSize:
		pageSize = MaxPageSize
	}

	q := scoped(filter)
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

// UpdateOne applies fields to the document with the given id via a
// $set update — never a whole-document replacement — so a caller can't
// accidentally wipe fields it didn't intend to touch. UpdatedAt is
// bumped automatically. Returns esmongo.ErrNotFound if no non-deleted
// document matched, rather than silently succeeding on a no-op.
func (r *Repository[T, PT]) UpdateOne(ctx context.Context, id string, fields bson.M) error {
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
	set["updated_at"] = time.Now().UTC()

	filter := scoped(bson.M{"_id": oid})
	res, err := r.collection.UpdateOne(ctx, filter, bson.M{"$set": set})
	if err != nil {
		return esmongo.TranslateError(err)
	}
	if res.MatchedCount == 0 {
		return esmongo.ErrNotFound
	}
	return nil
}

// DeleteOne soft-deletes the document with the given id by setting
// deleted_at, rather than removing it. A soft delete is usually right
// for anything tied to user data or sync state — it gives an audit
// trail and a recovery path. Returns esmongo.ErrNotFound if no
// non-deleted document matched.
func (r *Repository[T, PT]) DeleteOne(ctx context.Context, id string) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return esmongo.ErrNotFound
	}

	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	now := time.Now().UTC()
	filter := scoped(bson.M{"_id": oid})
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

// HardDelete permanently removes the document with the given id,
// bypassing the soft-delete marker entirely. This is a distinct,
// explicitly-named method — deliberately separate from DeleteOne — so a
// literal, unrecoverable removal is never invoked by accident. Prefer
// DeleteOne unless a hard delete is actually required (e.g. a
// user-initiated data erasure request).
func (r *Repository[T, PT]) HardDelete(ctx context.Context, id string) error {
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

// Count returns the number of non-deleted documents matching filter.
func (r *Repository[T, PT]) Count(ctx context.Context, filter bson.M) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	count, err := r.collection.CountDocuments(ctx, scoped(filter))
	if err != nil {
		return 0, esmongo.TranslateError(err)
	}
	return count, nil
}

// Exists reports whether at least one non-deleted document matches
// filter. It is implemented as a projected FindOne rather than Find
// plus a length check, which would be both slower and easy to get
// subtly wrong under pagination.
func (r *Repository[T, PT]) Exists(ctx context.Context, filter bson.M) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	findOneOpts := options.FindOne().SetProjection(bson.M{"_id": 1})
	err := r.collection.FindOne(ctx, scoped(filter), findOneOpts).Err()
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return false, nil
		}
		return false, esmongo.TranslateError(err)
	}
	return true, nil
}
