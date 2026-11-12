---
name: mongodb-data-access
description: >
  Work with MongoDB in the Esplanade Go backend server: build a new
  Mongo-backed resource (document struct, queries, indexes), query,
  insert, update, or delete data with the shared `db/mongo` primitives
  or the mongo-driver, or reason about audit fields, soft deletes, TTL
  expiry, error handling, or security guarantees for database code. Use
  whenever the user wants to store, query, update, or delete something
  in Mongo, add a new collection or resource with persistence, touch
  `db/mongo/` or any per-resource package, or asks how the server talks
  to the database — including the auth resources (`users`,
  `auth_identities`, `sessions`, `email_verification_tokens`,
  `password_reset_tokens`). Persistence code builds on the thin
  `Manager` and `Base` primitives; do not introduce a heavier
  repository layer.
---

# MongoDB Data Access

This skill covers the data layer of the Esplanade Go backend server
(`server/`): how it connects to MongoDB, and how to build a new
resource (e.g. `devices`, or an auth resource like `sessions`) on top
of the shared primitives in `db/mongo/`. It complements
`.skills/add-server-endpoint`, which covers the handler/router side —
use both together when a new endpoint needs persistence.

Persistence code stays close to the driver: `esmongo.Manager` wraps one
collection with the common CRUD operations and `esmongo.Base` carries
the shared document fields, while anything the primitives do not cover
goes through the raw `*mongo.Collection` the Manager exposes. Do not
grow a heavier per-resource repository layer.

## Prerequisites

- `db/mongo/` already exists and is wired into `main.go` — don't
  recreate connection, error-translation, or primitive plumbing
  (`Manager`, `Base`), compose on top of it.
- Go version per `server/go.mod`, `mongo-driver/v2`.

## Layout

```
server/
├── db/mongo/
│   ├── mongo.go     ← connection lifecycle: New, Ping, Disconnect, Database, Collection
│   ├── manager.go   ← Manager: the common CRUD primitives over one collection
│   ├── base.go      ← Base: the shared document fields, embeddable inline
│   └── errors.go    ← ErrNotFound, ErrConflict, ErrInvalidID, TranslateError
└── <resource>/      ← per-resource document types, queries, and indexes
                      (e.g. devices/, auth/repository/)
```

`db/mongo` owns the connection, shared error translation, and the thin
primitives. None of it knows anything about a specific resource — that
domain knowledge lives in a per-resource package, which declares its
document types, typed queries, and indexes and uses an
`*esmongo.Manager` (or the raw `*mongo.Collection` it exposes) to talk
to Mongo.

The connection itself is already wired up in `main.go`: a
`*mongo.Client` is built once at startup via `esmongo.New(ctx, cfg)`,
handed to `router.New`, and closed via `esmongo.Disconnect` in the
shutdown sequence. You shouldn't need to touch this wiring unless
you're changing connection behavior itself. Startup also calls
`CreateMany` for each resource's declared indexes (see step 4).

## Adding a new Mongo-backed resource

This is the main workflow. Say the user asks to persist something new
— e.g. "add a sessions collection" or "store password reset tokens in
Mongo."

### 1. Define the document type

Embed `esmongo.Base` to pick up `_id`, `created_at`, `updated_at`,
`deleted_at`, and `purge_at`. The BSON codec does not flatten embedded
structs implicitly, so the `,inline` tag is required:

```go
// handler/devices/model.go
package devices

import esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"

type Device struct {
    esmongo.Base `bson:",inline"`

    Name   string `bson:"name"`
    Model  string `bson:"model"`
    UserID string `bson:"user_id"`
}
```

Embedding is optional. A lightweight resource declares only the fields
its schema needs — deletes are then permanent and there is no TTL — and
a document with a different persisted shape implements just the setters
`Manager.InsertOne` applies (`SetID`, `SetCreatedAt`, `SetUpdatedAt`).

### 2. Hold an `esmongo.Manager`, call its primitives

Give the resource's store (or data-layer package) an
`*esmongo.Manager` field — built in `main.go` via
`esmongo.Collection(mongoDB, "<collection>")` and passed in:

```go
type Store struct{ devices *esmongo.Manager }

func NewStore(collection *mongo.Collection) *Store {
    return &Store{devices: esmongo.NewManager(collection)}
}
```

`InsertOne` fills `_id` and the audit timestamps in for
`Base`-embedding documents, so an insert is one call:

```go
func (s *Store) Insert(ctx context.Context, device *Device) error {
    return s.devices.InsertOne(ctx, device)
}
```

### 3. Add domain-specific queries with typed filters

Build every filter from typed parameters inside the resource package —
never a filter assembled from raw request input. This is the main
NoSQL-injection boundary in this codebase, so it's worth treating as a
hard rule rather than a style preference:

```go
func (s *Store) OnlineForUser(ctx context.Context, userID string) ([]*Device, error) {
    items := make([]*Device, 0)
    if err := s.devices.FindMany(ctx, bson.M{"user_id": userID, "online": true}, &items); err != nil {
        return nil, err
    }
    return items, nil
}
```

For a single document, use `FindOne`/`FindByID`; a miss surfaces as
`esmongo.ErrNotFound`. When a string ID goes into a filter of your own,
convert it with `esmongo.ParseObjectID` (malformed input returns
`esmongo.ErrInvalidID`). For cursor pagination, pass
`options.Find().SetSort(...).SetLimit(...)` through `FindMany` and use a
`_id > afterID` filter — never skip/limit.

### 4. Manage audit fields, soft deletes, and TTLs in writes

Audited resources follow these rules:

- on insert, `Manager.InsertOne` fills `_id` and `created_at` /
  `updated_at` for you;
- on update, `$set` `updated_at` alongside the changed fields in the
  caller-built update document;
- on delete, soft delete by `$set`-ing `deleted_at` (and
  `updated_at`) instead of removing the document;
- on every read and update, exclude soft-deleted documents with
  `"deleted_at": bson.M{"$exists": false}` in your filters.

`Manager.UpdateOne` and `Manager.DeleteOne` return
`esmongo.ErrNotFound` when nothing matched; the batch variants report a
count instead.

For TTL-based expiry, store the expiry time on the document (e.g.
`purge_at`) and declare the TTL on its index:

```go
func (Device) CollectionName() string { return "devices" }

func (Device) Indexes() []mongo.IndexModel {
    return []mongo.IndexModel{
        {Keys: bson.D{{Key: "user_id", Value: 1}}},
        {Keys: bson.D{{Key: "purge_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)},
    }
}
```

Then register the collection in `main.go`'s startup index loop, which
calls `mongoDB.Collection(name).Indexes().CreateMany(ctx, models)` for
each entry. Keeping index definitions in the resource package (not a
separate migrations file) is deliberate — it keeps schema and access
code from drifting apart.

### 5. Expose a narrow interface to the handler, not the concrete store

A handler should depend on an interface with just the methods it
actually calls (e.g. `FindByID`, `FindMany`), so handler tests can mock
exactly what they use, rather than standing up a real Mongo instance
for every test.

### 6. Wire the store into the handler package

`router.New` takes dependencies explicitly (e.g. `mongoClient` for
`/readyz`) — follow that same constructor-closure pattern for a
resource's handlers rather than reaching for a package-level global:

```go
// handler/devices/devices.go
func List(store Finder) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // store.FindMany(...), respond.JSON(...)
    }
}
```

```go
// router/router.go
mux.HandleFunc("GET /api/v1/devices", devices.List(devicesStore))
```

Building `devicesStore` happens in `main.go`, alongside the existing
`mongoClient`/`mongoDB` setup, and gets passed down to `router.New` the
same way `mongoClient` already is.

## Error handling

`db/mongo/errors.go` defines the sentinels — `esmongo.ErrNotFound`,
`esmongo.ErrConflict` (duplicate key), and `esmongo.ErrInvalidID`
(malformed ID) — and `esmongo.TranslateError` maps raw driver errors
into them. `Manager` methods translate for you; code still using the
raw collection calls `TranslateError` itself. A handler just checks the
sentinels and maps them to HTTP status codes with `respond.Error`:

```go
device, err := store.FindByID(ctx, id)
if errors.Is(err, esmongo.ErrNotFound) {
    respond.Error(w, http.StatusNotFound, "device not found")
    return
}
```

Never let a raw `mongo.CommandError` or similar reach the handler layer
or a response body — that leaks driver internals (and sometimes index
names) to the client.

This per-package ownership is the repo-wide convention for sentinel
errors: each sentinel is declared by the package that owns the behaviour
it represents (as `db/mongo` does here, and `auth.ErrInvalidCredentials`
does for auth), never in a central errors package.

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

- Growing a repository layer around the primitives: no
  domain-specific methods on `Manager`, and no per-resource wrappers
  whose only job is to repeat a single driver call — callers use the
  primitives or the raw collection directly.
- A package-level global `*mongo.Client` or collection instead of
  explicit dependency injection through `main.go` → `router.New` →
  handler constructor (matches how `cfg` and `logger` already flow).
- Skip/limit pagination instead of a `_id`-cursor filter.
- Hard-deleting an audited document where a soft delete (setting
  `deleted_at`) was actually called for.
- Letting a raw driver error or `bson.M` filter cross from a resource
  package into the handler layer.

## Verify

After writing the code:

```bash
cd server && go vet ./...
```

If you added a collection, also confirm it's registered in `main.go`'s
startup index loop — an unregistered collection's `Indexes()` output is
silently never applied.
