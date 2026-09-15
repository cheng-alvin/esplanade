package router

import (
	"context"
	"net/http"
	"time"

	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
	"github.com/cheng-alvin/esplanade/server/respond"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// readinessTimeout bounds the Mongo ping performed by /readyz — this
// check backs a load balancer / orchestrator probe and should fail fast
// rather than hang.
const readinessTimeout = 2 * time.Second

// New returns a fully configured *http.ServeMux with all application
// routes registered. mongoClient backs the /readyz endpoint's database
// check; pass nil to omit that check.
//
// To add a new route, create a handler package under handler/
// and register it here.  Example:
//
//	mux.HandleFunc("GET /api/v1/widgets", widgets.List)
//	mux.HandleFunc("POST /api/v1/widgets", widgets.Create)
func New(mongoClient *mongo.Client) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz(mongoClient))

	return mux
}

// healthz is a liveness check: it reports that the process is up,
// independent of any downstream dependency.
func healthz(w http.ResponseWriter, _ *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz is a readiness check, distinct from healthz: it also confirms
// the Mongo connection is reachable, so an orchestrator can hold traffic
// back from an instance that's up but can't yet serve requests.
func readyz(mongoClient *mongo.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mongoClient == nil {
			respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
		defer cancel()

		if err := esmongo.Ping(ctx, mongoClient); err != nil {
			respond.Error(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}

		respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
