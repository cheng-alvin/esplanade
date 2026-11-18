package mongo

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Base carries the Mongo document identity shared across Esplanade
// collections.
//
// Embed it inline — the BSON codec does not flatten embedded structs on its
// own, so without the tag the field lands in a nested "base" subdocument:
//
//	type Device struct {
//		Base `bson:",inline"`
//		...
//	}
type Base struct {
	ObjectID bson.ObjectID `bson:"_id,omitempty"`
}

func (b *Base) SetID(id bson.ObjectID) { b.ObjectID = id }
func (b *Base) ID() bson.ObjectID      { return b.ObjectID }

// BaseAudited carries the audit, soft-delete, and TTL fields for resources
// that need them. It embeds Base, so it also provides the Mongo document
// identity.
//
// Embed it inline the same way:
//
//	type User struct {
//		BaseAudited `bson:",inline"`
//		...
//	}
type BaseAudited struct {
	Base      `bson:",inline"`
	CreatedAt time.Time  `bson:"created_at"`
	UpdatedAt time.Time  `bson:"updated_at"`
	DeletedAt *time.Time `bson:"deleted_at,omitempty"`
	PurgeAt   time.Time  `bson:"purge_at,omitempty"`
}

func (b *BaseAudited) SetCreatedAt(t time.Time) { b.CreatedAt = t }
func (b *BaseAudited) SetUpdatedAt(t time.Time) { b.UpdatedAt = t }
