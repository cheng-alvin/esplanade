// Package repository holds the auth domain's Mongo documents and typed
// queries. Queries call the mongo-driver directly; filters are always built
// internally from typed parameters — never from raw client input.
package repository

import (
	"context"
	"time"

	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// UserCollection is the Mongo collection holding user documents.
const UserCollection = "users"

// AuthProvider identifies an authentication method.
type AuthProvider string

const (
	AuthProviderEmail  AuthProvider = "email"
	AuthProviderGoogle AuthProvider = "google"
)

const (
	UserStatusActive    = "active"
	UserStatusSuspended = "suspended"
	UserStatusDeleted   = "deleted"
)

// AuthMethodCommon is embedded in every provider-specific auth method.
type AuthMethodCommon struct {
	Provider  AuthProvider `bson:"provider"`
	CreatedAt time.Time    `bson:"created_at"`
	UpdatedAt time.Time    `bson:"updated_at"`
}

// EmailVerificationToken is embedded on EmailAuthMethod while a verification
// is pending; it is replaced or removed rather than retained.
type EmailVerificationToken struct {
	TokenHash    string    `bson:"token_hash"`
	ExpiresAt    time.Time `bson:"expires_at"`
	CreatedAt    time.Time `bson:"created_at"`
	AttemptsLeft int       `bson:"attempts_left"`
}

// EmailAuthMethod is embedded on User for the email + password sign-in method.
type EmailAuthMethod struct {
	AuthMethodCommon  `bson:",inline"`
	PasswordHash      string                  `bson:"password_hash"`
	EmailVerified     bool                    `bson:"email_verified"`
	VerificationToken *EmailVerificationToken `bson:"verification_token,omitempty"`
}

// GoogleAuthMethod is embedded on User for Google sign-in.
type GoogleAuthMethod struct {
	AuthMethodCommon `bson:",inline"`
	Subject          string  `bson:"subject"`
	Email            string  `bson:"email"`
	Picture          *string `bson:"picture,omitempty"`
}

type User struct {
	ObjectID    bson.ObjectID `bson:"_id"`
	Email       string        `bson:"email"`
	DisplayName string        `bson:"display_name"`
	AvatarURL   *string       `bson:"avatar_url"`
	Status      string        `bson:"status"`

	DeviceIDs   []string `bson:"device_ids,omitempty"`
	DeviceCount int      `bson:"device_count"`

	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`

	EmailAuth  *EmailAuthMethod  `bson:"email_auth,omitempty"`
	GoogleAuth *GoogleAuthMethod `bson:"google_auth,omitempty"`
}

// User carries its audit fields directly so the persisted schema matches the
// design exactly.
func (u *User) SetID(id bson.ObjectID)   { u.ObjectID = id }
func (u *User) ID() bson.ObjectID        { return u.ObjectID }
func (u *User) SetCreatedAt(t time.Time) { u.CreatedAt = t }
func (u *User) SetUpdatedAt(t time.Time) { u.UpdatedAt = t }

func (User) CollectionName() string { return UserCollection }

func (User) Indexes() []mongo.IndexModel {
	return []mongo.IndexModel{
		{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "google_auth.subject", Value: 1}}, Options: options.Index().SetUnique(true).SetSparse(true)},
		{Keys: bson.D{{Key: "email_auth.verification_token.token_hash", Value: 1}}, Options: options.Index().SetSparse(true)},
	}
}

// UserRepository queries the users collection with typed filters. The
// duplicate-key error from the unique email / Google-subject indexes surfaces
// as esmongo.ErrConflict via TranslateError.
type UserRepository struct {
	collection *mongo.Collection
}

func NewUserRepository(collection *mongo.Collection) *UserRepository {
	return &UserRepository{collection: collection}
}

func (r *UserRepository) Insert(ctx context.Context, user *User) error {
	user.SetID(bson.NewObjectID())
	now := time.Now().UTC()
	user.SetCreatedAt(now)
	user.SetUpdatedAt(now)
	if _, err := r.collection.InsertOne(ctx, user); err != nil {
		return esmongo.TranslateError(err)
	}
	return nil
}

func (r *UserRepository) FindByID(ctx context.Context, id string) (*User, error) {
	oid, err := objectID(id)
	if err != nil {
		return nil, err
	}
	return r.findOne(ctx, bson.M{"_id": oid})
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*User, error) {
	return r.findOne(ctx, bson.M{"email": email})
}

func (r *UserRepository) FindByGoogleSubject(ctx context.Context, subject string) (*User, error) {
	return r.findOne(ctx, bson.M{"google_auth.subject": subject})
}

func (r *UserRepository) FindByVerificationTokenHash(ctx context.Context, hash string) (*User, error) {
	return r.findOne(ctx, bson.M{"email_auth.verification_token.token_hash": hash})
}

func (r *UserRepository) findOne(ctx context.Context, filter bson.M) (*User, error) {
	var user User
	if err := r.collection.FindOne(ctx, scoped(filter)).Decode(&user); err != nil {
		return nil, esmongo.TranslateError(err)
	}
	return &user, nil
}

// update applies a single-document update, bumps updated_at, and excludes
// soft-deleted documents.
func (r *UserRepository) update(ctx context.Context, id string, fields bson.M, unset []string) error {
	oid, err := objectID(id)
	if err != nil {
		return err
	}

	set := make(bson.M, len(fields)+1)
	for k, v := range fields {
		set[k] = v
	}
	set["updated_at"] = time.Now().UTC()

	update := bson.M{"$set": set}
	if len(unset) > 0 {
		names := make(bson.M, len(unset))
		for _, name := range unset {
			names[name] = ""
		}
		update["$unset"] = names
	}

	res, err := r.collection.UpdateOne(ctx, scoped(bson.M{"_id": oid}), update)
	if err != nil {
		return esmongo.TranslateError(err)
	}
	if res.MatchedCount == 0 {
		return esmongo.ErrNotFound
	}
	return nil
}

func (r *UserRepository) SetVerificationToken(ctx context.Context, id string, tok *EmailVerificationToken) error {
	return r.update(ctx, id, bson.M{
		"email_auth.verification_token": tok,
		"email_auth.updated_at":         time.Now().UTC(),
	}, nil)
}

func (r *UserRepository) MarkEmailVerified(ctx context.Context, id string) error {
	return r.update(ctx, id, bson.M{"email_auth.email_verified": true}, []string{"email_auth.verification_token"})
}

func (r *UserRepository) SetPasswordHash(ctx context.Context, id string, passwordHash string) error {
	return r.update(ctx, id, bson.M{"email_auth.password_hash": passwordHash}, nil)
}

func (r *UserRepository) SetGoogleAuth(ctx context.Context, id string, method *GoogleAuthMethod) error {
	return r.update(ctx, id, bson.M{"google_auth": method}, nil)
}

func (r *UserRepository) UnsetEmailAuth(ctx context.Context, id string) error {
	return r.update(ctx, id, nil, []string{"email_auth"})
}

func (r *UserRepository) UnsetGoogleAuth(ctx context.Context, id string) error {
	return r.update(ctx, id, nil, []string{"google_auth"})
}
