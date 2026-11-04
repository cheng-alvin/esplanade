package auth

import (
	"net/http"

	authmw "github.com/cheng-alvin/esplanade/server/auth/middleware"
	"github.com/cheng-alvin/esplanade/server/auth/service"
	"github.com/cheng-alvin/esplanade/server/respond"
)

// Identities handles GET /api/v1/auth/identities.
func Identities(svc *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authmw.UserIDFrom(r.Context())
		if !ok {
			respond.ErrorCode(w, http.StatusUnauthorized, "invalid_token", "authentication required")
			return
		}

		identities, err := svc.Identities(r.Context(), userID)
		if err != nil {
			writeError(w, err)
			return
		}
		respond.JSON(w, http.StatusOK, map[string]any{"identities": identities})
	}
}

// UnlinkProvider handles DELETE /api/v1/auth/providers/{provider}.
func UnlinkProvider(svc *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authmw.UserIDFrom(r.Context())
		if !ok {
			respond.ErrorCode(w, http.StatusUnauthorized, "invalid_token", "authentication required")
			return
		}

		if err := svc.UnlinkProvider(r.Context(), userID, r.PathValue("provider")); err != nil {
			writeError(w, err)
			return
		}
		respond.JSON(w, http.StatusNoContent, nil)
	}
}
