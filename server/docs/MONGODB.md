# MongoDB Integration

This guide explains how the server talks to MongoDB, and how to build a
new resource (e.g. `devices`) against it. It complements
[ADDING_ENDPOINTS.md](./ADDING_ENDPOINTS.md), which covers the
handler/router side.

---

## Layout

```
server/
├── db/mongo/
│   ├── mongo.go     ← connection lifecycle: New, Ping, Disconnect, Database, Collection
│   └── errors.go    ← ErrNotFound, ErrConflict, TranslateError
└── <resource>/      ← a resource package owns its documents, indexes, and queries
```

`db/mongo` owns the connection lifecycle and the shared error
translation. Resource packages (e.g. `server/devices/`) own their
document types, typed queries, and index definitions, and call the
[mongo-driver](https://pkg.go.dev/go.mongodb.org/mongo-driver/v2) APIs
on the collections they are given. There is no generic CRUD layer in
between — persistence code is plain driver calls.

---

## Wiring

`main.go` builds the client once, at startup, and passes it down
explicitly — the same dependency-injection pattern already used for
`cfg` and `logger`:

```go
mongoClient, err := esmongo.New(ctx, cfg)
// ...
mongoDB := esmongo.Database(mongoClient, cfg.MongoDatabase)

indexCtx, indexCancel := context.WithTimeout(context.Background(), 5*time.Second)
for _, ix := range indexSpecs { // {collection, models} pairs from the resource packages
    if _, err := mongoDB.Collection(ix.collection).Indexes().CreateMany(indexCtx, ix.models); err != nil {
        logger.Fatal("ensuring mongo indexes", zap.String("collection", ix.collection), zap.Error(err))
    }
}
indexCancel()

mux := router.New(mongoClient)
```

`esmongo.Disconnect` is called from the existing shutdown block in
`main.go`, alongside `srv.Shutdown`, so Mongo closes cleanly on
SIGINT/SIGTERM.

`router.New` wires `mongoClient` into `GET /readyz`, which pings Mongo
with a short, bounded timeout — distinct from `GET /healthz`, which is a
pure liveness check with no downstream dependency.

---

## Adding a new resource

1. **Define the document type** with the audit fields its schema needs.
   An audit-aware resource carries `_id`, `created_at`, `updated_at`,
   and `deleted_at` directly on the struct:

   ```go
   // handler/devices/model.go (or a dedicated devices/ package)
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

2. **Query the collection directly.** Give the resource's store (or
   data-layer package) a `*mongo.Collection` and call the driver:

   ```go
   type Store struct{ collection *mongo.Collection }

   func (s *Store) Insert(ctx context.Context, entry *LogEntry) error {
       entry.ObjectID = bson.NewObjectID()
       _, err := s.collection.InsertOne(ctx, entry)
       return esmongo.TranslateError(err)
   }
   ```

3. **Add domain-specific queries** with a typed, internally-built
   filter — never a filter assembled from raw request input. This is
   the main NoSQL-injection boundary in this codebase, so it's worth
   treating as a hard rule rather than a style preference:

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

4. **Manage audit fields and soft deletes in the resource's writes.**
   For an audited resource:

   - on insert, set `ObjectID = bson.NewObjectID()` and
     `CreatedAt`/`UpdatedAt` to `time.Now().UTC()`;
   - on update, `$set` `updated_at` alongside the changed fields;
   - on delete, soft delete by setting `deleted_at` (and `updated_at`)
     instead of removing the document;
   - on every read and update, exclude soft-deleted documents with
     `"deleted_at": bson.M{"$exists": false}`.

   ```go
   func (s *Store) SoftDelete(ctx context.Context, id string) error {
       oid, err := bson.ObjectIDFromHex(id)
       if err != nil {
           return esmongo.ErrNotFound
       }
       now := time.Now().UTC()
       res, err := s.collection.UpdateOne(ctx,
           bson.M{"_id": oid, "deleted_at": bson.M{"$exists": false}},
           bson.M{"$set": bson.M{"deleted_at": now, "updated_at": now}})
       if err != nil {
           return esmongo.TranslateError(err)
       }
       if res.MatchedCount == 0 {
           return esmongo.ErrNotFound
       }
       return nil
   }
   ```

   For TTL-based expiry, store the expiry time on the document (e.g.
   `purge_at`) and set the TTL on its index (see step 5).

5. **Declare indexes next to the resource**, then ensure them from
   `main.go`'s startup loop.

   ```go
   func (Device) CollectionName() string { return "devices" }

   func (Device) Indexes() []mongo.IndexModel {
       return []mongo.IndexModel{
           {Keys: bson.D{{Key: "user_id", Value: 1}}},
           // TTL: Mongo removes a document once purge_at passes.
           {Keys: bson.D{{Key: "purge_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)},
       }
   }
   ```

   Keeping indexes in the resource package (not a separate migrations
   file) is deliberate — it keeps schema and access code from drifting
   apart. Register the collection's `Indexes()` output in the startup
   loop in `main.go`, which calls `CreateMany` for each collection.

6. **Expose a narrow interface to handlers** (e.g. just `FindByID`,
   `Find`), not the concrete store type, so handler tests can mock
   exactly what they use.

---

## Guarantees to carry into resource code

The driver does not provide these for you — they are conventions this
codebase follows:

- **Consistent audit fields** — `_id`, `created_at`, and `updated_at`
  are set by the resource's insert path; updates bump `updated_at`.
- **Soft deletes** — audited resources set `deleted_at` instead of
  deleting, and exclude soft-deleted documents in every read and
  update filter.
- **TTL expiry** — time-limited documents store an expiry field and
  declare a `SetExpireAfterSeconds(0)` index on it.
- **Injection safety** — build filters from typed parameters inside
  the resource package; never pass through a raw, client-controlled
  map.
- **Clean errors** — `esmongo.ErrNotFound` / `esmongo.ErrConflict` are
  the only Mongo-shaped errors that cross into the handler layer; map
  them to HTTP status codes with `respond.Error` (404 / 409). Route
  raw driver errors through `esmongo.TranslateError`.

---

## Security

- `MongoURI` is never given a real default and must come from
  `ESPLANADE_MONGO_URI` or a secret store — see the comment in
  `config.yaml.example`.
- In `env: production`, `esmongo.New` refuses to start unless the URI
  is TLS-enabled (`mongodb+srv://` or an explicit `tls=true`).
- The DB user in the connection string should have `readWrite` scoped
  to the single application database — never a cluster-wide or admin
  role.
- Mongo command logging should follow the same discipline as
  `middleware.Logging`: never log full document bodies on error, to
  avoid leaking PII into logs.
