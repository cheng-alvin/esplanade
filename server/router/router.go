package router

import (
	"net/http"

	"github.com/cheng-alvin/esplanade/server/respond"
)

// New returns a fully configured *http.ServeMux with all application
// routes registered.
//
// To add a new route, create a handler package under handler/
// and register it here.  Example:
//
//	mux.HandleFunc("GET /api/v1/widgets", widgets.List)
//	mux.HandleFunc("POST /api/v1/widgets", widgets.Create)
func New() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", healthz)

	return mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	respond.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
