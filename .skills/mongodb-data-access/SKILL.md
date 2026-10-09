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
generic repository, why cursor pagination, and why audited resources use
soft deletes by default), see the Canva doc "Esplanade Server — MongoDB
Integration Implementation Plan". This skill is the condensed,
action-oriented version of both — read them if you need more context than
what's here.

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
│   ├── primitives.go ← shared types, generic CRUD primitives, and primitive-adjacent API methods
│   ├── standard.go   ← lightweight Repository[T, PT] struct and New
│   ├── audited.go    ← AuditedRepository[T, PT], NewAudited, and audited document types
│   └── index.go      ← EnsureIndexes + IndexProvider, called once at startup
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

For an audit-aware resource, embed `repository.Base` for the audit fields
(`_id`, `created_at`, `updated_at`, `deleted_at`). It satisfies the
`Auditable` interface, which embeds `Document`, so it supplies both the
`SetID`/`ID` and `SetCreatedAt`/`SetUpdatedAt` methods automatically. A
lightweight resource only needs to implement `SetID` and `ID` and can omit
`Base`:

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

### 2. Choose a repository mode

Use the audited repository for resources that embed `Base`:

```go
repo := repository.NewAudited[Device](
    esmongo.Collection(mongoDB, "devices"),
    0, // 0 = repository.DefaultOperationTimeout (5s)
)
```

Use the lightweight repository for resources that do not need audit fields or
soft deletes:

```go
repo := repository.New[LogEntry](
    esmongo.Collection(mongoDB, "log_entries"),
    0,
)
```

Both constructors infer the pointer type parameter automatically — you don't
need to write `New[Device, *Device]` or `NewAudited[Device, *Device]`.

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
`*Repository[Device, *Device]` or `*AuditedRepository[Device, *Device]`
type — that's what lets handler tests mock exactly what they use, rather
than standing up a real Mongo instance for every test.

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

Keep shared repository mechanics and the public adapter methods in
`primitives.go`, colocated with the base primitive each method delegates to.
Keep the `Repository` and `AuditedRepository` structs and their constructors
in their respective files, along with the audited document types.

Every method takes `context.Context` first and applies its own bounded
timeout (`repository.DefaultOperationTimeout`, 5s by default) —
independent of the caller's request timeout, so a slow query can't hold
a connection open for the full request lifetime.

`Repository` and `AuditedRepository` expose the same CRUD surface, with
slightly different write and filtering guarantees:

| Method                                       | Does                         | Lightweight `Repository`                                                  | `AuditedRepository`                                        |
| -------------------------------------------- | ---------------------------- | ------------------------------------------------------------------------- | ---------------------------------------------------------- |
| `InsertOne(ctx, doc)`                        | Insert a new document        | Populates `_id`                                                           | Populates `_id`, `created_at`, and `updated_at`            |
| `FindByID(ctx, id)`                          | Look up by hex ObjectID      | Returns `esmongo.ErrNotFound` for malformed IDs and misses                | Same, excluding soft-deleted documents                     |
| `Find(ctx, filter)`                          | Query all matching documents | Standard non-paginated fetch                                              | Fetch excludes soft-deleted documents                      |
| `FindPage(ctx, filter, afterID, pageSize)`   | Query a page                 | Cursor-based (`_id > afterID`, sorted ascending), capped at `MaxPageSize` | Same, excluding soft-deleted documents                     |
| `UpdateOne(ctx, id, fields)`                 | Partial update via `$set`    | Does not modify audit fields                                              | Bumps `updated_at`; returns `ErrNotFound` on no match      |
| `DeleteOne(ctx, id)`                         | Delete a document            | Permanent delete                                                          | Soft delete via `deleted_at`; reads and updates exclude it |
| `HardDelete(ctx, id)`                        | Permanent delete             | Same as `DeleteOne`                                                       | Bypasses the soft-delete marker                            |
| `Count(ctx, filter)` / `Exists(ctx, filter)` | Aggregate checks             | Counts matching documents                                                 | Counts matching non-deleted documents                      |

Both variants return translated Mongo errors and apply the bounded operation
timeout. Filters must still be built from typed parameters inside the resource
package, never passed through directly from client input.

`FindPage` returns a `FindResult[PT]{Items, NextCursor}`. `NextCursor`
is the hex ID to pass as `afterID` for the next page, and is empty
when there is no further page. Use `Find` by default; call `FindPage`
only when the caller explicitly needs pagination.

**For audited repositories, default to `DeleteOne`.** A soft delete gives
an audit trail and a recovery path, which is usually right for anything tied
to user data or sync state. Reach for `HardDelete` only when permanence is
the actual requirement, not the default. In a lightweight repository,
`DeleteOne` is already permanent because no soft-delete contract is assumed.

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
  per-resource instead of composing `repository.Repository[T, PT]` or
  `repository.AuditedRepository[T, PT]`.
- A package-level global `*mongo.Client` or repository instead of
  explicit dependency injection through `main.go` → `router.New` →
  handler constructor (matches how `cfg` and `logger` already flow).
- Skip/limit pagination instead of the cursor-based pattern `FindPage`
  already implements.
- Calling `HardDelete` on an audited repository where `DeleteOne`
  (soft delete) was actually called for.
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
