---
name: mongodb-data-access
description: >
  Work with MongoDB in the Esplanade Go backend server: build a new
  Mongo-backed resource (document struct, queries, indexes), query,
  insert, update, or delete data with the mongo-driver, or reason about
  audit fields, soft deletes, TTL expiry, error handling, or security
  guarantees for database code. Use whenever the user wants to store,
  query, update, or delete something in Mongo, add a new collection or
  resource with persistence, touch `db/mongo/` or any per-resource
  package, or asks how the server talks to the database — including the
  auth resources (`users`, `auth_identities`, `sessions`,
  `email_verification_tokens`, `password_reset_tokens`).
  Persistence code calls the driver directly; do not introduce a
  generic repository layer.
---

# MongoDB Data Access

This skill covers the data layer of the Esplanade Go backend server
(`server/`): how it connects to MongoDB, and how to build a new
resource (e.g. `devices`, or an auth resource like `sessions`) by
calling the mongo-driver directly. It complements
`.skills/add-server-endpoint`, which covers the handler/router side —
use both together when a new endpoint needs persistence.

For the full narrative version of this guide, see
`server/docs/MONGODB.md`. Persistence code in this codebase is plain
driver calls — there is no generic CRUD/repository layer, and new code
should not add one.

## Prerequisites

- `db/mongo/` already exists and is wired into `main.go` — don't
  recreate connection or error-translation plumbing, compose on top of
  it.
- Go version per `server/go.mod`, `mongo-driver/v2`.

## Layout

```
server/
├── db/mongo/
│   ├── mongo.go     ← connection lifecycle: New, Ping, Disconnect, Database, Collection
│   └── errors.go    ← ErrNotFound, ErrConflict, TranslateError
└── <resource>/      ← per-resource document types, queries, and indexes
                      (e.g. devices/, auth/repository/)
```

`db/mongo` owns the connection and shared error translation. Neither it
nor any other shared package knows anything about a specific resource —
that domain knowledge lives in a per-resource package that holds a
`*mongo.Collection` and issues driver calls itself.

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

For an audit-aware resource, carry the audit fields directly on the
struct — `_id`, `created_at`, `updated_at`, and `deleted_at`:

```go
// handler/devices/model.go
package devices

import "go.mongodb.org/mongo-driver/v2/bson"

type Device struct {
    ObjectID  bson.ObjectID `bson:"_id,omitempty"`
    CreatedAt time.Time     `bson:"created_at"`
    UpdatedAt time.Time     `bson:"updated_at"`
    DeletedAt *time.Time    `bson:"deleted_at,omitempty"`

    Name   string `bson:"name"`
    Model  string `bson:"model"`
    UserID string `bson:"user_id"`
}
```

A lightweight resource only needs `_id` plus the fields its schema
requires — no audit fields, and deletes are permanent:

```go
type LogEntry struct {
    ObjectID bson.ObjectID `bson:"_id,omitempty"`
    Message  string        `bson:"message"`
}
```

### 2. Hold a collection, query it directly

Give the resource's store (or data-layer package) a
`*mongo.Collection` field — built in `main.go` via
`esmongo.Collection(mongoDB, "<collection>")` and passed in:

```go
type Store struct{ collection *mongo.Collection }

func NewStore(collection *mongo.Collection) *Store {
    return &Store{collection: collection}
}
```

Insert paths set `_id` and, for audited resources, the audit
timestamps:

```go
func (s *Store) Insert(ctx context.Context, entry *LogEntry) error {
    entry.ObjectID = bson.NewObjectID()
    _, err := s.collection.InsertOne(ctx, entry)
    return esmongo.TranslateError(err)
}
```

### 3. Add domain-specific queries with typed filters

Build every filter from typed parameters inside the resource package —
never a filter assembled from raw request input. This is the main
NoSQL-injection boundary in this codebase, so it's worth treating as a
hard rule rather than a style preference:

```go
func (s *Store) OnlineForUser(ctx context.Context, userID string) ([]*Device, error) {
    cursor, err := s.collection.Find(ctx, bson.M{"user_id": userID, "online": true})
    if err != nil {
        return nil, esmongo.TranslateError(err)
    }
    items := make([]*Device, 0)
    if err := cursor.All(ctx, &items); err != nil {
        return nil, esmongo.TranslateError(err)
    }
    return items, nil
}
```

For a single document, use `FindOne(...).Decode(&doc)`; a miss
surfaces as `mongo.ErrNoDocuments`, which `esmongo.TranslateError`
maps to `esmongo.ErrNotFound`. For cursor pagination, use
`options.Find().SetSort(...).SetLimit(...)` and a `_id > afterID`
filter — never skip/limit.

### 4. Manage audit fields, soft deletes, and TTLs in writes

Audited resources follow these rules:

- on insert, set `_id` (`bson.NewObjectID()`) and `created_at` /
  `updated_at` (`time.Now().UTC()`);
- on update, `$set` `updated_at` alongside the changed fields;
- on delete, soft delete by `$set`-ing `deleted_at` (and
  `updated_at`) instead of removing the document;
- on every read and update, exclude soft-deleted documents with
  `"deleted_at": bson.M{"$exists": false}`.

Return `esmongo.ErrNotFound` when a matched update (`MatchedCount ==
0`) or lookup misses.

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
actually calls (e.g. `FindByID`, `Find`), so handler tests can mock
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
        // store.Find(...), respond.JSON(...)
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

`db/mongo/errors.go` defines two sentinels — `esmongo.ErrNotFound` and
`esmongo.ErrConflict` (duplicate key) — and `esmongo.TranslateError`
maps the raw driver errors into them. Resource code calls
`TranslateError` on driver errors, so a handler just checks the
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

- Reintroducing a generic repository/CRUD layer, or a per-resource
  abstraction whose only job is to wrap driver calls in one more
  method — persistence code calls the driver directly.
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
