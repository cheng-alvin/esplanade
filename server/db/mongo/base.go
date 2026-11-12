package mongo

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Base carries the document properties shared across Esplanade collections:
// the Mongo ID, audit timestamps, and the soft-delete / TTL markers.
//
// Embed it inline — the BSON codec does not flatten embedded structs on its
// own, so without the tag the fields land in a nested "base" subdocument:
//
//	type Device struct {
//		Base `bson:",inline"`
//		...
//	}
//
// Embedding is optional: documents with a different persisted shape declare
// their own fields instead and implement only the setters Manager.InsertOne
// applies (SetID, SetCreatedAt, SetUpdatedAt).
type Base struct {
	ObjectID  bson.ObjectID `bson:"_id,omitempty"`
	CreatedAt time.Time     `bson:"created_at"`
	UpdatedAt time.Time     `bson:"updated_at"`
	DeletedAt *time.Time    `bson:"deleted_at,omitempty"`
	PurgeAt   time.Time     `bson:"purge_at,omitempty"`
}

func (b *Base) SetID(id bson.ObjectID)   { b.ObjectID = id }
func (b *Base) ID() bson.ObjectID        { return b.ObjectID }
func (b *Base) SetCreatedAt(t time.Time) { b.CreatedAt = t }
func (b *Base) SetUpdatedAt(t time.Time) { b.UpdatedAt = t }
