// Package repository composes the generic Mongo repository layer with the auth
// domain's documents and typed queries. Filters are always built internally
// from typed parameters — never from raw client input.
package repository

import (
	"context"
	"time"

	repository "github.com/cheng-alvin/esplanade/server/repository"
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

// User does not embed repository.Base; it implements the Auditable contract
// directly so the persisted schema matches the design exactly.
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

// UserRepository composes the audited generic repository with typed user
// queries. The duplicate-key error from the unique email / Google-subject
// indexes surfaces as esmongo.ErrConflict through the generic layer.
type UserRepository struct {
	repo *repository.AuditedRepository[User, *User]
}

func NewUserRepository(collection *mongo.Collection) *UserRepository {
	return &UserRepository{repo: repository.NewAudited[User](collection, 0)}
}

func (r *UserRepository) Insert(ctx context.Context, user *User) error {
	return r.repo.InsertOne(ctx, user)
}

func (r *UserRepository) FindByID(ctx context.Context, id string) (*User, error) {
	return r.repo.FindByID(ctx, id)
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*User, error) {
	return first(r.repo.Find(ctx, bson.M{"email": email}))
}

func (r *UserRepository) FindByGoogleSubject(ctx context.Context, subject string) (*User, error) {
	return first(r.repo.Find(ctx, bson.M{"google_auth.subject": subject}))
}

func (r *UserRepository) FindByVerificationTokenHash(ctx context.Context, hash string) (*User, error) {
	return first(r.repo.Find(ctx, bson.M{"email_auth.verification_token.token_hash": hash}))
}

func (r *UserRepository) SetVerificationToken(ctx context.Context, id string, tok *EmailVerificationToken) error {
	return r.repo.UpdateOne(ctx, id, bson.M{
		"email_auth.verification_token": tok,
		"email_auth.updated_at":         time.Now().UTC(),
	})
}

func (r *UserRepository) MarkEmailVerified(ctx context.Context, id string) error {
	return r.repo.UpdateOneWithUnset(ctx, id, bson.M{"email_auth.email_verified": true}, []string{"email_auth.verification_token"})
}

func (r *UserRepository) SetPasswordHash(ctx context.Context, id string, passwordHash string) error {
	return r.repo.UpdateOne(ctx, id, bson.M{"email_auth.password_hash": passwordHash})
}

func (r *UserRepository) SetGoogleAuth(ctx context.Context, id string, method *GoogleAuthMethod) error {
	return r.repo.UpdateOne(ctx, id, bson.M{"google_auth": method})
}

func (r *UserRepository) UnsetEmailAuth(ctx context.Context, id string) error {
	return r.repo.UpdateOneWithUnset(ctx, id, nil, []string{"email_auth"})
}

func (r *UserRepository) UnsetGoogleAuth(ctx context.Context, id string) error {
	return r.repo.UpdateOneWithUnset(ctx, id, nil, []string{"google_auth"})
}
