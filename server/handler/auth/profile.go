package auth

import (
	"net/http"

	authmw "github.com/cheng-alvin/esplanade/server/auth/middleware"
	"github.com/cheng-alvin/esplanade/server/auth/service"
	"github.com/cheng-alvin/esplanade/server/respond"
)

// Me handles GET /api/v1/auth/me.
func Me(svc *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authmw.UserIDFrom(r.Context())
		if !ok {
			respond.ErrorCode(w, http.StatusUnauthorized, "invalid_token", "authentication required")
			return
		}

		profile, err := svc.Me(r.Context(), userID)
		if err != nil {
			writeError(w, err)
			return
		}
		respond.JSON(w, http.StatusOK, profile)
	}
}
