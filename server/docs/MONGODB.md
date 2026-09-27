# MongoDB Integration

This guide explains how the server talks to MongoDB, and how to build a
new resource (e.g. `devices`) on top of it. It complements
[ADDING_ENDPOINTS.md](./ADDING_ENDPOINTS.md), which covers the
handler/router side.

---

## Layout

```
server/
├── db/mongo/
│   ├── mongo.go     ← connection lifecycle: New, Ping, Disconnect, Database, Collection
│   └── errors.go    ← ErrNotFound, ErrConflict, TranslateError
└── repository/
    ├── repository.go ← generic Repository[T, PT] CRUD primitive
    └── index.go       ← EnsureIndexes + IndexProvider, called once at startup
```

`db/mongo` owns the connection. `repository` owns generic CRUD mechanics
on top of that connection. Neither package knows anything about a
specific resource (devices, transfers, clipboard entries, ...) — that
domain knowledge lives in per-resource packages that don't exist yet,
built the same way `handler/widgets` is described in
ADDING_ENDPOINTS.md.

---

## Wiring

`main.go` builds the client once, at startup, and passes it down
explicitly — the same dependency-injection pattern already used for
`cfg` and `logger`:

```go
mongoClient, err := esmongo.New(ctx, cfg)
// ...
mongoDB := esmongo.Database(mongoClient, cfg.MongoDatabase)
repository.EnsureIndexes(ctx, mongoDB /* , providers... */)
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

1. **Define the document type**, embedding `repository.Base` for the
   audit fields and `Document` interface:

   ```go
   // handler/devices/model.go (or a dedicated devices/ package)
   package devices

   import "github.com/cheng-alvin/esplanade/server/repository"

   type Device struct {
       repository.Base `bson:",inline"`

       Name   string `bson:"name"`
       Model  string `bson:"model"`
       UserID string `bson:"user_id"`
   }
   ```

2. **Instantiate a Repository** with the resource's collection:

   ```go
   repo := repository.New[Device](
       esmongo.Collection(mongoDB, "devices"),
       0, // 0 = repository.DefaultOperationTimeout
   )
   ```

3. **Add domain-specific queries** as functions in the resource package
   that compose `repo.Find` with a typed, internally-built filter —
   never a filter built from raw request input:

   ```go
   func (r *Repo) OnlineForUser(ctx context.Context, userID string, afterID string) (repository.FindResult[*Device], error) {
       return r.repo.Find(ctx, bson.M{"user_id": userID, "online": true}, afterID, 0)
   }
   ```

4. **Declare indexes** next to the resource, by implementing
   `repository.IndexProvider`, and register the provider in `main.go`'s
   `EnsureIndexes` call:

   ```go
   func (Device) CollectionName() string { return "devices" }

   func (Device) Indexes() []mongo.IndexModel {
       return []mongo.IndexModel{
           {Keys: bson.D{{Key: "user_id", Value: 1}}},
       }
   }
   ```

5. **Expose a narrow interface to handlers** (e.g. just `FindByID` and
   `Find`), not the concrete `*Repository[...]` type, so handler tests
   can mock exactly what they use.

---

## Guarantees the generic layer gives you

- **Consistent audit fields** — `InsertOne` sets `_id`, `created_at`,
  `updated_at` for you.
- **Soft deletes by default** — `DeleteOne` sets `deleted_at`; every
  read excludes soft-deleted documents automatically. Use `HardDelete`
  only when a literal, unrecoverable removal is actually required.
- **Cursor pagination** — `Find` pages on `_id`, never `skip`/`limit`,
  and caps page size at `repository.MaxPageSize` regardless of what's
  requested.
- **Injection safety** — `Find`/`Count`/`Exists` take a `bson.M` your
  package builds from typed parameters; never pass through a raw,
  client-controlled map.
- **Bounded operations** — every method applies its own timeout
  (`repository.DefaultOperationTimeout` by default), independent of the
  caller's request timeout.
- **Clean errors** — `esmongo.ErrNotFound` / `esmongo.ErrConflict` are
  the only Mongo-shaped errors that cross into the handler layer; map
  them to HTTP status codes with `respond.Error` (404 / 409).

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
