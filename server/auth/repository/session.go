package repository

import (
	"context"
	"time"

	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// SessionCollection is the Mongo collection holding refresh-token sessions.
const SessionCollection = "sessions"

const (
	RevokedReasonReuseDetected = "reuse_detected"
	RevokedReasonLogoutAll     = "logout_all"
	RevokedReasonPasswordReset = "password_reset"
	RevokedReasonUserInactive  = "user_inactive"
)

type Session struct {
	ObjectID          bson.ObjectID `bson:"_id"`
	UserID            bson.ObjectID `bson:"user_id"`
	DeviceID          bson.ObjectID `bson:"device_id"`
	RefreshTokenHash  string        `bson:"refresh_token_hash"`
	PreviousTokenHash *string       `bson:"previous_token_hash,omitempty"`
	UserAgent         string        `bson:"user_agent"`
	IssuedAt          time.Time     `bson:"issued_at"`
	LastRefreshedAt   time.Time     `bson:"last_refreshed_at"`
	ExpiresAt         time.Time     `bson:"expires_at"`
	RevokedAt         *time.Time    `bson:"revoked_at,omitempty"`
	RevokedReason     *string       `bson:"revoked_reason,omitempty"`
	PurgeAt           time.Time     `bson:"purge_at"`
}

func (s *Session) SetID(id bson.ObjectID) { s.ObjectID = id }
func (s *Session) ID() bson.ObjectID      { return s.ObjectID }

func (Session) CollectionName() string { return SessionCollection }

func (Session) Indexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: "user_id", Value: 1}}},
		{Keys: bson.D{{Key: "refresh_token_hash", Value: 1}}},
		{Keys: bson.D{{Key: "previous_token_hash", Value: 1}}, Options: options.Index().SetSparse(true)},
		{Keys: bson.D{{Key: "purge_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)},
	}
}

// SessionRepository queries the sessions collection with typed filters.
// Sessions are hard-deleted on logout, per the plan.
type SessionRepository struct {
	collection *mongo.Collection
}

func NewSessionRepository(collection *mongo.Collection) *SessionRepository {
	return &SessionRepository{collection: collection}
}

func (r *SessionRepository) Insert(ctx context.Context, session *Session) error {
	session.SetID(bson.NewObjectID())
	if _, err := r.collection.InsertOne(ctx, session); err != nil {
		return esmongo.TranslateError(err)
	}
	return nil
}

func (r *SessionRepository) FindByID(ctx context.Context, id string) (*Session, error) {
	oid, err := objectID(id)
	if err != nil {
		return nil, err
	}
	return r.findOne(ctx, bson.M{"_id": oid})
}

func (r *SessionRepository) FindByRefreshTokenHash(ctx context.Context, hash string) (*Session, error) {
	return r.findOne(ctx, bson.M{"refresh_token_hash": hash})
}

func (r *SessionRepository) FindByPreviousTokenHash(ctx context.Context, hash string) (*Session, error) {
	return r.findOne(ctx, bson.M{"previous_token_hash": hash})
}

func (r *SessionRepository) findOne(ctx context.Context, filter bson.M) (*Session, error) {
	var session Session
	if err := r.collection.FindOne(ctx, filter).Decode(&session); err != nil {
		return nil, esmongo.TranslateError(err)
	}
	return &session, nil
}

// Rotate atomically replaces the refresh hash only when the stored hash still
// matches expectedRefreshHash. A false return means a concurrent rotation won
// the race.
func (r *SessionRepository) Rotate(ctx context.Context, id, expectedRefreshHash, newRefreshHash string, expiresAt, purgeAt time.Time) (bool, error) {
	oid, err := objectID(id)
	if err != nil {
		return false, err
	}

	res, err := r.collection.UpdateMany(ctx,
		bson.M{"_id": oid, "refresh_token_hash": expectedRefreshHash},
		bson.M{"$set": bson.M{
			"refresh_token_hash":  newRefreshHash,
			"previous_token_hash": expectedRefreshHash,
			"last_refreshed_at":   time.Now().UTC(),
			"expires_at":          expiresAt,
			"purge_at":            purgeAt,
		}})
	if err != nil {
		return false, esmongo.TranslateError(err)
	}
	return res.MatchedCount > 0, nil
}

func (r *SessionRepository) Revoke(ctx context.Context, id, reason string, purgeAt time.Time) error {
	oid, err := objectID(id)
	if err != nil {
		return err
	}

	res, err := r.collection.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{
		"revoked_at":     time.Now().UTC(),
		"revoked_reason": reason,
		"purge_at":       purgeAt,
	}})
	if err != nil {
		return esmongo.TranslateError(err)
	}
	if res.MatchedCount == 0 {
		return esmongo.ErrNotFound
	}
	return nil
}

// RevokeAllForUser revokes every not-yet-revoked session of the user. The nil
// filter value matches both a missing and a null revoked_at.
func (r *SessionRepository) RevokeAllForUser(ctx context.Context, userID, reason string, purgeAt time.Time) (int64, error) {
	oid, err := objectID(userID)
	if err != nil {
		return 0, err
	}

	res, err := r.collection.UpdateMany(ctx,
		bson.M{"user_id": oid, "revoked_at": nil},
		bson.M{"$set": bson.M{
			"revoked_at":     time.Now().UTC(),
			"revoked_reason": reason,
			"purge_at":       purgeAt,
		}})
	if err != nil {
		return 0, esmongo.TranslateError(err)
	}
	return res.MatchedCount, nil
}

func (r *SessionRepository) Delete(ctx context.Context, id string) error {
	oid, err := objectID(id)
	if err != nil {
		return err
	}

	res, err := r.collection.DeleteOne(ctx, bson.M{"_id": oid})
	if err != nil {
		return esmongo.TranslateError(err)
	}
	if res.DeletedCount == 0 {
		return esmongo.ErrNotFound
	}
	return nil
}

func (r *SessionRepository) DeleteForUserDevice(ctx context.Context, userID, deviceID string) (int64, error) {
	uoid, err := objectID(userID)
	if err != nil {
		return 0, err
	}
	doid, err := objectID(deviceID)
	if err != nil {
		return 0, err
	}

	res, err := r.collection.DeleteMany(ctx, bson.M{"user_id": uoid, "device_id": doid})
	if err != nil {
		return 0, esmongo.TranslateError(err)
	}
	return res.DeletedCount, nil
}
