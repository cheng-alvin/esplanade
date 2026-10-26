package repository

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	// DefaultPageSize is the page size FindPage uses when the caller
	// requests zero or a negative size.
	DefaultPageSize = 50

	// MaxPageSize caps a single FindPage call so a buggy or malicious
	// client cannot request an unbounded page.
	MaxPageSize = 200

	// DefaultOperationTimeout bounds one Mongo operation. It is separate
	// from the request timeout on purpose: a slow query must not tie up a
	// connection for the request's full lifetime.
	DefaultOperationTimeout = 5 * time.Second
)

// Document is the minimum contract for a repository document: the repository
// assigns Mongo ObjectIDs, the document decides how to store them.
type Document interface {
	SetID(bson.ObjectID)
	ID() bson.ObjectID
}

type documentPtr[T any] interface {
	*T
	Document
}

// RepositoryOperations is the shared CRUD surface implemented by Repository
// and AuditedRepository. Every method applies the repository operation timeout
// to ctx.
type RepositoryOperations[PT any] interface {
	InsertOne(ctx context.Context, doc PT) error
	FindByID(ctx context.Context, id string) (PT, error)
	Find(ctx context.Context, filter bson.M) ([]PT, error)

	UpdateOne(ctx context.Context, id string, fields bson.M) error

	// UpdateOneWithUnset applies $set fields (possibly nil or empty) and
	// $unset field names in one atomic update. A malformed ID or missing
	// document returns esmongo.ErrNotFound; an update with nothing to
	// apply is a no-op. Audited repositories bump updated_at.
	UpdateOneWithUnset(ctx context.Context, id string, fields bson.M, unset []string) error

	// UpdateMany applies $set fields to every document matching filter and
	// returns the number of matched documents. Audited repositories scope
	// the filter to non-deleted documents and bump updated_at.
	UpdateMany(ctx context.Context, filter bson.M, fields bson.M) (int64, error)

	DeleteOne(ctx context.Context, id string) error
	HardDelete(ctx context.Context, id string) error

	// DeleteMany permanently deletes every document matching filter and
	// returns the number of deleted documents. Like HardDelete, it ignores
	// the soft-delete marker on audited repositories.
	DeleteMany(ctx context.Context, filter bson.M) (int64, error)

	Count(ctx context.Context, filter bson.M) (int64, error)
	Exists(ctx context.Context, filter bson.M) (bool, error)

	// FindPage returns a cursor-paginated result matching filter, ordered
	// by ascending _id. An empty afterID starts at the beginning; pageSize
	// defaults to DefaultPageSize and is capped at MaxPageSize. Audited
	// repositories exclude soft-deleted documents.
	FindPage(ctx context.Context, filter bson.M, afterID string, pageSize int) (FindResult[PT], error)
}

type filterScope func(bson.M) bson.M

// deletedAtUnset matches documents that have not been soft-deleted.
var deletedAtUnset = bson.M{"$exists": false}

// clone copies m, returning a non-nil map so callers can add to it without
// mutating the original.
func clone(m bson.M) bson.M {
	out := maps.Clone(m)
	if out == nil {
		out = bson.M{}
	}
	return out
}

func unscoped(filter bson.M) bson.M { return clone(filter) }

func scoped(filter bson.M) bson.M {
	q := clone(filter)
	q["deleted_at"] = deletedAtUnset
	return q
}

// repositoryConfig captures the only ways Repository and AuditedRepository
// differ: which documents reads and writes touch, the audit fields stamped on
// writes, and whether DeleteOne soft-deletes.
type repositoryConfig[T any, PT documentPtr[T]] struct {
	scope        filterScope
	initialize   func(PT, time.Time)
	updateFields func(bson.M)
	softDelete   bool
}

// repository implements the full RepositoryOperations surface once. Repository
// and AuditedRepository embed it anonymously, so these methods are promoted
// onto both types as if declared on each; the config passed to newRepository
// selects the lightweight or audit-aware behaviour.
type repository[T any, PT documentPtr[T]] struct {
	collection *mongo.Collection
	timeout    time.Duration
	config     repositoryConfig[T, PT]
}

func newRepository[T any, PT documentPtr[T]](
	collection *mongo.Collection,
	timeout time.Duration,
	config repositoryConfig[T, PT],
) *repository[T, PT] {
	if timeout <= 0 {
		timeout = DefaultOperationTimeout
	}
	if config.scope == nil {
		config.scope = unscoped
	}
	return &repository[T, PT]{collection: collection, timeout: timeout, config: config}
}

func (r *repository[T, PT]) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.timeout)
}

func (r *repository[T, PT]) InsertOne(ctx context.Context, doc PT) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	doc.SetID(bson.NewObjectID())
	if r.config.initialize != nil {
		r.config.initialize(doc, time.Now().UTC())
	}

	if _, err := r.collection.InsertOne(ctx, doc); err != nil {
		return esmongo.TranslateError(err)
	}
	return nil
}

func (r *repository[T, PT]) FindByID(ctx context.Context, id string) (PT, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, esmongo.ErrNotFound
	}

	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	var doc T
	target := r.collection.FindOne(ctx, r.config.scope(bson.M{"_id": oid}))
	if err := target.Decode(&doc); err != nil {
		return nil, esmongo.TranslateError(err)
	}
	return PT(&doc), nil
}

func (r *repository[T, PT]) Find(ctx context.Context, filter bson.M) ([]PT, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	cursor, err := r.collection.Find(ctx, r.config.scope(filter))
	if err != nil {
		return nil, esmongo.TranslateError(err)
	}
	items := make([]PT, 0)
	if err := cursor.All(ctx, &items); err != nil {
		return nil, esmongo.TranslateError(err)
	}
	return items, nil
}

// FindResult is the page returned by FindPage: the matched documents plus a
// cursor for fetching the next page, if any.
type FindResult[PT any] struct {
	Items []PT

	// NextCursor is the hex ObjectID to pass as afterID to fetch the next
	// page. It is empty when there is no further page.
	NextCursor string
}

func (r *repository[T, PT]) FindPage(
	ctx context.Context,
	filter bson.M,
	afterID string,
	pageSize int,
) (FindResult[PT], error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	pageSize = min(pageSize, MaxPageSize)

	q := r.config.scope(filter)
	if afterID != "" {
		oid, err := bson.ObjectIDFromHex(afterID)
		if err != nil {
			return FindResult[PT]{},
				fmt.Errorf("repository: invalid cursor %q: %w", afterID, err)
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
			return FindResult[PT]{},
				fmt.Errorf("repository: decoding document: %w", err)
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

func setUpdatedAt(set bson.M) {
	set["updated_at"] = time.Now().UTC()
}

func buildSet(fields bson.M, updateFields func(bson.M)) bson.M {
	set := clone(fields)
	if updateFields != nil {
		updateFields(set)
	}
	return set
}

func (r *repository[T, PT]) updateOne(
	ctx context.Context,
	id string,
	fields bson.M,
	updateFields func(bson.M),
) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return esmongo.ErrNotFound
	}

	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	update := bson.M{"$set": buildSet(fields, updateFields)}
	res, err := r.collection.UpdateOne(ctx, r.config.scope(bson.M{"_id": oid}), update)
	if err != nil {
		return esmongo.TranslateError(err)
	}
	if res.MatchedCount == 0 {
		return esmongo.ErrNotFound
	}
	return nil
}

func (r *repository[T, PT]) UpdateOne(ctx context.Context, id string, fields bson.M) error {
	return r.updateOne(ctx, id, fields, r.config.updateFields)
}

func (r *repository[T, PT]) UpdateOneWithUnset(
	ctx context.Context,
	id string,
	fields bson.M,
	unset []string,
) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return esmongo.ErrNotFound
	}

	update := bson.M{}
	if len(fields) > 0 || r.config.updateFields != nil {
		update["$set"] = buildSet(fields, r.config.updateFields)
	}
	if len(unset) > 0 {
		names := make(bson.M, len(unset))
		for _, name := range unset {
			names[name] = ""
		}
		update["$unset"] = names
	}
	if len(update) == 0 {
		return nil
	}

	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	res, err := r.collection.UpdateOne(ctx, r.config.scope(bson.M{"_id": oid}), update)
	if err != nil {
		return esmongo.TranslateError(err)
	}
	if res.MatchedCount == 0 {
		return esmongo.ErrNotFound
	}
	return nil
}

func (r *repository[T, PT]) UpdateMany(
	ctx context.Context,
	filter bson.M,
	fields bson.M,
) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	update := bson.M{"$set": buildSet(fields, r.config.updateFields)}
	res, err := r.collection.UpdateMany(ctx, r.config.scope(filter), update)
	if err != nil {
		return 0, esmongo.TranslateError(err)
	}
	return res.MatchedCount, nil
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

// DeleteOne permanently removes a lightweight document, or soft-deletes an
// audit-aware one by stamping deleted_at.
func (r *repository[T, PT]) DeleteOne(ctx context.Context, id string) error {
	if r.config.softDelete {
		now := time.Now().UTC()
		return r.updateOne(ctx, id, bson.M{"deleted_at": now, "updated_at": now}, nil)
	}
	return r.deleteOne(ctx, id)
}

func (r *repository[T, PT]) HardDelete(ctx context.Context, id string) error {
	return r.deleteOne(ctx, id)
}

func (r *repository[T, PT]) DeleteMany(ctx context.Context, filter bson.M) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	res, err := r.collection.DeleteMany(ctx, filter)
	if err != nil {
		return 0, esmongo.TranslateError(err)
	}
	return res.DeletedCount, nil
}

func (r *repository[T, PT]) Count(ctx context.Context, filter bson.M) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	count, err := r.collection.CountDocuments(ctx, r.config.scope(filter))
	if err != nil {
		return 0, esmongo.TranslateError(err)
	}
	return count, nil
}

func (r *repository[T, PT]) Exists(ctx context.Context, filter bson.M) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	findOneOpts := options.FindOne().SetProjection(bson.M{"_id": 1})
	err := r.collection.FindOne(ctx, r.config.scope(filter), findOneOpts).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	if err != nil {
		return false, esmongo.TranslateError(err)
	}
	return true, nil
}
