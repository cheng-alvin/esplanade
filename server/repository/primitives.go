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
	ID() bson.ObjectID
}

// RepositoryOperations is the shared CRUD surface implemented by standard and
// audited repositories. All methods apply the repository operation timeout to
// ctx.
type RepositoryOperations[PT any] interface {
	InsertOne(ctx context.Context, doc PT) error
	FindByID(ctx context.Context, id string) (PT, error)
	Find(ctx context.Context, filter bson.M) ([]PT, error)

	// FindPage returns a cursor-paginated result matching filter, ordered by
	// ascending _id.
	//
	// - filter: resource-built filter from typed parameters.
	// - afterID: optional hex-encoded ObjectID cursor; empty starts at the beginning.
	// - pageSize: defaults to DefaultPageSize when zero or negative and is capped at MaxPageSize.
	//
	// Audited repositories exclude soft-deleted documents.
	FindPage(ctx context.Context, filter bson.M, afterID string, pageSize int) (FindResult[PT], error)
	UpdateOne(ctx context.Context, id string, fields bson.M) error
	DeleteOne(ctx context.Context, id string) error
	HardDelete(ctx context.Context, id string) error
	Count(ctx context.Context, filter bson.M) (int64, error)
	Exists(ctx context.Context, filter bson.M) (bool, error)
}

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

func (r *Repository[T, PT]) InsertOne(ctx context.Context, doc PT) error {
	return r.base.insertOne(ctx, doc, nil)
}

func (r *AuditedRepository[T, PT]) InsertOne(ctx context.Context, doc PT) error {
	return r.base.insertOne(ctx, doc, func(doc PT, now time.Time) {
		doc.SetCreatedAt(now)
		doc.SetUpdatedAt(now)
	})
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

func (r *Repository[T, PT]) FindByID(ctx context.Context, id string) (PT, error) {
	return r.base.findByID(ctx, id, unscoped)
}

func (r *AuditedRepository[T, PT]) FindByID(ctx context.Context, id string) (PT, error) {
	return r.base.findByID(ctx, id, scoped)
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

func (r *Repository[T, PT]) Find(ctx context.Context, filter bson.M) ([]PT, error) {
	return r.base.find(ctx, filter, unscoped)
}

func (r *AuditedRepository[T, PT]) Find(ctx context.Context, filter bson.M) ([]PT, error) {
	return r.base.find(ctx, filter, scoped)
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
		result.NextCursor = items[len(items)-1].ID().Hex()
	}
	return result, nil
}

func (r *Repository[T, PT]) FindPage(ctx context.Context, filter bson.M, afterID string, pageSize int) (FindResult[PT], error) {
	return r.base.findPage(ctx, filter, afterID, pageSize, unscoped)
}

func (r *AuditedRepository[T, PT]) FindPage(ctx context.Context, filter bson.M, afterID string, pageSize int) (FindResult[PT], error) {
	return r.base.findPage(ctx, filter, afterID, pageSize, scoped)
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

func (r *Repository[T, PT]) UpdateOne(ctx context.Context, id string, fields bson.M) error {
	return r.base.updateOne(ctx, id, fields, unscoped, nil)
}

func (r *AuditedRepository[T, PT]) UpdateOne(ctx context.Context, id string, fields bson.M) error {
	return r.base.updateOne(ctx, id, fields, scoped, func(set bson.M) {
		set["updated_at"] = time.Now().UTC()
	})
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

func (r *Repository[T, PT]) DeleteOne(ctx context.Context, id string) error {
	return r.base.deleteOne(ctx, id)
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

func (r *AuditedRepository[T, PT]) DeleteOne(ctx context.Context, id string) error {
	return r.base.softDeleteOne(ctx, id, scoped)
}

func (r *repository[T, PT]) hardDelete(ctx context.Context, id string) error {
	return r.deleteOne(ctx, id)
}

func (r *Repository[T, PT]) HardDelete(ctx context.Context, id string) error {
	return r.base.hardDelete(ctx, id)
}

func (r *AuditedRepository[T, PT]) HardDelete(ctx context.Context, id string) error {
	return r.base.hardDelete(ctx, id)
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

func (r *Repository[T, PT]) Count(ctx context.Context, filter bson.M) (int64, error) {
	return r.base.count(ctx, filter, unscoped)
}

func (r *AuditedRepository[T, PT]) Count(ctx context.Context, filter bson.M) (int64, error) {
	return r.base.count(ctx, filter, scoped)
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

func (r *Repository[T, PT]) Exists(ctx context.Context, filter bson.M) (bool, error) {
	return r.base.exists(ctx, filter, unscoped)
}

func (r *AuditedRepository[T, PT]) Exists(ctx context.Context, filter bson.M) (bool, error) {
	return r.base.exists(ctx, filter, scoped)
}
