---
name: mongodb-data-access
description: >
  Work with MongoDB in the Esplanade Go backend server: build a new
  Mongo-backed resource (document struct, repository, indexes), query,
  insert, update, or delete data through the generic repository, or
  reason about pagination, soft deletes, error handling, or security
  guarantees for database code. Use whenever the user wants to store,
  query, update, or delete something in Mongo, add a new collection or
  resource with persistence, touch `db/mongo/`, `repository/`, or any
  per-resource package, or asks how the server talks to the database —
  including the upcoming auth resources (`users`, `auth_identities`,
  `sessions`, `email_verification_tokens`, `password_reset_tokens`).
  Prefer this skill over hand-rolling raw `mongo-driver` calls.

---

# MongoDB Data Access

This skill covers the data layer of the Esplanade Go backend server
(`server/`): how it connects to MongoDB, and how to build a new
resource (e.g. `devices`, or an auth resource like `sessions`) on top
of the generic repository that already exists. It complements
`.skills/add-server-endpoint`, which covers the handler/router side —
use both together when a new endpoint needs persistence.

For the full narrative version of this guide, see
`server/docs/MONGODB.md`. For the original design rationale (why a
generic repository, why cursor pagination, why soft deletes by
default), see the Canva doc "Esplanade Server — MongoDB Integration
Implementation Plan". This skill is the condensed, action-oriented
version of both — read them if you need more context than what's here.

## Prerequisites

- `db/mongo/` and `repository/` already exist and are wired into
  `main.go` — don't recreate connection or CRUD plumbing, compose on
  top of it.
- Go version per `server/go.mod` (currently 1.25.0), `mongo-driver/v2`.

## Layout

```
server/
├── db/mongo/
│   ├── mongo.go     ← connection lifecycle: New, Ping, Disconnect, Database, Collection
│   └── errors.go    ← ErrNotFound, ErrConflict, TranslateError
├── repository/
│   ├── repository.go ← generic Repository[T, PT] CRUD primitive
│   └── index.go       ← EnsureIndexes + IndexProvider, called once at startup
└── handler/<resource>/ ← where a resource's handlers live (see add-server-endpoint skill)
```

`db/mongo` owns the connection. `repository` owns generic CRUD
mechanics on top of that connection. Neither package knows anything
about a specific resource — that domain knowledge lives in a
per-resource package (e.g. `handler/devices/`, or a dedicated
`devices/` package) that composes the generic layer with its own
document type and typed queries.

The connection itself is already wired up in `main.go`: a
`*mongo.Client` is built once at startup via `esmongo.New(ctx, cfg)`,
handed to `repository.EnsureIndexes` and `router.New`, and closed via
`esmongo.Disconnect` in the shutdown sequence. You shouldn't need to
touch this wiring unless you're changing connection behavior itself —
for a new resource, you're extending the `EnsureIndexes(...)` call
with a new provider and threading a repository into a handler, not
rebuilding the client.

## Adding a new Mongo-backed resource

This is the main workflow. Say the user asks to persist something new
— e.g. "add a sessions collection" or "store password reset tokens in
Mongo."

### 1. Define the document type

Embed `repository.Base` for the audit fields (`_id`, `created_at`,
`updated_at`, `deleted_at`) — this is what satisfies the `Document`
interface (`SetID`/`GetID`/`SetCreatedAt`/`SetUpdatedAt`) automatically,
so you never hand-roll it:

```go
// handler/devices/model.go
package devices

import "github.com/cheng-alvin/esplanade/server/repository"

type Device struct {
    repository.Base `bson:",inline"`

    Name   string `bson:"name"`
    Model  string `bson:"model"`
    UserID string `bson:"user_id"`
}
```

### 2. Instantiate a Repository

```go
repo := repository.New[Device](
    esmongo.Collection(mongoDB, "devices"),
    0, // 0 = repository.DefaultOperationTimeout (5s)
)
```

`repository.New[Device](...)` infers the pointer type parameter from
`Device` automatically — you don't need to write `New[Device, *Device]`.

### 3. Add domain-specific queries

Compose `repo.Find` (or `Count`/`Exists`) with a **typed, internally-built
filter** — never a filter assembled from raw request input. This is
the main NoSQL-injection boundary in this codebase, so it's worth
treating as a hard rule rather than a style preference:

```go
func (r *Repo) OnlineForUser(ctx context.Context, userID string) ([]*Device, error) {
    return r.repo.Find(ctx, bson.M{"user_id": userID, "online": true})
}

func (r *Repo) OnlineForUserPage(ctx context.Context, userID string, afterID string, pageSize int) (repository.FindResult[*Device], error) {
    return r.repo.FindPage(ctx, bson.M{"user_id": userID, "online": true}, afterID, pageSize)
}
```

### 4. Declare indexes next to the resource

```go
func (Device) CollectionName() string { return "devices" }

func (Device) Indexes() []mongo.IndexModel {
    return []mongo.IndexModel{
        {Keys: bson.D{{Key: "user_id", Value: 1}}},
    }
}
```

Then register the provider in `main.go`'s `repository.EnsureIndexes`
call (currently called with no providers, since no resource exists
yet):

```go
err = repository.EnsureIndexes(indexCtx, mongoDB, devices.Device{})
```

Keeping index definitions in the resource package (not a separate
migrations file) is deliberate — it keeps schema and access code from
drifting apart.

### 5. Expose a narrow interface to the handler, not the concrete repository

A handler should depend on an interface with just the methods it
actually calls (e.g. `FindByID`, `Find`, or `FindPage`), not the full
`*Repository[Device, *Device]` type — that's what lets handler tests
mock exactly what they use, rather than standing up a real Mongo
instance for every test.

### 6. Wire the repository into the handler package

`router.New` currently takes a `*mongo.Client` and closes over it for
`/readyz` (see `readyz(mongoClient)` in `router/router.go`) — follow
that same closure pattern for a resource's handlers rather than
reaching for a package-level global:

```go
// handler/devices/devices.go
func List(repo Finder) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // repo.Find(...), respond.JSON(...)
    }
}
```

```go
// router/router.go
mux.HandleFunc("GET /api/v1/devices", devices.List(devicesRepo))
```

Building `devicesRepo` (step 2) happens in `main.go`, alongside the
existing `mongoClient`/`mongoDB` setup, and gets passed down to
`router.New` the same way `mongoClient` already is.

## Repository primitive reference

Every method takes `context.Context` first and applies its own bounded
timeout (`repository.DefaultOperationTimeout`, 5s by default) —
independent of the caller's request timeout, so a slow query can't hold
a connection open for the full request lifetime.

| Method | Does | Guarantee |
|---|---|---|
| `InsertOne(ctx, doc)` | Insert a new document | Populates `_id`, `created_at`, `updated_at` for you |
| `FindByID(ctx, id)` | Look up by hex ObjectID | Returns `esmongo.ErrNotFound` for both a malformed ID and no match — never a raw driver error or panic |
| `Find(ctx, filter)` | Query all matching documents | Standard non-paginated fetch of all non-deleted matches |
| `FindPage(ctx, filter, afterID, pageSize)` | Query a page of documents | Explicit cursor-based pagination (`_id > afterID`, sorted ascending); `pageSize` is capped at `MaxPageSize` (200) |
| `UpdateOne(ctx, id, fields)` | Partial update | Applied via `$set`, never a whole-document replacement; bumps `updated_at`; returns `ErrNotFound` on no match rather than silently no-op'ing |
| `DeleteOne(ctx, id)` | Soft delete | Sets `deleted_at`; every read/update above excludes soft-deleted documents automatically |
| `HardDelete(ctx, id)` | Permanent delete | Deliberately separate from `DeleteOne` so an unrecoverable removal is never invoked by accident — only use it when a literal, permanent erasure is actually required (e.g. a user data-erasure request) |
| `Count(ctx, filter)` / `Exists(ctx, filter)` | Aggregate checks | Use these instead of `Find` + a length check when only an aggregate result is needed |

`FindPage` returns a `FindResult[PT]{Items, NextCursor}`. `NextCursor`
is the hex ID to pass as `afterID` for the next page, and is empty
when there is no further page. Use `Find` by default; call `FindPage`
only when the caller explicitly needs pagination.

**Default to `DeleteOne`.** A soft delete gives an audit trail and a
recovery path, which is usually right for anything tied to user data or
sync state. Reach for `HardDelete` only when permanence is the actual
requirement, not the default.

## Error handling

`db/mongo/errors.go` defines two sentinels — `esmongo.ErrNotFound` and
`esmongo.ErrConflict` (duplicate key) — and `esmongo.TranslateError`
maps the raw driver errors into them. The repository layer already
calls `TranslateError` internally, so a handler calling into a
repository just needs to check the sentinels and map them to HTTP
status codes with `respond.Error`:

```go
device, err := repo.FindByID(ctx, id)
if errors.Is(err, esmongo.ErrNotFound) {
    respond.Error(w, http.StatusNotFound, "device not found")
    return
}
```

Never let a raw `mongo.CommandError` or similar reach the handler layer
or a response body — that leaks driver internals (and sometimes index
names) to the client.

## Security rules

These are non-negotiable, not stylistic preferences:

- **Never pass a client-controlled map into a Mongo filter.** Build
  filters from typed parameters inside the resource package (see step
  3 above) — this is the primary NoSQL-injection vector in Go Mongo
  code.
- **`MongoURI` has no default and must come from `ESPLANADE_MONGO_URI`
  or a secret store** — never hardcode it or add a real value to
  `config.yaml.example`.
- **`esmongo.New` refuses to start in `production` without TLS**
  (`mongodb+srv://` or an explicit `tls=true`/`ssl=true`). Don't work
  around this check.
- The DB user in the connection string should have `readWrite` scoped
  to the single application database — never a cluster-wide or admin
  role.
- Never log full document bodies on error — follow the same discipline
  as `middleware.Logging`, to avoid leaking PII into logs.

## Anti-patterns to avoid

- Reimplementing connection setup, timeouts, or CRUD boilerplate
  per-resource instead of composing `repository.Repository[T, PT]`.
- A package-level global `*mongo.Client` or repository instead of
  explicit dependency injection through `main.go` → `router.New` →
  handler constructor (matches how `cfg` and `logger` already flow).
- Skip/limit pagination instead of the cursor-based pattern `FindPage`
  already implements.
- Calling `HardDelete` where `DeleteOne` (soft delete) was actually
  called for.
- Letting a raw driver error or `bson.M` filter cross from a resource
  package into the handler layer.

## Verify

After writing the code:

```bash
cd server && go vet ./...
```

If you added an `IndexProvider`, also confirm it's registered in
`main.go`'s `repository.EnsureIndexes(...)` call — an unregistered
provider silently does nothing.
