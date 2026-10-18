# Esplanade Authentication

This guide explains how the server authenticates users and devices: the token
model, the HTTP endpoints, the sign-in flows, and how to protect new endpoints
with the auth middleware. [AUTH.md](./AUTH.md) is the authoritative record of
the design decisions, data schema, and interface contracts; this guide
describes the system as built. For the surrounding machinery, see
[ADDING_ENDPOINTS.md](./ADDING_ENDPOINTS.md) (handlers and routing) and
[MONGODB.md](./MONGODB.md) (the repository layer).

---

## Token model

A session is backed by two credentials:

| Credential    | Form                                      | Stored as                        | Lifetime        |
| ------------- | ----------------------------------------- | -------------------------------- | --------------- |
| Access token  | HS256-signed JWT                          | nothing — verified statelessly   | 15m (default)   |
| Refresh token | 32 random bytes, base64url, unpadded      | SHA-256 hex on the session doc   | 30d sliding     |

Access-token claims: `sub` (user hex ID), `email`, `device_id`, `iat`, `exp`,
`jti`. Responses report the access-token lifetime as `expires_in` (seconds).
Tokens are signed with `ESPLANADE_JWT_SECRET`, which has no default — the
server refuses to start without it.

Refresh tokens are single-use and rotate on every refresh: the newly minted
token replaces the old one as the session's current hash, and the old hash is
retained as `previous_token_hash`. Presenting a rotated-out token again is
treated as theft: the implicated session — and every other session of that
user — is revoked. There is no grace window, so clients must serialize refresh
calls; a duplicate refresh in flight will sign the user out everywhere.

Email verification and password reset use the same opaque-token mechanics
(32 bytes, hash at rest, single use) with shorter lifetimes: 24 hours for
verification, 1 hour for reset. A successful password reset revokes all of the
user's sessions; logout hard-deletes the session record.

### Expiry vs. purge

Persisted credentials carry two clock fields with different jobs:

- `expires_at` / `revoked_at` / `used_at` — business validity, checked
  explicitly on every use.
- `purge_at` — deletion only. It drives a Mongo TTL index and is set to the
  record's effective expiry (or invalidation) plus `purge_retention`
  (30 days by default). It is never consulted for authorization.

---

## Layout

```
server/
├── handler/auth/            ← handlers, DTOs, and error mapping (imported as authh)
├── auth/
│   ├── errors/              ← sentinel errors shared by service and handlers (autherr)
│   ├── jwt/                 ← HS256 access-token issuer and verifier
│   ├── token/               ← opaque token generation and SHA-256 hashing
│   ├── middleware/          ← RequireAuth / OptionalAuth / RequireVerifiedEmail (authmw)
│   ├── providers/email/     ← email normalization, password policy, argon2id hashing
│   ├── providers/google/    ← Google ID-token verification via OIDC (google)
│   ├── repository/          ← User / Session / PasswordResetToken documents and queries (authrepo)
│   └── service/             ← AuthService: every flow in one place
├── devices/                 ← device-identifier → device ID resolution for sessions
├── email/                   ← SMTP sender plus a development log sender
├── ratelimit/               ← in-memory limiter for email-sending endpoints
└── router/router.go         ← route registration via router.AuthDeps
```

Handlers depend on the concrete `*service.AuthService`; the service depends on
the concrete repositories, senders, and verifiers. No interfaces are extracted
until tests exist.

---

## Endpoints

All routes live under `/api/v1/auth`. Registration and sign-in are public;
session and provider endpoints require a bearer access token; resend is
optionally authenticated.

| Method | Path                                  | Auth            | Purpose                                     |
| ------ | ------------------------------------- | --------------- | ------------------------------------------- |
| POST   | `/api/v1/auth/register`               | public          | Create an email/password account; returns a session |
| POST   | `/api/v1/auth/login`                  | public          | Email/password sign-in                      |
| POST   | `/api/v1/auth/refresh`                | public          | Rotate the refresh token and issue a new pair |
| POST   | `/api/v1/auth/logout`                 | bearer          | Delete the caller's session or device sessions |
| POST   | `/api/v1/auth/logout-all`             | bearer          | Revoke every session of the user            |
| GET    | `/api/v1/auth/me`                     | bearer          | Current user's profile                      |
| GET    | `/api/v1/auth/verify-email`           | public          | Consume an email-verification token         |
| POST   | `/api/v1/auth/resend-verification`    | optional bearer | Reissue a verification email                |
| POST   | `/api/v1/auth/password/forgot`        | public          | Request a password reset email              |
| POST   | `/api/v1/auth/password/reset`         | public          | Consume a reset token and set a new password |
| GET    | `/api/v1/auth/identities`             | bearer          | List linked sign-in methods                 |
| DELETE | `/api/v1/auth/providers/{provider}`   | bearer          | Unlink `email` or `google`                  |
| POST   | `/api/v1/auth/google`                 | public          | Sign in with a Google ID token              |
| POST   | `/api/v1/auth/link/google`            | bearer          | Link a Google account to the current user   |

Success bodies use three shapes:

```json
{ "access_token": "...", "refresh_token": "...", "expires_in": 900 }
```

```json
{
  "id": "65f1…",
  "email": "ada@example.com",
  "display_name": "ada",
  "avatar_url": null,
  "status": "active",
  "email_verified": false,
  "created_at": "2026-10-18T12:00:00Z"
}
```

…plus endpoint-specific objects (`{"status": "verified"}`,
`{"message": "..."}`, `{"identities": [...]}`). Every error uses the
`respond.ErrorCode` envelope:

```json
{ "error": "invalid token", "code": "invalid_token" }
```

The `code` is stable and meant for programmatic handling — e.g. a client can
treat `token_expired` as "refresh and retry".

### Register

`POST /api/v1/auth/register`

```json
{
  "email": "ada@example.com",
  "password": "correct horse battery",
  "device_identifier": "ios-8F3A2C"
}
```

- `device_identifier` is a client-generated opaque string, stable per install
  (trimmed, non-empty, up to 256 characters). The server resolves it to a
  stable device document per user; it scopes logout.
- The email is trimmed, lowercased, and validated; the password must be 8–128
  bytes.
- Responds `201` with a token pair. Unverified users can sign in and use the
  API; verification gates specific actions (see `RequireVerifiedEmail`).
- A duplicate email is `409 user_exists` — registration is deliberately not
  enumeration-safe. Only login, forgot, and unauthenticated resend are.

Errors: `400 invalid_email`, `400 weak_password`,
`400 invalid_device_identifier`, `409 user_exists`.

### Login

`POST /api/v1/auth/login` — same body, responds `200` with a token pair.
Unknown emails, accounts without an email method, wrong passwords, and
non-active users all return the same `401 invalid_credentials`; unknown
accounts and accounts without an email method still perform a dummy password
verification, so response timing doesn't reveal whether the account exists.

### Refresh

`POST /api/v1/auth/refresh`

```json
{ "refresh_token": "..." }
```

Responds `200` with a new pair and rotates the stored hash. All of these
return `401 invalid_token`:

- the token is unknown or its session has expired,
- the session was revoked,
- the token is no longer the session's current one — a replay of a
  rotated-out token, or a concurrent refresh that lost the rotation race.
  This additionally revokes the session and every other session the user
  holds,
- the user is missing or suspended — the session is revoked with reason
  `user_inactive`.

### Logout and logout-all

`POST /api/v1/auth/logout` is idempotent and always responds `204`. With a
`{"refresh_token": "..."}` body field it deletes exactly that session when it
belongs to the caller; without a usable token it deletes every session for the
caller's `(user, device)`.

`POST /api/v1/auth/logout-all` revokes every session of the user (reason
`logout_all`) and responds `204`.

### Me

`GET /api/v1/auth/me` responds `200` with the profile object. `email_verified`
is the app-level, combined notion: a linked Google account counts as verified
(Google sign-in is not gated on Google's own `email_verified` claim).

### Verify email

Registration and resend email a link (`{AppBaseURL}/verify?token=…`) plus the
raw token as a manual-entry code. Exchanging the token is:

`GET /api/v1/auth/verify-email?token=...`

- `200 {"status":"verified"}` on success. The update is atomic: it sets
  `email_verified` and removes the pending token in a single write.
- `400 invalid_token` ("invalid or expired token") for unknown, expired, or
  already-consumed tokens — a consumed token no longer resolves, so a second
  click is indistinguishable from garbage.

### Resend verification

`POST /api/v1/auth/resend-verification`

- Authenticated: the body may be empty; the caller's own address is used. It
  fails with `400 email_already_verified` when the address is already verified
  or the account is Google-linked.
- Unauthenticated: submit `{"email": "..."}`. The response is always
  `200 {"message":"If an account exists for that email address, a message has been sent."}`
  — the rate limiter runs before any lookup, so a rejection cannot leak
  account existence, and a verified or nonexistent account gets the same
  response.

Rate limit: 3 requests per hour per email address (`resend:<email>`); beyond
that the endpoint returns `429 rate_limited`.

### Forgot password

`POST /api/v1/auth/password/forgot` with `{"email": "..."}`. Responds `200`
with the same generic message as resend; a malformed address is
`400 invalid_email`, and the same 3-per-hour limiter applies
(`forgot:<email>`). Issuing a reset token invalidates all outstanding unused
tokens for the user. Accounts without an email method get the generic response
and no mail.

### Reset password

`POST /api/v1/auth/password/reset`

```json
{ "token": "...", "new_password": "..." }
```

- `200 {"status":"password_reset"}`. The token is single-use (CAS-marked used,
  so a double submit can only win once), and success revokes every session of
  the user — all devices must sign in again.
- `400 weak_password` for a password outside 8–128 bytes.
- `400 invalid_token` ("invalid or expired token") for unknown, expired,
  already-used, or double-submitted tokens.

### Identities and unlinking

`GET /api/v1/auth/identities` responds `200`:

```json
{
  "identities": [
    { "provider": "email", "created_at": "2026-10-18T12:00:00Z" },
    { "provider": "google", "created_at": "2026-10-19T09:30:00Z" }
  ]
}
```

`DELETE /api/v1/auth/providers/{provider}`, where provider is `email` or
`google`, responds `204`. Unlinking is refused when it would remove the only
sign-in method (`409 last_auth_method`), and a provider that isn't linked is
`404 provider_not_found` — even when it is the last remaining one. Unlinking
`email` also discards a pending verification token, invalidates outstanding
reset tokens, and ends the account's ability to sign in with a password.
Sessions survive unlink: they authenticate the user, not the provider.

### Google sign-in and linking

`POST /api/v1/auth/google` with
`{"id_token": "...", "device_identifier": "..."}`. The ID token is verified
against Google's OIDC discovery document and the configured client ID.

- Known Google subject → signed in (`200` token pair).
- Unknown subject whose email already belongs to an account →
  `409 link_required`. There is no auto-linking: the user signs in to the
  existing account and links explicitly.
- Unknown subject and email → a new account is created (`display_name` from
  the `name` claim, avatar from `picture`).
- Bad, malformed, expired, or wrong-audience tokens → `401 invalid_google_token`;
  Google discovery being unreachable is a `500`, not a credential rejection.

`POST /api/v1/auth/link/google` with `{"id_token": "..."}` responds
`200 {"status":"linked"}`, idempotent when the same subject is already linked.
A different Google subject on the account, or a subject linked to another
user, is `409 google_already_linked`. The Google-reported email may differ
from the account email; both are kept.

### Error mapping

The handler layer maps service sentinels to HTTP status and `code`
(`server/handler/auth/errors.go`):

| Error                          | Status | code                       |
| ------------------------------ | ------ | -------------------------- |
| `ErrUserExists`                | 409    | `user_exists`              |
| `ErrInvalidEmail`              | 400    | `invalid_email`            |
| `ErrWeakPassword`              | 400    | `weak_password`            |
| `ErrInvalidCredentials`        | 401    | `invalid_credentials`      |
| `ErrEmailNotVerified`          | 403    | `email_not_verified`       |
| `ErrEmailAlreadyVerified`      | 400    | `email_already_verified`   |
| `ErrTokenExpired`              | 401    | `token_expired`            |
| `ErrTokenInvalid`              | 401    | `invalid_token`            |
| `ErrRateLimited`               | 429    | `rate_limited`             |
| `ErrLinkRequired`              | 409    | `link_required`            |
| `ErrGoogleAlreadyLinked`       | 409    | `google_already_linked`    |
| `ErrInvalidGoogleToken`        | 401    | `invalid_google_token`     |
| `ErrProviderNotFound`          | 404    | `provider_not_found`       |
| `ErrLastAuthMethod`            | 409    | `last_auth_method`         |
| `ErrUnsupportedProvider`       | 400    | `unsupported_provider`     |
| `ErrInvalidDeviceIdentifier`   | 400    | `invalid_device_identifier` |
| user not found                 | 404    | `not_found`                |
| anything else                  | 500    | `internal_error`           |

`verify-email` and `password/reset` map `ErrTokenInvalid` to `400`
`invalid_token` with "invalid or expired token". A JSON body that fails to
decode is `400 invalid_request`; the default 500 message never leaks internal
error text.

---

## Protecting endpoints

Auth middleware wraps individual routes in `router.New`; the global middleware
chain (RequestID → Logging → Recovery → CORS) is unchanged. To protect a new
endpoint, wrap its handler:

```go
mux.Handle("GET /api/v1/transfers", authmw.RequireAuth(verifier)(transfers.List(svc)))
// require the caller to have a verified email as well:
mux.Handle("POST /api/v1/transfers",
    authmw.RequireAuth(verifier)(authmw.RequireVerifiedEmail(users)(transfers.Create(svc))))
```

- `RequireAuth(verifier)` — requires `Authorization: Bearer <access token>`
  (scheme case-insensitive). Missing or malformed → `401 invalid_token`
  "authentication required"; expired → `401 token_expired`; anything else
  invalid → `401 invalid_token`. On success it injects the user and device
  IDs into the request context.
- `OptionalAuth(verifier)` — continues without a token, but a present and
  invalid token still fails closed with `401`.
- `RequireVerifiedEmail(users)` — must be stacked on `RequireAuth`. It passes
  when the user has a Google method or `EmailAuth.EmailVerified`; otherwise
  `403 email_not_verified`. It currently has no consumers; it exists for
  future pairing and transfer endpoints.

Handlers read the identity from the context:

```go
userID, ok := authmw.UserIDFrom(r.Context())
deviceID, _ := authmw.DeviceIDFrom(r.Context())
```

For stable client-facing errors in your own handlers, use
`respond.ErrorCode(w, status, code, message)`; `respond.Error` remains the
message-only form used elsewhere.

---

## Data model

Four collections back the auth features. Indexes are declared on the document
types (`CollectionName` / `Indexes`) and registered once at startup through
`repository.EnsureIndexes` — see [MONGODB.md](./MONGODB.md). Full document
definitions are in [AUTH.md](./AUTH.md), §4.

| Collection              | Repository  | Indexes                                                              | Notes                                                                                  |
| ----------------------- | ----------- | -------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `users`                 | audited     | unique `email`; unique sparse `google_auth.subject`; sparse `email_auth.verification_token.token_hash` | implements `Auditable` directly (no `repository.Base` embed); `status` is the lifecycle field |
| `sessions`              | lightweight | `user_id`; `refresh_token_hash`; sparse `previous_token_hash`; TTL `purge_at` | hard-deleted on logout                                                                 |
| `password_reset_tokens` | lightweight | `token_hash`; `user_id`; TTL `purge_at`                              | single-use                                                                             |
| `devices`               | audited     | unique `(user_id, identifier)`                                       | created on the first session for a device identifier                                   |

All repository filters are built internally from typed parameters; no raw
client input ever reaches a Mongo filter.

---

## Configuration

Auth settings follow the usual yaml-tag + `ESPLANADE_`-prefixed env pattern
(env wins). `server/config.yaml.example` carries the non-secret fields with
comments.

| Field                   | yaml / env                                         | Default                                  |
| ----------------------- | -------------------------------------------------- | ---------------------------------------- |
| `JWTSecret`             | `jwt_secret` / `JWT_SECRET`                        | none (required)                          |
| `AccessTokenTTL`        | `access_token_ttl` / `ACCESS_TOKEN_TTL`            | `15m`                                    |
| `RefreshTokenTTL`       | `refresh_token_ttl` / `REFRESH_TOKEN_TTL`          | `720h`                                   |
| `PurgeRetention`        | `purge_retention` / `PURGE_RETENTION`              | `720h`                                   |
| `VerificationTokenTTL`  | `verification_token_ttl` / `VERIFICATION_TOKEN_TTL`| `24h`                                    |
| `PasswordResetTokenTTL` | `password_reset_token_ttl` / `PASSWORD_RESET_TOKEN_TTL` | `1h`                                |
| `AppBaseURL`            | `app_base_url` / `APP_BASE_URL`                    | `""` (email links become relative)       |
| `GoogleClientID`        | `google_client_id` / `GOOGLE_CLIENT_ID`            | `""` (Google sign-in unavailable; startup warns) |
| `SMTPHost`              | `smtp_host` / `SMTP_HOST`                          | `""` (dev log sender — see below)        |
| `SMTPPort`              | `smtp_port` / `SMTP_PORT`                          | `587`                                    |
| `SMTPUsername`          | `smtp_username` / `SMTP_USERNAME`                  | `""`                                     |
| `SMTPPassword`          | `smtp_password` / `SMTP_PASSWORD`                  | none (secret)                            |
| `SMTPFrom`              | `smtp_from` / `SMTP_FROM`                          | `""`                                     |
| `ResendLimit`           | `resend_limit` / `RESEND_LIMIT`                    | `3`                                      |
| `ResendWindow`          | `resend_window` / `RESEND_WINDOW`                  | `1h`                                     |

`JWTSecret` and `SMTPPassword` are secrets: supply them through the
environment or a secret store, never in a committed file.

### Running locally

```bash
cd server
go mod tidy   # the committed go.sum predates some auth dependencies; needed once
ESPLANADE_JWT_SECRET=dev-secret ESPLANADE_MONGO_URI=mongodb://localhost:27017 make run
```

With `smtp_host` unset, the dev log sender warns with the full message body,
so verification and reset links can be copied straight from the server log.
That sender is development-only: the body carries one-time tokens.

```bash
curl -s http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"ada@example.com","password":"correct horse battery","device_identifier":"curl"}'

curl -s http://localhost:8080/api/v1/auth/refresh \
  -H 'Content-Type: application/json' \
  -d '{"refresh_token":"<from the register response>"}'
```

Calling refresh again with that same old token demonstrates reuse detection:
the user's sessions are revoked and the call fails `401 invalid_token`.

---

## Security notes

- **Secrets stay outside the repo.** `jwt_secret` has no default and is not
  logged; the server exits at startup when it is missing. SMTP credentials are
  only ever passed to the SMTP client.
- **Passwords** are hashed with argon2id (m=19456 KiB, t=2, p=1, 32-byte key,
  16-byte salt) and verified with `subtle.ConstantTimeCompare`. Login performs
  a dummy verification when the account is missing or has no password method,
  to equalize timing.
- **Refresh tokens** are 256-bit randoms stored only as SHA-256 hashes. Strict
  rotation plus reuse detection means a stolen refresh token can be used at
  most once before the session — and all of the user's other sessions — are
  revoked.
- **Password reset revokes all sessions**, so a reset locks out any session an
  attacker may hold.
- **Enumeration safety**: forgot and unauthenticated resend return identical
  responses and run the rate limiter before looking the account up; login is
  uniform `invalid_credentials`. Register deliberately discloses existing
  emails (`409 user_exists`) — a documented tradeoff.
- **Verification and reset tokens** are stored hashed and are single-use
  (verify is atomic; reset is CAS). Raw tokens appear only in the email — and
  in development, in the log sender output.
- **Rate limiting** covers `resend-verification` and `password/forgot` (3 per
  hour per email). It is in-memory and single-instance; a shared store is
  needed once the server runs more than one replica. Login limiting is not
  implemented yet.
- **`purge_at` is never consulted for authorization** — validity always reads
  `expires_at` / `revoked_at` / `used_at` explicitly.
- **Google** ID tokens are verified offline against Google's discovery
  document and the configured client ID; the Google account email is never
  auto-linked to an existing local account, and Google's `email_verified`
  claim is not used for gating (the subject is the identity).

---

## Deferred and known limitations

- **Login rate limiting** (credential stuffing) — deferred; only resend and
  forgot are limited.
- **In-memory rate limiter** — single-instance assumption (above).
- **Manual code entry** — verification emails include a code, but no
  code-entry endpoint exists yet; `attempts_left` on the pending token is
  reserved for it and stays at 5.
- **`RequireVerifiedEmail` has no consumer yet** — future pairing and
  transfer endpoints will use it.
- **`users.device_ids` / `users.device_count`** are not maintained; device
  management will own them.
- **Password-change notification email** and a stored **session IP** were
  deliberately excluded.
