// Package devices resolves client-generated device identifiers to stable
// device document IDs for session linkage.
package devices

import (
	"context"
	"errors"
	"strings"

	autherr "github.com/cheng-alvin/esplanade/server/auth/errors"
	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
	repository "github.com/cheng-alvin/esplanade/server/repository"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const DeviceCollection = "devices"

// maxIdentifierLength keeps the compound unique index key (user_id +
// identifier) well within Mongo's 1024-byte index entry limit.
const maxIdentifierLength = 256

type Device struct {
	repository.Base `bson:",inline"`

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
	repo *repository.AuditedRepository[Device, *Device]
}

func NewStore(collection *mongo.Collection) *Store {
	return &Store{repo: repository.NewAudited[Device](collection, 0)}
}

func (s *Store) FindOrRegister(ctx context.Context, userID bson.ObjectID, identifier string) (bson.ObjectID, error) {
	identifier, err := ValidateIdentifier(identifier)
	if err != nil {
		return bson.ObjectID{}, err
	}

	filter := bson.M{"user_id": userID, "identifier": identifier}

	found, err := s.repo.Find(ctx, filter)
	if err != nil {
		return bson.ObjectID{}, err
	}
	if len(found) > 0 {
		return found[0].ID(), nil
	}

	device := &Device{UserID: userID, Identifier: identifier}
	if err := s.repo.InsertOne(ctx, device); err != nil {
		if errors.Is(err, esmongo.ErrConflict) {
			// A concurrent call registered the same (user_id, identifier)
			// between the find and the insert; the unique index guarantees a
			// single winner, so re-read and hand back its ID rather than
			// surfacing a spurious conflict.
			found, findErr := s.repo.Find(ctx, filter)
			if findErr == nil && len(found) > 0 {
				return found[0].ID(), nil
			}
		}
		return bson.ObjectID{}, err
	}
	return device.ID(), nil
}
