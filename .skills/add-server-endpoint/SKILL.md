---
name: add-server-endpoint
description: >
  Add a new HTTP endpoint to the Esplanade Go backend server (server/).
  Use whenever the user wants to create, add, or wire up an API route,
  REST endpoint, or HTTP handler — including when they just describe a
  resource needing HTTP access (e.g. "devices need a list endpoint",
  "let the client fetch transfer status") without using the words
  "endpoint" or "route" explicitly. If the endpoint also needs to read
  or write data, pair this with the mongodb-data-access skill for the
  persistence side.
---

# Add Server Endpoint

This skill adds new HTTP API endpoints to the Esplanade Go backend
server located at `server/`. The project follows a two-file pattern:
**create a handler, register it in the router.** If the endpoint
touches the database, see `.skills/mongodb-data-access` for how to
build and wire in a repository — this skill covers the HTTP surface
only.

## Prerequisites

- The `server/` directory exists with the boilerplate already set up.
- Go 1.22+ (required for the enhanced `net/http` `ServeMux` routing
  used here — path parameters, per-method patterns). Check
  `server/go.mod` for the exact version this module currently targets.

## Steps

### 1. Determine the resource and operations

Identify from the user's request:
- **Resource name** (e.g. "users", "widgets") — always plural, lowercase.
- **Operations** needed (List, Create, GetByID, Update, Delete).
- **API version** — default to `v1`.

### 2. Create the handler package

Create a new file at:

```
server/handler/<resource>/<resource>.go
```

Each handler function must have the signature:

```go
func HandlerName(w http.ResponseWriter, r *http.Request)
```

Use these imports for responses:

```go
import "github.com/cheng-alvin/esplanade/server/respond"
```

- `respond.JSON(w, statusCode, data)` — write a JSON success response.
- `respond.Error(w, statusCode, "message")` — write a JSON error response.

Extract path parameters with `r.PathValue("paramName")`.

Decode JSON request bodies with:

```go
var input MyStruct
if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
    respond.Error(w, http.StatusBadRequest, "invalid JSON")
    return
}
```

If a handler needs to read or write data, don't reach into the
database driver directly — build a repository for the resource first
(`.skills/mongodb-data-access`) and have the handler depend on that
repository, injected as shown in step 3.

### 3. Register routes in the router

Open `server/router/router.go`.

1. Add an import for the new handler package:

   ```go
   "github.com/cheng-alvin/esplanade/server/handler/<resource>"
   ```

2. Inside the `New()` function, add `mux.HandleFunc` calls alongside
   the existing ones, before `return mux`:

   ```go
   mux.HandleFunc("GET /api/v1/<resource>", <resource>.List)
   mux.HandleFunc("POST /api/v1/<resource>", <resource>.Create)
   mux.HandleFunc("GET /api/v1/<resource>/{id}", <resource>.GetByID)
   mux.HandleFunc("PUT /api/v1/<resource>/{id}", <resource>.Update)
   mux.HandleFunc("DELETE /api/v1/<resource>/{id}", <resource>.Delete)
   ```

   Only register the operations the user requested.

   If a handler needs a dependency (a repository, a client, etc.),
   follow the pattern already used for `/readyz` in this file:
   `readyz(mongoClient)` returns an `http.HandlerFunc` closing over
   `mongoClient`. Write your handler the same way — a small
   constructor function that takes the dependency and returns the
   `http.HandlerFunc` — rather than a package-level global. This keeps
   the handler mockable in tests and keeps dependency wiring explicit
   and visible in `main.go`, the same way `cfg` and `logger` already
   flow through the app.

### 4. Route format reference

The Go 1.22+ ServeMux supports these patterns:

| Pattern                       | Meaning                        |
|-------------------------------|--------------------------------|
| `"GET /path"`                 | Exact method and path          |
| `"GET /path/{id}"`            | Named path parameter           |
| `"GET /path/{rest...}"`       | Wildcard / greedy parameter    |
| `"/path"`                     | Any method (avoid this)        |

### 5. Do NOT modify

- `main.go` — the entrypoint doesn't need to change for a new endpoint,
  unless the handler needs a new dependency constructed at startup
  (e.g. a repository) and threaded down through `router.New`.
- `middleware/` — middleware is applied globally and automatically; a
  new endpoint doesn't need to opt into request ID, logging, panic
  recovery, or CORS individually.
- `respond/` — only modify if a genuinely new response shape is
  needed; reuse `respond.JSON`/`respond.Error` otherwise so every
  endpoint's error envelope stays consistent for clients.

### 6. Verify

After writing the code, run:

```bash
cd server && go vet ./...
```

to confirm the code compiles and passes static analysis.
