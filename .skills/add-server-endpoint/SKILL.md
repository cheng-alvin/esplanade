name: add-server-endpoint
description: >
  Add a new HTTP endpoint to the Esplanade Go backend server.
  Use when the user asks to create a new API route, REST endpoint,
  or handler in the server.

---

# Add Server Endpoint

This skill adds new HTTP API endpoints to the Esplanade Go backend
server located at `server/`.

## Prerequisites

- The `server/` directory exists with the boilerplate already set up.
- Go 1.22+ (required for enhanced ServeMux routing).

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
import "github.com/cheng-alvin/esplanade/server/internal/respond"
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

### 3. Register routes in the router

Open `server/internal/router/router.go`.

1. Add an import for the new handler package:

   ```go
   "github.com/cheng-alvin/esplanade/server/internal/handler/<resource>"
   ```

2. Inside the `New()` function, add `mux.HandleFunc` calls below the
   `// Register new endpoint handlers below this line.` comment:

   ```go
   mux.HandleFunc("GET /api/v1/<resource>", <resource>.List)
   mux.HandleFunc("POST /api/v1/<resource>", <resource>.Create)
   mux.HandleFunc("GET /api/v1/<resource>/{id}", <resource>.GetByID)
   mux.HandleFunc("PUT /api/v1/<resource>/{id}", <resource>.Update)
   mux.HandleFunc("DELETE /api/v1/<resource>/{id}", <resource>.Delete)
   ```

   Only register the operations the user requested.

### 4. Route format reference

The Go 1.22+ ServeMux supports these patterns:

| Pattern                       | Meaning                        |
|-------------------------------|--------------------------------|
| `"GET /path"`                 | Exact method and path          |
| `"GET /path/{id}"`            | Named path parameter           |
| `"GET /path/{rest...}"`       | Wildcard / greedy parameter    |
| `"/path"`                     | Any method (avoid this)        |

### 5. Do NOT modify

- `cmd/server/main.go` — the entrypoint does not change for new endpoints.
- `internal/middleware/` — middleware is applied globally and automatically.
- `internal/respond/` — only modify if a new response format is needed.

### 6. Verify

After writing the code, run:

```bash
cd server && go vet ./...
```

to confirm the code compiles and passes static analysis.
