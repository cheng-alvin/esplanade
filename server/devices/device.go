// Package devices resolves client-generated device identifiers to stable
// device document IDs for session linkage.
package devices

import (
	"context"
	"errors"
	"strings"
	"time"

	autherr "github.com/cheng-alvin/esplanade/server/auth/errors"
	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const DeviceCollection = "devices"

// maxIdentifierLength keeps the compound unique index key (user_id +
// identifier) well within Mongo's 1024-byte index entry limit.
const maxIdentifierLength = 256

type Device struct {
	ObjectID  bson.ObjectID `bson:"_id,omitempty"`
	CreatedAt time.Time     `bson:"created_at"`
	UpdatedAt time.Time     `bson:"updated_at"`
	DeletedAt *time.Time    `bson:"deleted_at,omitempty"`

	UserID     bson.ObjectID `bson:"user_id"`
	Identifier string        `bson:"identifier"`
}

func (Device) CollectionName() string { return DeviceCollection }

func (Device) Indexes() []mongo.IndexModel {
	return []mongo.IndexModel{{
		Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "identifier", Value: 1}},
		Options: options.Index().SetUnique(true),
	}}
}

func ValidateIdentifier(raw string) (string, error) {
	identifier := strings.TrimSpace(raw)
	if identifier == "" || len(identifier) > maxIdentifierLength {
		return "", autherr.ErrInvalidDeviceIdentifier
	}
	return identifier, nil
}

type Store struct {
	collection *mongo.Collection
}

func NewStore(collection *mongo.Collection) *Store {
	return &Store{collection: collection}
}

func (s *Store) FindOrRegister(ctx context.Context, userID bson.ObjectID, identifier string) (bson.ObjectID, error) {
	identifier, err := ValidateIdentifier(identifier)
	if err != nil {
		return bson.ObjectID{}, err
	}

	filter := bson.M{
		"user_id":    userID,
		"identifier": identifier,
		"deleted_at": bson.M{"$exists": false},
	}

	var found Device
	err = s.collection.FindOne(ctx, filter).Decode(&found)
	if err == nil {
		return found.ObjectID, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return bson.ObjectID{}, esmongo.TranslateError(err)
	}

	now := time.Now().UTC()
	device := &Device{
		ObjectID:   bson.NewObjectID(),
		CreatedAt:  now,
		UpdatedAt:  now,
		UserID:     userID,
		Identifier: identifier,
	}
	if _, err := s.collection.InsertOne(ctx, device); err != nil {
		translated := esmongo.TranslateError(err)
		if errors.Is(translated, esmongo.ErrConflict) {
			// A concurrent call registered the same (user_id, identifier)
			// between the find and the insert; the unique index guarantees a
			// single winner, so re-read and hand back its ID rather than
			// surfacing a spurious conflict.
			var existing Device
			if err := s.collection.FindOne(ctx, filter).Decode(&existing); err == nil {
				return existing.ObjectID, nil
			}
		}
		return bson.ObjectID{}, translated
	}
	return device.ObjectID, nil
}
