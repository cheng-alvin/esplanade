# Adding Endpoints to the Esplanade Server

This guide explains how to add new HTTP endpoints to the server. The project follows a straightforward pattern: **create a handler, register it in the router**.

---

## Quick Start

Adding a new endpoint is a two-file change:

1. Create a handler file in `handler/<resource>/`
2. Register the route in `router/router.go`

---

## Step-by-step Walkthrough

### 1. Create a Handler Package

Handlers live under `server/handler/`. Each logical resource gets its own sub-package. For example, to add a "widgets" resource:

```
server/handler/widgets/
└── widgets.go
```

A handler is just a standard `http.HandlerFunc`:

```go
// server/handler/widgets/widgets.go
package widgets

import (
    "net/http"

    "github.com/cheng-alvin/esplanade/server/respond"
)

// List returns all widgets.
func List(w http.ResponseWriter, r *http.Request) {
    // TODO: fetch from database / service layer
    result := []map[string]string{
        {"id": "1", "name": "Sprocket"},
    }
    respond.JSON(w, http.StatusOK, result)
}

// Create handles POST requests to create a widget.
func Create(w http.ResponseWriter, r *http.Request) {
    // TODO: decode request body, validate, persist
    respond.JSON(w, http.StatusCreated, map[string]string{"id": "2"})
}

// GetByID returns a single widget by its ID.
// The path parameter {id} is extracted from the request.
func GetByID(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    respond.JSON(w, http.StatusOK, map[string]string{"id": id})
}
```

**Key conventions:**

- Use `respond.JSON()` and `respond.Error()` from the `respond` package for all responses. This keeps the JSON format consistent across the API.
- Decode request bodies with `encoding/json` — `json.NewDecoder(r.Body).Decode(&v)`.
- Extract path parameters with `r.PathValue("name")` (Go 1.22+).

### 2. Register Routes in the Router

Open `server/router/router.go` and import the new handler package, then add routes inside the `New()` function:

```go
import (
    "github.com/cheng-alvin/esplanade/server/handler/widgets"
)

func New() *http.ServeMux {
    mux := http.NewServeMux()

    // Health
    mux.HandleFunc("GET /healthz", healthz)

    // Widgets
    mux.HandleFunc("GET /api/v1/widgets", widgets.List)
    mux.HandleFunc("POST /api/v1/widgets", widgets.Create)
    mux.HandleFunc("GET /api/v1/widgets/{id}", widgets.GetByID)

    return mux
}
```

**Route format** (Go 1.22+ enhanced ServeMux):

```
"METHOD /path"           → exact method + path
"GET /users/{id}"        → path parameter captured via r.PathValue("id")
"GET /files/{path...}"   → wildcard (greedy) path parameter
```

### 3. That's It

The middleware stack (request ID, logging, panic recovery, CORS) is applied automatically to every route. You don't need to add any of that to individual handlers.

---

## URL Conventions

| Element         | Convention                  | Example                     |
| --------------- | --------------------------- | --------------------------- |
| API prefix      | `/api/v1/`                  | `/api/v1/users`             |
| Resource names  | Plural, lowercase           | `widgets`, `users`          |
| Path parameters | `{name}` in the route       | `/api/v1/widgets/{id}`      |
| HTTP methods    | Use standard REST semantics | GET list, POST create, etc. |

---

## Error Responses

Use `respond.Error()` to return errors in a consistent JSON envelope:

```go
if err != nil {
    respond.Error(w, http.StatusBadRequest, "invalid widget ID")
    return
}
```

This produces:

```json
{ "error": "invalid widget ID" }
```

---

## Directory Layout Reference

```
server/
├── cmd/server/main.go           ← entrypoint (you won't usually edit this)
├── config/                      ← configuration loading
├── ctxkey/                      ← context key types
├── db/mongo/                    ← Mongo connection lifecycle (see docs/MONGODB.md)
├── handler/                     ← ★ your endpoint handlers go here
│   └── widgets/widgets.go       ← example
├── logging/                     ← structured logger setup
├── middleware/                  ← request pipeline (auto-applied)
├── repository/                  ← generic Mongo CRUD primitive (see docs/MONGODB.md)
├── respond/                     ← JSON response helpers
└── router/router.go             ← ★ route registration
├── docs/                        ← documentation (you are here)
├── config.yaml.example          ← example configuration
├── Dockerfile                   ← container build
├── Makefile                     ← dev commands
├── go.mod
└── go.sum
```

The two files marked with ★ are the only files you need to touch when adding a basic endpoint.
