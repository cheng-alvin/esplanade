package router

import (
	"context"
	"net/http"
	"time"

	"github.com/cheng-alvin/esplanade/server/auth/jwt"
	authmw "github.com/cheng-alvin/esplanade/server/auth/middleware"
	authrepo "github.com/cheng-alvin/esplanade/server/auth/repository"
	"github.com/cheng-alvin/esplanade/server/auth/service"
	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
	authh "github.com/cheng-alvin/esplanade/server/handler/auth"
	"github.com/cheng-alvin/esplanade/server/respond"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// readinessTimeout bounds the Mongo ping performed by /readyz — this
// check backs a load balancer / orchestrator probe and should fail fast
// rather than hang.
const readinessTimeout = 2 * time.Second

// AuthDeps are the auth collaborators the router wires into routes. A zero
// value (nil Service) skips auth route registration, mirroring readyz(nil).
type AuthDeps struct {
	Service  *service.AuthService
	Verifier *jwt.Verifier
	Users    *authrepo.UserRepository
}

// New returns a fully configured *http.ServeMux with all application
// routes registered. mongoClient backs the /readyz endpoint's database
// check; pass nil to omit that check.
func New(mongoClient *mongo.Client, auth AuthDeps) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz(mongoClient))

	if auth.Service != nil {
		mux.HandleFunc("POST /api/v1/auth/register", authh.Register(auth.Service))
		mux.HandleFunc("POST /api/v1/auth/login", authh.Login(auth.Service))
		mux.HandleFunc("POST /api/v1/auth/refresh", authh.Refresh(auth.Service))
		mux.Handle("POST /api/v1/auth/logout", authmw.RequireAuth(auth.Verifier)(authh.Logout(auth.Service)))
		mux.Handle("POST /api/v1/auth/logout-all", authmw.RequireAuth(auth.Verifier)(authh.LogoutAll(auth.Service)))
		mux.Handle("GET /api/v1/auth/me", authmw.RequireAuth(auth.Verifier)(authh.Me(auth.Service)))

		mux.HandleFunc("GET /api/v1/auth/verify-email", authh.VerifyEmail(auth.Service))
		mux.Handle("POST /api/v1/auth/resend-verification", authmw.OptionalAuth(auth.Verifier)(authh.ResendVerification(auth.Service)))

		mux.HandleFunc("POST /api/v1/auth/password/forgot", authh.ForgotPassword(auth.Service))
		mux.HandleFunc("POST /api/v1/auth/password/reset", authh.ResetPassword(auth.Service))
	}

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
