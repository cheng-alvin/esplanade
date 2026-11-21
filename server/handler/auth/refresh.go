package auth

import (
	"net/http"

	"github.com/cheng-alvin/esplanade/server/auth/service"
	"github.com/cheng-alvin/esplanade/server/respond"
)

// Refresh handles POST /api/v1/auth/refresh: rotates the refresh token and
// returns a fresh pair (reuse detection revokes the user's sessions).
func Refresh(svc *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in refreshRequest
		if !decode(w, r, &in) {
			return
		}

		pair, err := svc.Refresh(r.Context(), in.RefreshToken)
		if err != nil {
			writeError(w, err)
			return
		}
		respond.JSON(w, http.StatusOK, pair)
	}
}
