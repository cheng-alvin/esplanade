# Esplanade Authentication — Implementation Contract

> **Status:** adopted for the `auth-implementation/*` branch group.
>
> This document is the **authoritative contract** for that work. It records the adopted
> sprint plan, every decision taken, and the interfaces each branch implements. Where a
> habit, guess, or memory of the original sprint document conflicts with this document,
> **this document wins**.

---

## 1. Scope

Email/password and Google sign-in, registration, sessions (JWT access token + rotating
opaque refresh token), email verification, password reset, provider linking/unlinking.

**Non-goals:** roles/permissions; Google API scopes (Drive, etc.); device
trust/pairing/verification (only a minimal `devices` package for `device_id` resolution);
Swift/client work (the iOS Keychain note in the original plan describes future client
behavior); automated tests and build/lint execution (the maintainer compiles and tests
manually).

## 2. Adopted decisions

| ID | Decision |
|----|----------|
| D1 | **HS256** access tokens. Secret supplied via `ESPLANADE_JWT_SECRET` (no default, never committed, never logged). |
| D2 | Package layout follows existing repo conventions (§5). |
| D3 | Branches are pushed to GitHub; stacked PRs are merged bottom-up. |
| D4 | **No tests** (`*_test.go`) and **no build/lint/test/gofmt/tidy execution** in this branch group. Write code that compiles first try. |
| D5 | New dependencies: `github.com/golang-jwt/jwt/v5` v5.3.1, `github.com/coreos/go-oidc/v3` v3.21.0, `golang.org/x/crypto` (argon2), stdlib `net/smtp`. These are declared on the base branch's `go.mod`; the maintainer runs `go mod tidy` locally before building (pre-existing repo gap: stale `go.sum`). |
| R1 | **Strict** refresh-token reuse detection: replay of a rotated-out token revokes **all** of the user's sessions. No grace window. |
| R2 | A successful password reset revokes all sessions for the user. |
| R3 | Rate limiter applies to `resend-verification` and `password/forgot` only (in-memory; single-instance assumption; login limiting deferred). |
| R4 | `sessions` has **no IP field** (the original §3 `ip` mention is dropped). |
| R5 | `POST /auth/logout` accepts an optional `refresh_token` body field for exact-session deletion; without it, deletes the sessions for the `(user_id, device_id)` from the access token. Always 204 (idempotent). |
| R6 | `email_verified` exists **only** on `email_auth`. App-level email verification is an email/password-only concept; Google sign-in does **not** gate on Google's `email_verified` claim. `RequireVerifiedEmail` passes when `google_auth` is set **or** `email_auth.email_verified` is `true`. |
| R7 | Additional indexes: `sessions.refresh_token_hash`, `sessions.previous_token_hash` (sparse), `password_reset_tokens.token_hash`, `password_reset_tokens.user_id`, unique `devices(user_id, identifier)`. |
| R8 | `POST /auth/register` returns **409** for an existing email (registration is not enumeration-safe; only login / forgot / resend are). |
| R9 | Constants: verification-token TTL 24h; reset-token TTL 1h; resend cap 3/h; `verification_token.attempts_left` is set to 5 and is **never decremented** this sprint (no code-entry endpoint; the field is reserved for a future one). |
| R11 | `users` uses the **audited** repository (managed `created_at`/`updated_at`; soft delete unused — `status` is the lifecycle field; `User` does **not** embed `repository.Base`, it implements the `Auditable` methods directly). `sessions` and `password_reset_tokens` use the **lightweight** repository (hard delete for logout). |
| R12 | Login performs a dummy password verification when the account is missing or has no email auth, equalizing response timing. |

**PurgeAt / ExpiresAt semantics (normative):** `ExpiresAt` and `RevokedAt`/`UsedAt` are
business-validity fields, checked explicitly on every use. `PurgeAt` is deletion-only
(TTL index), set to the record's effective expiry/invalidation moment **+ purge retention
(30 days)**, and must never appear in an authorization check. Sliding renewal moves
`ExpiresAt` and `PurgeAt` forward together; early invalidation pulls `PurgeAt` in to
`now + 30d`.

## 3. Branch plan

Canonical stack order (each PR targets the branch above it in this list; the first
targets the base):

| # | Branch | Contents |
|---|--------|----------|
| 1 | `repository-write-ops` | `UpdateMany`, `UpdateOneWithUnset`, `DeleteMany` on both repository variants (base `server/repository` package). |
| 2 | `foundation` | Config additions (§6); `server/auth/errors`; `server/auth/token`; `server/auth/jwt`; `server/auth/providers/email`; `server/ratelimit`. |
| 3 | `google-verifier` | `server/auth/providers/google`. |
| 4 | `device-package` | `server/devices`; `main.go` registers the devices index provider. |
| 5 | `data-layer` | `server/auth/repository` (User, Session, PasswordResetToken: documents, indexes, typed queries); `main.go` registers the three auth index providers. |
| 6 | `email-sender` | `server/email` (Sender interface, SMTP implementation, dev log sender). |
| 7 | `service-core` | `server/auth/service`: service struct + Deps, register/login/refresh/logout/logout-all/me, session machinery. |
| 8 | `http-layer` | `server/handler/auth` (core endpoints, DTOs, error mapping); `server/auth/middleware`; `respond.ErrorCode`; ctx key additions; `router` wiring for core routes; `main.go` assembly. |
| 9 | `email-verification` | Service + handler for `verify-email` / `resend-verification`; router routes. |
| 10 | `password-reset` | Service + handler for `password/forgot` / `password/reset`; router routes. |
| 11 | `provider-management` | Service + handler for `identities` / `DELETE providers/{provider}`; router routes. |
| 12 | `google-flows` | Service + handler for `POST /google` / `POST /link/google`; router routes. |

## 4. Collections and documents

Mongo ID type is `bson.ObjectID` (mongo-driver v2 naming; the original plan's
`primitive.ObjectID` spells the same type).

### `users` — audited repository, no `Base` embed

```go
type AuthProvider string

const (
	AuthProviderEmail  AuthProvider = "email"
	AuthProviderGoogle AuthProvider = "google"
)

type AuthMethodCommon struct {
	Provider  AuthProvider `bson:"provider"`
	CreatedAt time.Time    `bson:"created_at"`
	UpdatedAt time.Time    `bson:"updated_at"`
}

type EmailVerificationToken struct {
	TokenHash    string    `bson:"token_hash"`
	ExpiresAt    time.Time `bson:"expires_at"`
	CreatedAt    time.Time `bson:"created_at"`
	AttemptsLeft int       `bson:"attempts_left"`
}

type EmailAuthMethod struct {
	AuthMethodCommon  `bson:",inline"`
	PasswordHash      string                  `bson:"password_hash"`
	EmailVerified     bool                    `bson:"email_verified"`
	VerificationToken *EmailVerificationToken `bson:"verification_token,omitempty"`
}

type GoogleAuthMethod struct {
	AuthMethodCommon `bson:",inline"`
	Subject          string  `bson:"subject"`
	Email            string  `bson:"email"`
	Picture          *string `bson:"picture,omitempty"`
}

type User struct {
	ID          bson.ObjectID `bson:"_id"`
	Email       string        `bson:"email"` // normalized lowercase; unique
	DisplayName string        `bson:"display_name"`
	AvatarURL   *string       `bson:"avatar_url"`
	Status      string        `bson:"status"` // active | suspended | deleted

	DeviceIDs   []string `bson:"device_ids,omitempty"`
	DeviceCount int      `bson:"device_count"`

	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`

	// At most one is required at any time; a linked account may carry both.
	EmailAuth  *EmailAuthMethod  `bson:"email_auth,omitempty"`
	GoogleAuth *GoogleAuthMethod `bson:"google_auth,omitempty"`
}

// Auditable contract — User does NOT embed repository.Base; implement directly.
func (u *User) SetID(id bson.ObjectID)   { u.ID = id }
func (u *User) ID() bson.ObjectID        { return u.ID }
func (u *User) SetCreatedAt(t time.Time) { u.CreatedAt = t }
func (u *User) SetUpdatedAt(t time.Time) { u.UpdatedAt = t }

const (
	UserStatusActive    = "active"
	UserStatusSuspended = "suspended"
	UserStatusDeleted   = "deleted"
)
```

Indexes: unique `email`; unique sparse `google_auth.subject`; sparse
`email_auth.verification_token.token_hash`.

Note: `device_ids` / `device_count` are owned by future device-management; the auth
component never writes them.

### `sessions` — lightweight repository

```go
type Session struct {
	ID                bson.ObjectID `bson:"_id"`
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

func (s *Session) SetID(id bson.ObjectID) { s.ID = id }
func (s *Session) ID() bson.ObjectID      { return s.ID }

const (
	RevokedReasonReuseDetected = "reuse_detected"
	RevokedReasonLogoutAll     = "logout_all"
	RevokedReasonPasswordReset = "password_reset"
	RevokedReasonUserInactive  = "user_inactive"
)
```

Indexes: `user_id`; `refresh_token_hash`; sparse `previous_token_hash`; **TTL** on
`purge_at` (`SetExpireAfterSeconds(0)`).

### `password_reset_tokens` — lightweight repository

```go
type PasswordResetToken struct {
	ID        bson.ObjectID `bson:"_id"`
	UserID    bson.ObjectID `bson:"user_id"`
	TokenHash string        `bson:"token_hash"`
	ExpiresAt time.Time     `bson:"expires_at"`
	UsedAt    *time.Time    `bson:"used_at,omitempty"`
	CreatedAt time.Time     `bson:"created_at"`
	PurgeAt   time.Time     `bson:"purge_at"`
}

func (t *PasswordResetToken) SetID(id bson.ObjectID) { t.ID = id }
func (t *PasswordResetToken) ID() bson.ObjectID      { return t.ID }
```

Indexes: `token_hash`; `user_id`; **TTL** on `purge_at`.

### `devices` — audited repository (embeds `repository.Base`)

```go
type Device struct {
	repository.Base `bson:",inline"`

	UserID     bson.ObjectID `bson:"user_id"`
	Identifier string        `bson:"identifier"` // client-generated, opaque
}
```

Index: unique compound `{user_id: 1, identifier: 1}`.

## 5. Package layout and import conventions

```
server/
├── handler/auth/                package auth   (conventionally aliased `authh`)
├── auth/
│   ├── errors/                  package errors (conventionally aliased `autherr`)
│   ├── jwt/                     package jwt
│   ├── token/                   package token
│   ├── middleware/              package middleware (conventionally aliased `authmw`)
│   ├── providers/email/         package email (aliased `emailprovider` when next to `server/email`)
│   ├── providers/google/        package google
│   ├── repository/              package repository (conventionally aliased `authrepo`)
│   └── service/                 package service
├── email/                       package email (SMTP sender)
├── devices/                     package devices
├── ratelimit/                   package ratelimit
```

Existing packages edited: `server/config`, `server/respond`, `server/ctxkey`,
`server/router`, `server/main.go`, `server/repository` (branch 1 only).

## 6. Configuration (branch 2)

All fields follow the existing yaml-tag + `ESPLANADE_`-prefixed env pattern:

| Field | yaml / env | Default |
|---|---|---|
| `JWTSecret string` | `jwt_secret` / `JWT_SECRET` | none (secret) |
| `AccessTokenTTL time.Duration` | `access_token_ttl` / `ACCESS_TOKEN_TTL` | `15m` |
| `RefreshTokenTTL time.Duration` | `refresh_token_ttl` / `REFRESH_TOKEN_TTL` | `720h` |
| `PurgeRetention time.Duration` | `purge_retention` / `PURGE_RETENTION` | `720h` |
| `VerificationTokenTTL time.Duration` | `verification_token_ttl` / `VERIFICATION_TOKEN_TTL` | `24h` |
| `PasswordResetTokenTTL time.Duration` | `password_reset_token_ttl` / `PASSWORD_RESET_TOKEN_TTL` | `1h` |
| `AppBaseURL string` | `app_base_url` / `APP_BASE_URL` | `""` (links become relative) |
| `GoogleClientID string` | `google_client_id` / `GOOGLE_CLIENT_ID` | `""` |
| `SMTPHost string` | `smtp_host` / `SMTP_HOST` | `""` (⇒ dev log sender) |
| `SMTPPort int` | `smtp_port` / `SMTP_PORT` | `587` |
| `SMTPUsername string` | `smtp_username` / `SMTP_USERNAME` | `""` |
| `SMTPPassword string` | `smtp_password` / `SMTP_PASSWORD` | none (secret) |
| `SMTPFrom string` | `smtp_from` / `SMTP_FROM` | `""` |
| `ResendLimit int` | `resend_limit` / `RESEND_LIMIT` | `3` |
| `ResendWindow time.Duration` | `resend_window` / `RESEND_WINDOW` | `1h` |

`config.yaml.example` gains the non-secret fields with comments; secrets stay
commented out / absent.

## 7. Interface contracts

### 7.1 Repository write operations (branch 1, `server/repository`)

Added to both `Repository[T, PT]` and `AuditedRepository[T, PT]` (and the
`RepositoryOperations` interface):

```go
// UpdateMany applies $set fields to every document matching filter and
// returns the number of matched documents. Audited repositories scope the
// filter to non-deleted documents and bump updated_at.
UpdateMany(ctx context.Context, filter bson.M, fields bson.M) (int64, error)

// UpdateOneWithUnset applies $set fields (may be nil/empty) and $unset names
// to the document with the given hex ID, atomically. Audited repositories
// bump updated_at. Returns esmongo.ErrNotFound for a malformed ID or no
// match. A call with nothing to change returns nil (no-op).
UpdateOneWithUnset(ctx context.Context, id string, fields bson.M, unset []string) error

// DeleteMany permanently deletes every document matching filter and returns
// the number deleted.
DeleteMany(ctx context.Context, filter bson.M) (int64, error)
```

Implementation mirrors existing primitives: bounded operation timeout, filters passed
through the internal `scope` function, `esmongo.TranslateError` on driver errors, set
fields copied (never mutated in place).

### 7.2 `server/auth/errors` (branch 2)

```go
var (
	ErrUserExists              = errors.New("auth: user already exists")
	ErrUserNotFound            = errors.New("auth: user not found")
	ErrInvalidEmail            = errors.New("auth: invalid email address")
	ErrWeakPassword            = errors.New("auth: password does not meet requirements")
	ErrInvalidCredentials      = errors.New("auth: invalid credentials")
	ErrEmailNotVerified        = errors.New("auth: email not verified")
	ErrEmailAlreadyVerified    = errors.New("auth: email already verified")
	ErrTokenInvalid            = errors.New("auth: token invalid")
	ErrTokenExpired            = errors.New("auth: token expired")
	ErrRateLimited             = errors.New("auth: rate limit exceeded")
	ErrLinkRequired            = errors.New("auth: account exists; link required")
	ErrGoogleAlreadyLinked     = errors.New("auth: google account already linked")
	ErrInvalidGoogleToken      = errors.New("auth: invalid google token")
	ErrProviderNotFound        = errors.New("auth: provider not linked")
	ErrLastAuthMethod          = errors.New("auth: cannot remove the only sign-in method")
	ErrUnsupportedProvider     = errors.New("auth: unsupported provider")
	ErrInvalidDeviceIdentifier = errors.New("auth: invalid device identifier")
)
```

### 7.3 `server/auth/token` (branch 2)

```go
// Generate returns a new opaque token: 32 bytes from crypto/rand, encoded
// with base64.RawURLEncoding.
func Generate() (string, error)

// Hash returns the hex-encoded SHA-256 digest of the raw token. This is the
// only form that is ever persisted.
func Hash(raw string) string
```

### 7.4 `server/auth/jwt` (branch 2)

```go
type Claims struct {
	UserID    string
	Email     string
	DeviceID  string
	JTI       string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type Issuer struct{ /* secret []byte; ttl time.Duration */ }
func NewIssuer(secret string, ttl time.Duration) *Issuer

// Issue returns the signed token and the lifetime in seconds (for expires_in).
func (i *Issuer) Issue(userID, email, deviceID string) (string, int, error)

type Verifier struct{ /* secret []byte */ }
func NewVerifier(secret string) *Verifier

// Verify returns autherr.ErrTokenExpired for an expired token and
// autherr.ErrTokenInvalid for anything else (bad signature, malformed,
// wrong algorithm).
func (v *Verifier) Verify(raw string) (*Claims, error)
```

Implementation notes (no compiler available — follow exactly):

```go
import jwtlib "github.com/golang-jwt/jwt/v5"

type claims struct {
	Email    string `json:"email"`
	DeviceID string `json:"device_id"`
	jwtlib.RegisteredClaims
}
// Issue: claims{Email, DeviceID, RegisteredClaims{Subject: userID,
//   IssuedAt: jwtlib.NewNumericDate(now), ExpiresAt: jwtlib.NewNumericDate(now.Add(ttl)),
//   ID: uuid.NewString()}}; jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, c).SignedString(secret)
// Verify: jwtlib.ParseWithClaims(raw, &claims{}, func(t *jwtlib.Token) (any, error) {
//   if _, ok := t.Method.(*jwtlib.SigningMethodHMAC); !ok { return nil, autherr.ErrTokenInvalid }
//   return secret, nil
// }, jwtlib.WithValidMethods([]string{"HS256"}))
//   - errors.Is(err, jwtlib.ErrTokenExpired)  -> autherr.ErrTokenExpired
//   - other err / !token.Valid                -> autherr.ErrTokenInvalid
```

`jti` uses `github.com/google/uuid` (already in the module graph).
JSON claims on the wire: `sub`, `email`, `device_id`, `iat`, `exp`, `jti`.

### 7.5 `server/auth/providers/email` (branch 2)

```go
const (
	MinPasswordLength = 8
	MaxPasswordLength = 128
	MaxEmailLength    = 254
)

// NormalizeEmail trims, lowercases, validates via net/mail.ParseAddress and
// returns the bare address. Display-name forms ("Name <a@b>") and inputs
// longer than MaxEmailLength are rejected with a non-nil error.
func NormalizeEmail(raw string) (string, error)

// ValidatePassword enforces 8..128 bytes.
func ValidatePassword(plain string) error

// HashPassword returns a PHC-format argon2id string:
//   $argon2id$v=19$m=19456,t=2,p=1$<b64raw(salt16)>$<b64raw(key32)>
// salt = 16 bytes from crypto/rand; key = 32 bytes.
func HashPassword(plain string) (string, error)

// VerifyPassword recomputes the hash from the encoded parameters and compares
// with subtle.ConstantTimeCompare. Malformed encodings return false.
func VerifyPassword(plain, encoded string) bool
```

argon2id parameters: time = 2, memory = 19456 KiB, threads = 1, keyLen = 32.
Use `golang.org/x/crypto/argon2.IDKey` and `encoding/base64.RawStdEncoding`.

### 7.6 `server/auth/providers/google` (branch 3)

```go
type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool // captured but NOT used for gating (R6)
	Name          string
	Picture       string
}

type Verifier interface {
	Verify(ctx context.Context, rawIDToken string) (*Identity, error)
}

type OIDCVerifier struct{ /* clientID; lazy sync.Once init */ }
func New(clientID string) *OIDCVerifier
func (v *OIDCVerifier) Verify(ctx context.Context, rawIDToken string) (*Identity, error)
```

- Lazy initialization on first `Verify` (so the server starts without network):
  `sync.Once` performing `oidc.NewProvider(ctx, "https://accounts.google.com")` with a
  bounded context (10s, derived from `context.Background()`, not the request), then
  `provider.Verifier(&oidc.Config{ClientID: clientID})`. Store the resulting provider,
  verifier, and error on the struct (guard with a mutex).
- `Verify` uses `verifier.Verify(ctx, rawIDToken)` then `idToken.Claims(&claims)` with
  `var claims struct { Email string `json:"email"`; EmailVerified bool `json:"email_verified"`; Name string `json:"name"`; Picture string `json:"picture"` }`.
- Failure semantics: **token-level** failures (verify error, claims decode, empty
  subject) return `autherr.ErrTokenInvalid` (wrapped with detail). Discovery/network
  failures return the raw wrapped error (the service maps that to a 500).
- Library: `github.com/coreos/go-oidc/v3/oidc`.

### 7.7 `server/email` (branch 6)

```go
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// NewSender returns an SMTP sender when cfg.SMTPHost is set, otherwise a
// development log sender (warn-level log including the body, so the
// maintainer can copy verification/reset links locally without SMTP).
func NewSender(cfg *config.Config, logger *zap.Logger) Sender
```

- SMTP message: `From:`, `To:`, `Subject:`, `MIME-Version: 1.0`,
  `Content-Type: text/plain; charset="utf-8"` headers, CRLF line endings, blank line,
  body.
- `smtp.PlainAuth("", username, password, host)` only when username is non-empty.
- Port 465: `tls.Dial` + `smtp.NewClient` + `Auth` + `Mail/Rcpt/Data`.
  Other ports: `smtp.SendMail(host:port, auth, from, []string{to}, msg)` (handles
  STARTTLS when advertised). 10s dial timeout.
- Never log credentials. The dev log sender logs the body (documented as
  development-only convenience).

### 7.8 `server/ratelimit` (branch 2)

```go
type Limiter struct{ /* mutex, max, window, hits map[string][]time.Time */ }
func New(max int, window time.Duration) *Limiter

// Allow records an attempt for key and reports whether it is within the
// window's cap. Fixed-window per key; expired hits are pruned on access.
func (l *Limiter) Allow(key string) bool
```

Key convention: `"resend:<email>"`, `"forgot:<email>"` (normalized email).
In-memory, single instance; documented limitation.

### 7.9 `server/devices` (branch 4)

```go
const DeviceCollection = "devices"

type Device struct {
	repository.Base `bson:",inline"`
	UserID          bson.ObjectID `bson:"user_id"`
	Identifier      string        `bson:"identifier"`
}

func (Device) CollectionName() string        { return DeviceCollection }
func (Device) Indexes() []mongo.IndexModel   { /* unique {user_id:1, identifier:1} */ }

// ValidateIdentifier trims and enforces a non-empty identifier of at most
// 256 characters. Returns the trimmed value.
func ValidateIdentifier(raw string) (string, error)

type Store struct{ /* *repository.AuditedRepository[Device, *Device] */ }
func NewStore(collection *mongo.Collection) *Store

// FindOrRegister returns the stable device ID for (userID, identifier),
// inserting a new devices document when none exists. On a duplicate-key race
// (esmongo.ErrConflict) it re-reads and returns the existing ID. Invalid
// identifiers return autherr.ErrInvalidDeviceIdentifier.
func (s *Store) FindOrRegister(ctx context.Context, userID bson.ObjectID, identifier string) (bson.ObjectID, error)
```

Does **not** maintain `users.device_ids` / `users.device_count` (future
device-management owns those).

### 7.10 `server/auth/repository` (branch 5)

Collection-name constants: `UserCollection = "users"`, `SessionCollection = "sessions"`,
`PasswordResetCollection = "password_reset_tokens"`.

Documents and indexes per §4 (`CollectionName()` / `Indexes()` implemented on the value
types so they satisfy `repository.IndexProvider`). All filters are built internally from
typed parameters — no raw client input ever reaches a Mongo filter. All methods return
`esmongo` sentinels (`TranslateError` is applied by the generic layer).

```go
type UserRepository struct{ /* *repository.AuditedRepository[User, *User] */ }
func NewUserRepository(collection *mongo.Collection) *UserRepository

func (r *UserRepository) Insert(ctx, u *User) error
func (r *UserRepository) FindByID(ctx context.Context, id string) (*User, error)          // hex string
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*User, error)    // normalized input
func (r *UserRepository) FindByGoogleSubject(ctx context.Context, subject string) (*User, error)
func (r *UserRepository) FindByVerificationTokenHash(ctx context.Context, hash string) (*User, error)
func (r *UserRepository) SetVerificationToken(ctx context.Context, id string, tok *EmailVerificationToken) error // also bumps email_auth.updated_at
func (r *UserRepository) MarkEmailVerified(ctx context.Context, id string) error          // $set email_verified=true, $unset verification_token
func (r *UserRepository) SetPasswordHash(ctx context.Context, id string, passwordHash string) error
func (r *UserRepository) SetGoogleAuth(ctx context.Context, id string, method *GoogleAuthMethod) error
func (r *UserRepository) UnsetEmailAuth(ctx context.Context, id string) error
func (r *UserRepository) UnsetGoogleAuth(ctx context.Context, id string) error

type SessionRepository struct{ /* *repository.Repository[Session, *Session] */ }
func NewSessionRepository(collection *mongo.Collection) *SessionRepository

func (r *SessionRepository) Insert(ctx context.Context, s *Session) error
func (r *SessionRepository) FindByID(ctx context.Context, id string) (*Session, error)
func (r *SessionRepository) FindByRefreshTokenHash(ctx context.Context, hash string) (*Session, error)
func (r *SessionRepository) FindByPreviousTokenHash(ctx context.Context, hash string) (*Session, error)

// Rotate atomically replaces the refresh hash only if it still matches
// expectedRefreshHash (CAS): filter {_id, refresh_token_hash: expected}.
// Sets refresh_token_hash, previous_token_hash (= expected), expires_at,
// purge_at, last_refreshed_at = now. Returns false when no document matched
// (concurrent rotation).
func (r *SessionRepository) Rotate(ctx context.Context, id, expectedRefreshHash, newRefreshHash string, expiresAt, purgeAt time.Time) (bool, error)

// Revoke sets revoked_at=now, revoked_reason, purge_at on the given session.
func (r *SessionRepository) Revoke(ctx context.Context, id, reason string, purgeAt time.Time) error

// RevokeAllForUser revokes every not-yet-revoked session of the user
// (filter {user_id, revoked_at: nil}) and returns the count affected.
func (r *SessionRepository) RevokeAllForUser(ctx context.Context, userID, reason string, purgeAt time.Time) (int64, error)

func (r *SessionRepository) Delete(ctx context.Context, id string) error
// DeleteForUserDevice permanently deletes all sessions for (user_id, device_id); returns the count.
func (r *SessionRepository) DeleteForUserDevice(ctx context.Context, userID, deviceID string) (int64, error)

type PasswordResetRepository struct{ /* *repository.Repository[PasswordResetToken, *PasswordResetToken] */ }
func NewPasswordResetRepository(collection *mongo.Collection) *PasswordResetRepository

func (r *PasswordResetRepository) Insert(ctx context.Context, t *PasswordResetToken) error
func (r *PasswordResetRepository) FindByTokenHash(ctx context.Context, hash string) (*PasswordResetToken, error)

// MarkUsed CAS-marks the token used (filter {_id, used_at: nil}), sets
// used_at=now and purge_at; returns false when already used.
func (r *PasswordResetRepository) MarkUsed(ctx context.Context, id string, purgeAt time.Time) (bool, error)

// InvalidateAllForUser marks every unused token of the user used (used_at=now,
// purge_at) and returns the count.
func (r *PasswordResetRepository) InvalidateAllForUser(ctx context.Context, userID string, purgeAt time.Time) (int64, error)
```

### 7.11 `server/auth/service` (branch 7, extended later)

```go
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

type Profile struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	DisplayName   string    `json:"display_name"`
	AvatarURL     *string   `json:"avatar_url"`
	Status        string    `json:"status"`
	EmailVerified bool      `json:"email_verified"` // R6 combined notion
	CreatedAt     time.Time `json:"created_at"`
}

type Identity struct {
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"created_at"`
}

type Deps struct {
	Config      *config.Config
	Logger      *zap.Logger
	Users       *authrepo.UserRepository
	Sessions    *authrepo.SessionRepository
	ResetTokens *authrepo.PasswordResetRepository
	Devices     *devices.Store
	Emails      email.Sender
	Google      google.Verifier
	Issuer      *jwt.Issuer
	Limiter     *ratelimit.Limiter
}

func New(deps Deps) *AuthService
```

Methods, introduced by branch:

- **7** `Register(ctx, email, password, deviceIdentifier, userAgent string) (*TokenPair, error)`
- **7** `Login(ctx, email, password, deviceIdentifier, userAgent string) (*TokenPair, error)`
- **7** `Refresh(ctx, refreshToken string) (*TokenPair, error)`
- **7** `Logout(ctx, userID, deviceID, refreshToken string) error`
- **7** `LogoutAll(ctx, userID string) error`
- **7** `Me(ctx, userID string) (*Profile, error)`
- **9** `VerifyEmail(ctx, rawToken string) error`
- **9** `ResendVerification(ctx, userID, email string) error`
- **10** `ForgotPassword(ctx, email string) error`
- **10** `ResetPassword(ctx, rawToken, newPassword string) error`
- **11** `Identities(ctx, userID string) ([]Identity, error)`
- **11** `UnlinkProvider(ctx, userID, provider string) error`
- **12** `GoogleLogin(ctx, idToken, deviceIdentifier, userAgent string) (*TokenPair, error)`
- **12** `LinkGoogle(ctx, userID, idToken string) error`

Shared helpers (branch 7, `service.go` / `sessions.go`):

- `newSession(ctx, user *authrepo.User, deviceIdentifier, userAgent string) (*TokenPair, error)` —
  validates the device identifier, `Devices.FindOrRegister`, generates the refresh
  token, inserts the `Session` doc (`IssuedAt`/`LastRefreshedAt` = now,
  `ExpiresAt` = now + RefreshTokenTTL, `PurgeAt` = ExpiresAt + PurgeRetention),
  issues the access token (`Issuer.Issue(user.ID.Hex(), user.Email, deviceID.Hex())`).
- `newVerificationToken(now) (raw string, tok *EmailVerificationToken, err error)` —
  24h TTL, `AttemptsLeft` 5.
- `sendVerificationEmail(ctx, to, raw string)` — composes the message
  (`{AppBaseURL}/verify?token={url-escaped}` + raw token as a manual-entry code) and
  dispatches in a goroutine using `context.WithoutCancel(ctx)`; logs send errors only
  (never the token or email body) via `s.logger.Error`.
- `normalizeEmail` wrapper; `dummyHash` generated once in `New` via
  `emailprovider.HashPassword` (timing equalization, R12).
- Email copy is defined in the service (subjects: "Verify your Esplanade email",
  "Reset your Esplanade password").

Services depend on the concrete repository/verifier types above; handlers depend on the
concrete `*AuthService` (no interface extraction this sprint — mockability is only
needed once tests exist).

### 7.12 `server/auth/middleware` + context keys (branch 8)

`server/ctxkey` gains `UserID` and `DeviceID` keys (same `key` type).

```go
type UserFetcher interface {
	FindByID(ctx context.Context, id string) (*authrepo.User, error)
}

func RequireAuth(verifier *jwt.Verifier) func(http.Handler) http.Handler
func OptionalAuth(verifier *jwt.Verifier) func(http.Handler) http.Handler
func RequireVerifiedEmail(users UserFetcher) func(http.Handler) http.Handler

func UserIDFrom(ctx context.Context) (string, bool)
func DeviceIDFrom(ctx context.Context) (string, bool)
```

- Parses `Authorization: Bearer <token>` (case-insensitive `bearer` via
  `strings.Fields`; anything else is malformed).
- `RequireAuth`: missing/malformed ⇒ 401 `invalid_token` "authentication required";
  `autherr.ErrTokenExpired` ⇒ 401 `token_expired`; invalid ⇒ 401 `invalid_token`.
  Injects `UserID`/`DeviceID` into the context.
- `OptionalAuth`: absent header ⇒ continue unauthenticated; present but invalid ⇒ 401
  (fail closed).
- `RequireVerifiedEmail`: reads `UserID` from the context (must be stacked on
  `RequireAuth`), loads the user, passes when `GoogleAuth != nil` or
  `EmailAuth.EmailVerified` (R6); otherwise 403 `email_not_verified`. On
  `esmongo.ErrNotFound` ⇒ 401 `invalid_token`; other errors ⇒ 500 `internal_error`.
  Ships without a consumer this sprint (future pairing/transfer endpoints).

### 7.13 `server/respond` (branch 8)

```go
// ErrorCode writes {"error": message, "code": code}.
func ErrorCode(w http.ResponseWriter, status int, code, message string)
```

`Error` and `JSON` keep their current behavior.

### 7.14 `server/handler/auth` (branch 8, extended by 9–12)

Package `auth`; conventionally imported as `authh`. Handler constructors take
`*service.AuthService` and return `http.HandlerFunc`. All responses go through
`respond`. `r.UserAgent()` is truncated to 512 bytes before use.

Error mapping helper (`errors.go`) — `writeError(w, err)`:

| Error | Status | code |
|---|---|---|
| `ErrUserExists` | 409 | `user_exists` |
| `ErrInvalidEmail` | 400 | `invalid_email` |
| `ErrWeakPassword` | 400 | `weak_password` |
| `ErrInvalidCredentials` | 401 | `invalid_credentials` |
| `ErrEmailNotVerified` | 403 | `email_not_verified` |
| `ErrEmailAlreadyVerified` | 400 | `email_already_verified` |
| `ErrTokenExpired` | 401 | `token_expired` |
| `ErrTokenInvalid` | 401 | `invalid_token` |
| `ErrRateLimited` | 429 | `rate_limited` |
| `ErrLinkRequired` | 409 | `link_required` |
| `ErrGoogleAlreadyLinked` | 409 | `google_already_linked` |
| `ErrInvalidGoogleToken` | 401 | `invalid_google_token` |
| `ErrProviderNotFound` | 404 | `provider_not_found` |
| `ErrLastAuthMethod` | 409 | `last_auth_method` |
| `ErrUnsupportedProvider` | 400 | `unsupported_provider` |
| `ErrInvalidDeviceIdentifier` | 400 | `invalid_device_identifier` |
| `ErrUserNotFound`, `esmongo.ErrNotFound` | 404 | `not_found` |
| default | 500 | `internal_error` ("internal server error"; never leak `err.Error()`) |

Exception: `verify-email` and `password/reset` map `ErrTokenInvalid` to **400**
`invalid_token` with message "invalid or expired token" before falling through to the
generic mapper.

Generic enumeration-safe message (identical for forgot & unauthenticated resend):
`"If an account exists for that email address, a message has been sent."`

DTOs:

```go
// requests
registerRequest      { email, password, device_identifier }
loginRequest         { email, password, device_identifier }
refreshRequest       { refresh_token }
logoutRequest        { refresh_token }        // body optional
resendRequest        { email }                // optional when authenticated
forgotRequest        { email }
resetRequest         { token, new_password }
googleRequest        { id_token, device_identifier }
linkGoogleRequest    { id_token }
```

Response bodies: `TokenPair` as-is; `Profile` as-is; verify ⇒ `{"status":"verified"}`;
reset ⇒ `{"status":"password_reset"}`; link ⇒ `{"status":"linked"}`; identities ⇒
`{"identities":[...]}`; logout/logout-all/unlink ⇒ 204 with no body.

### 7.15 Router and main wiring (branch 8)

```go
// server/router
type AuthDeps struct {
	Service  *service.AuthService
	Verifier *jwt.Verifier
	Users    *authrepo.UserRepository
}

func New(mongoClient *mongo.Client, auth AuthDeps) *http.ServeMux
```

`New` keeps the health/readiness routes, registers auth routes per §8, and skips auth
routes entirely when `auth.Service == nil` (mirrors the existing `readyz(nil)` pattern).

`main.go` assembly (after `EnsureIndexes`): fatal when `cfg.JWTSecret == ""`; warn when
`cfg.GoogleClientID == ""`; construct repositories, `devices.Store`, `jwt.Issuer`,
`jwt.Verifier`, `email.NewSender`, `google.New`, `ratelimit.New`, then
`service.New(service.Deps{...})`, then
`mux := router.New(mongoClient, router.AuthDeps{Service: authService, Verifier: verifier, Users: userRepo})`.

Collection construction via `esmongo.Collection(mongoDB, authrepo.UserCollection)` etc.

## 8. Endpoints and normative flows

Routes are added by the branch that implements the endpoint (branch 8 registers the core
set; branches 9–12 append theirs):

| Branch | Route |
|---|---|
| 8 | `POST /api/v1/auth/register` → `authh.Register(svc)` |
| 8 | `POST /api/v1/auth/login` → `authh.Login(svc)` |
| 8 | `POST /api/v1/auth/refresh` → `authh.Refresh(svc)` |
| 8 | `POST /api/v1/auth/logout` → `RequireAuth(...)(authh.Logout(svc))` |
| 8 | `POST /api/v1/auth/logout-all` → `RequireAuth(...)(authh.LogoutAll(svc))` |
| 8 | `GET /api/v1/auth/me` → `RequireAuth(...)(authh.Me(svc))` |
| 9 | `GET /api/v1/auth/verify-email` → `authh.VerifyEmail(svc)` |
| 9 | `POST /api/v1/auth/resend-verification` → `OptionalAuth(...)(authh.ResendVerification(svc))` |
| 10 | `POST /api/v1/auth/password/forgot` → `authh.ForgotPassword(svc)` |
| 10 | `POST /api/v1/auth/password/reset` → `authh.ResetPassword(svc)` |
| 11 | `GET /api/v1/auth/identities` → `RequireAuth(...)(authh.Identities(svc))` |
| 11 | `DELETE /api/v1/auth/providers/{provider}` → `RequireAuth(...)(authh.UnlinkProvider(svc))` |
| 12 | `POST /api/v1/auth/google` → `authh.GoogleLogin(svc)` |
| 12 | `POST /api/v1/auth/link/google` → `RequireAuth(...)(authh.LinkGoogle(svc))` |

### Register (branch 7)

1. `NormalizeEmail` (⇒ `ErrInvalidEmail`), `ValidatePassword` (⇒ `ErrWeakPassword`),
   `devices.ValidateIdentifier` (⇒ `ErrInvalidDeviceIdentifier`).
2. `FindByEmail` — found ⇒ `ErrUserExists`.
3. Hash the password; generate the verification token (`newVerificationToken`).
4. Build the `User`: `Status` active; `DisplayName` = email local part; `EmailAuth`
   populated with `EmailVerified: false` and the pending token; `AuthMethodCommon`
   timestamps now.
5. `Insert` — `esmongo.ErrConflict` ⇒ `ErrUserExists` (race).
6. Dispatch the verification email asynchronously (failure never fails registration).
7. `newSession` ⇒ `*TokenPair`.

### Login (branch 7)

1. `NormalizeEmail`; `FindByEmail`.
2. Not found, no `EmailAuth`, or failed `VerifyPassword` ⇒ dummy `VerifyPassword` call
   (R12) then `ErrInvalidCredentials`.
3. `Status != active` ⇒ `ErrInvalidCredentials`.
4. `newSession` ⇒ `*TokenPair`. **Unverified users log in successfully** (R6/§7).

### Refresh (branch 7) — exact semantics

1. `hash := token.Hash(raw)`; `FindByRefreshTokenHash`.
2. **Case A** — hash matches the current token: require `RevokedAt == nil` and
   `ExpiresAt.After(now)` (explicit checks; found-but-invalid ⇒ 401 `ErrTokenInvalid`,
   no state change). Load the user (missing / not active ⇒ `Revoke` session with reason
   `user_inactive`, `ErrTokenInvalid`). Generate a new refresh token; `Rotate` (CAS):
   `ExpiresAt = now + RefreshTokenTTL`, `PurgeAt = ExpiresAt + PurgeRetention`.
   Rotation CAS miss ⇒ re-classify (see step 4). Issue a fresh access token from the
   current user row and session device. Return the new pair.
3. **Case B** — hash matches `previous_token_hash` of a session (or step 2's CAS miss
   re-classifies to this): **reuse detected** — `Revoke(session, "reuse_detected", now+PurgeRetention)`
   and `RevokeAllForUser(userID, "reuse_detected", now+PurgeRetention)`; return
   `ErrTokenInvalid`.
4. **Case C** — neither: `ErrTokenInvalid`, no state changes.

Authorization checks always read `ExpiresAt`/`RevokedAt` explicitly; `PurgeAt` is never
consulted for validity.

### Logout (branch 7)

- `refresh_token` present: hash it; if a session matches and `UserID == caller`, delete
  it (hard delete).
- Otherwise (or no match): delete every session for `(userID, deviceID)` from the
  access token (skip when deviceID is empty).
- Always succeed (204); idempotent.

### Logout all (branch 7)

- `RevokeAllForUser(userID, "logout_all", now+PurgeRetention)`. 204.

### Me (branch 7)

- Load user ⇒ `Profile`; `EmailVerified` = `GoogleAuth != nil || (EmailAuth != nil && EmailAuth.EmailVerified)`.

### Verify email (branch 9)

1. Hash the incoming token; `FindByVerificationTokenHash` — miss ⇒ `ErrTokenInvalid`.
2. Token `ExpiresAt.After(now)` must hold (explicit) ⇒ else `ErrTokenInvalid`.
3. `MarkEmailVerified` (single atomic update: `email_verified=true`, unset
   `verification_token`). A consumed token no longer resolves — a second click is
   indistinguishable from garbage (accepted tradeoff).
4. 200 `{"status":"verified"}`.

### Resend verification (branch 9)

- **Authenticated**: load user; `EmailAuth == nil` or already verified (including
  Google-linked per R6) ⇒ `ErrEmailAlreadyVerified`. Rate-limit key
  `"resend:"+user.Email` ⇒ `ErrRateLimited`. Issue a new token via
  `SetVerificationToken` (replaces the token wholesale) + async email.
- **Unauthenticated**: normalize the submitted email; apply the limiter first
  (uniformly ⇒ no existence leak); then, when the account exists with an unverified
  `EmailAuth`, issue + send; always return 200 with the generic message.
- `attempts_left` remains at its default 5 (R9).

### Forgot password (branch 10)

1. Normalize email (⇒ 400 `invalid_email` for malformed input).
2. Limiter key `"forgot:"+email` ⇒ `ErrRateLimited`.
3. Lookup; missing user or no `EmailAuth` ⇒ return 200 generic (no account probing).
4. Invalidate all outstanding unused tokens (`InvalidateAllForUser`), insert the new
   reset token (1h TTL, `PurgeAt = ExpiresAt + PurgeRetention`), async email. 200
   generic.

### Reset password (branch 10)

1. `ValidatePassword(new)` ⇒ `ErrWeakPassword`.
2. Hash; `FindByTokenHash` ⇒ miss ⇒ `ErrTokenInvalid`.
3. `UsedAt != nil` or expired (explicit) ⇒ `ErrTokenInvalid`.
4. Load user; missing or `EmailAuth == nil` ⇒ `ErrTokenInvalid`.
5. `MarkUsed` CAS — false ⇒ `ErrTokenInvalid` (double-submit safe).
6. `SetPasswordHash`; `RevokeAllForUser(userID, "password_reset", now+PurgeRetention)`
   (R2).
7. 200 `{"status":"password_reset"}`.

### Identities (branch 11)

- Read straight off the loaded user: `email` (when `EmailAuth != nil`) then `google`
  (when `GoogleAuth != nil`), each `{provider, created_at}`. 200
  `{"identities":[...]}`.

### Unlink provider (branch 11)

1. `provider` ∈ {`email`, `google`} ⇒ else `ErrUnsupportedProvider`.
2. Load user; requested field nil ⇒ `ErrProviderNotFound` (checked before the count —
   a not-linked provider is a 404 even when the user has a single method).
3. `count <= 1` populated methods ⇒ `ErrLastAuthMethod`.
4. `UnsetEmailAuth` / `UnsetGoogleAuth` (atomic `$unset` + `updated_at`). Unsetting
   `email_auth` also discards any pending verification token implicitly and invalidates
   outstanding reset tokens (`InvalidateAllForUser`).
5. 204. No session revocation (sessions authenticate the user, not the provider).

### Google login (branch 12)

1. `Google.Verify` — token-level failure ⇒ `ErrInvalidGoogleToken`; infrastructure
   failure propagates (500).
2. Require non-empty subject and email (email lowercased/trimmed).
3. `FindByGoogleSubject(subject)`:
   - found ⇒ status must be active ⇒ `newSession`.
   - `esmongo.ErrNotFound` ⇒ continue.
4. `FindByEmail(email)` found ⇒ **`ErrLinkRequired`** (409; no session, no auto-linking).
5. Otherwise create the user: `DisplayName` from the Google `name` claim (fallback:
   email local part), `AvatarURL` from `picture` when non-empty, `GoogleAuth` populated
   with subject/normalized email/picture. `Insert` conflict ⇒ re-resolve once (subject
   ⇒ treat as login; email ⇒ `ErrLinkRequired`; else propagate).
6. `newSession`.

### Link Google (branch 12)

1. Verify the ID token as above.
2. Load the user. Already linked: same subject ⇒ no-op 200 `{"status":"linked"}`;
   different subject ⇒ `ErrGoogleAlreadyLinked`.
3. `FindByGoogleSubject` — linked to a different user ⇒ `ErrGoogleAlreadyLinked`.
4. `SetGoogleAuth` (conflict ⇒ `ErrGoogleAlreadyLinked`). 200 `{"status":"linked"}`.
   The Google-reported email may differ from the account email (schema keeps it
   separately); no `email_verified` gating (R6).

## 9. Constants

| Constant | Value |
|---|---|
| Access token TTL | 15m (`expires_in: 900`) |
| Refresh token TTL | 30d sliding (each rotation extends) |
| Purge retention | 30d past effective expiry/invalidation |
| Verification token TTL | 24h; `attempts_left` 5 |
| Password reset token TTL | 1h |
| Resend rate limit | 3 per hour per email (also applies to forgot) |
| Argon2id | m=19456 KiB, t=2, p=1, keyLen=32, salt=16 |

## 10. Operational notes

- **Build gap:** `go mod tidy` must be run locally before the server compiles
  (pre-existing `go.sum` gap; new deps are declared on the base branch). Do not run it
  as part of this branch group.
- **No automated validation** in this branch group: no tests, no `go build`/`go vet`/
  `gofmt` execution by agents. Correctness is enforced by careful contract adherence and
  review.
- Manual QA suggestions: register→verify→login→refresh→reuse-detection→logout flows;
  forgot→reset (sessions revoked); google login (link-required path); unlink constraints.

## 11. Deferred / flagged

- Login rate limiting (credential stuffing) — deferred; only resend/forgot limited.
- In-memory rate limiter — single-instance assumption; a shared store is needed for
  multi-instance deployments.
- `ResendVerification` manual code entry: no code-entry endpoint exists; `attempts_left`
  cannot be decremented this sprint (documented in R9).
- `RequireVerifiedEmail` ships without a consumer (future pairing/transfer endpoints).
- `users.device_ids` / `users.device_count` are not maintained yet (future
  device-management).
- Password-change notification email and `sessions.ip` were flagged and deliberately
  excluded.
