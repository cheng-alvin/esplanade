package mongo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Manager wraps a single *mongo.Collection with the CRUD operations shared
// across resources, so resource code does not repeat them per collection.
// It is deliberately thin: documents are supplied and decoded by the caller,
// updates are caller-built update documents, and the underlying collection
// stays reachable via Collection for anything the primitives do not cover.
//
// Filters and updates must be built from typed parameters — never from raw
// client input.
type Manager struct {
	collection *mongo.Collection
}

// NewManager returns a Manager backed by collection.
func NewManager(collection *mongo.Collection) *Manager {
	return &Manager{collection: collection}
}

// Collection exposes the underlying driver collection for operations the
// primitives do not cover.
func (m *Manager) Collection() *mongo.Collection {
	return m.collection
}

// The setters InsertOne applies to the base properties it can reach. Base
// implements all three; a document with a different persisted shape can
// implement any subset, and each property is filled independently.
type (
	idSetter        interface{ SetID(bson.ObjectID) }
	createdAtSetter interface{ SetCreatedAt(time.Time) }
	updatedAtSetter interface{ SetUpdatedAt(time.Time) }
)

// InsertOne fills in the base properties doc supports — a fresh ObjectID for
// documents with SetID (replacing any ID already set), the current UTC time
// for documents with SetCreatedAt or SetUpdatedAt — then inserts it. Pass a
// pointer to the document so those setters are reachable. Duplicate-key
// conflicts from unique indexes surface as ErrConflict.
func (m *Manager) InsertOne(ctx context.Context, doc any, opts ...options.Lister[options.InsertOneOptions]) error {
	now := time.Now().UTC()
	if d, ok := doc.(idSetter); ok {
		d.SetID(bson.NewObjectID())
	}
	if d, ok := doc.(createdAtSetter); ok {
		d.SetCreatedAt(now)
	}
	if d, ok := doc.(updatedAtSetter); ok {
		d.SetUpdatedAt(now)
	}

	_, err := m.collection.InsertOne(ctx, doc, opts...)
	return TranslateError(err)
}

// FindOne decodes the first document matching filter into out. A miss
// surfaces as ErrNotFound.
func (m *Manager) FindOne(ctx context.Context, filter any, out any, opts ...options.Lister[options.FindOneOptions]) error {
	if err := m.collection.FindOne(ctx, filter, opts...).Decode(out); err != nil {
		return TranslateError(err)
	}
	return nil
}

// FindByID decodes the document with the given hex ID into out. Malformed
// IDs return ErrInvalidID; misses surface as ErrNotFound.
func (m *Manager) FindByID(ctx context.Context, id string, out any, opts ...options.Lister[options.FindOneOptions]) error {
	oid, err := ParseObjectID(id)
	if err != nil {
		return err
	}
	return m.FindOne(ctx, bson.M{"_id": oid}, out, opts...)
}

// FindMany decodes every document matching filter into out, which must be a
// pointer to a slice. Sort, projection, and limit pass through opts.
func (m *Manager) FindMany(ctx context.Context, filter any, out any, opts ...options.Lister[options.FindOptions]) error {
	cursor, err := m.collection.Find(ctx, filter, opts...)
	if err != nil {
		return TranslateError(err)
	}
	if err := cursor.All(ctx, out); err != nil {
		return TranslateError(err)
	}
	return nil
}

// UpdateOne applies a caller-built update document to the first document
// matching filter. The update's semantics, including any updated_at bump,
// stay the caller's job. Returns ErrNotFound when no document matched and
// none was upserted.
func (m *Manager) UpdateOne(ctx context.Context, filter any, update any, opts ...options.Lister[options.UpdateOneOptions]) error {
	res, err := m.collection.UpdateOne(ctx, filter, update, opts...)
	if err != nil {
		return TranslateError(err)
	}
	if res.MatchedCount == 0 && res.UpsertedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateMany applies a caller-built update document to every document
// matching filter and reports how many matched. Matching nothing is not an
// error for a batch operation.
func (m *Manager) UpdateMany(ctx context.Context, filter any, update any, opts ...options.Lister[options.UpdateManyOptions]) (int64, error) {
	res, err := m.collection.UpdateMany(ctx, filter, update, opts...)
	if err != nil {
		return 0, TranslateError(err)
	}
	return res.MatchedCount, nil
}

// DeleteOne removes the first document matching filter. Returns ErrNotFound
// when nothing matched.
func (m *Manager) DeleteOne(ctx context.Context, filter any, opts ...options.Lister[options.DeleteOneOptions]) error {
	res, err := m.collection.DeleteOne(ctx, filter, opts...)
	if err != nil {
		return TranslateError(err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteByID removes the document with the given hex ID. Malformed IDs
// return ErrInvalidID; misses surface as ErrNotFound.
func (m *Manager) DeleteByID(ctx context.Context, id string, opts ...options.Lister[options.DeleteOneOptions]) error {
	oid, err := ParseObjectID(id)
	if err != nil {
		return err
	}
	return m.DeleteOne(ctx, bson.M{"_id": oid}, opts...)
}

// DeleteMany removes every document matching filter and reports how many
// were deleted.
func (m *Manager) DeleteMany(ctx context.Context, filter any, opts ...options.Lister[options.DeleteManyOptions]) (int64, error) {
	res, err := m.collection.DeleteMany(ctx, filter, opts...)
	if err != nil {
		return 0, TranslateError(err)
	}
	return res.DeletedCount, nil
}

// ParseObjectID converts a hex-encoded ID, as callers hold it in strings,
// into a bson.ObjectID. Malformed input returns ErrInvalidID rather than the
// driver's hex error; callers building their own _id filters from string IDs
// should use it so bad input fails the same way everywhere.
func ParseObjectID(id string) (bson.ObjectID, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return bson.ObjectID{}, ErrInvalidID
	}
	return oid, nil
}
