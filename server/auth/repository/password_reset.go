package repository

import (
	"context"
	"time"

	repository "github.com/cheng-alvin/esplanade/server/repository"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// PasswordResetCollection is the Mongo collection holding single-use password
// reset tokens.
const PasswordResetCollection = "password_reset_tokens"

type PasswordResetToken struct {
	ObjectID  bson.ObjectID `bson:"_id"`
	UserID    bson.ObjectID `bson:"user_id"`
	TokenHash string        `bson:"token_hash"`
	ExpiresAt time.Time     `bson:"expires_at"`
	UsedAt    *time.Time    `bson:"used_at,omitempty"`
	CreatedAt time.Time     `bson:"created_at"`
	PurgeAt   time.Time     `bson:"purge_at"`
}

func (t *PasswordResetToken) SetID(id bson.ObjectID) { t.ObjectID = id }
func (t *PasswordResetToken) ID() bson.ObjectID      { return t.ObjectID }

func (PasswordResetToken) CollectionName() string { return PasswordResetCollection }

func (PasswordResetToken) Indexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: "token_hash", Value: 1}}},
		{Keys: bson.D{{Key: "user_id", Value: 1}}},
		{Keys: bson.D{{Key: "purge_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)},
	}
}

// PasswordResetRepository composes the lightweight generic repository with
// typed reset-token queries.
type PasswordResetRepository struct {
	repo *repository.Repository[PasswordResetToken, *PasswordResetToken]
}

func NewPasswordResetRepository(collection *mongo.Collection) *PasswordResetRepository {
	return &PasswordResetRepository{repo: repository.New[PasswordResetToken](collection, 0)}
}

func (r *PasswordResetRepository) Insert(ctx context.Context, token *PasswordResetToken) error {
	return r.repo.InsertOne(ctx, token)
}

func (r *PasswordResetRepository) FindByTokenHash(ctx context.Context, hash string) (*PasswordResetToken, error) {
	return first(r.repo.Find(ctx, bson.M{"token_hash": hash}))
}

// MarkUsed CAS-marks the token used; a false return means it was already
// consumed. The nil filter value matches both missing and null used_at.
func (r *PasswordResetRepository) MarkUsed(ctx context.Context, id string, purgeAt time.Time) (bool, error) {
	oid, err := objectID(id)
	if err != nil {
		return false, err
	}

	matched, err := r.repo.UpdateMany(ctx,
		bson.M{"_id": oid, "used_at": nil},
		bson.M{"used_at": time.Now().UTC(), "purge_at": purgeAt})
	if err != nil {
		return false, err
	}
	return matched > 0, nil
}

// InvalidateAllForUser marks every unused token of the user used, pulling each
// purge_at in to the provided time.
func (r *PasswordResetRepository) InvalidateAllForUser(ctx context.Context, userID string, purgeAt time.Time) (int64, error) {
	oid, err := objectID(userID)
	if err != nil {
		return 0, err
	}

	return r.repo.UpdateMany(ctx,
		bson.M{"user_id": oid, "used_at": nil},
		bson.M{"used_at": time.Now().UTC(), "purge_at": purgeAt})
}
