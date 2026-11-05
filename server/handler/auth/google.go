package auth

import (
	"net/http"

	authmw "github.com/cheng-alvin/esplanade/server/auth/middleware"
	"github.com/cheng-alvin/esplanade/server/auth/service"
	"github.com/cheng-alvin/esplanade/server/respond"
)

// GoogleLogin handles POST /api/v1/auth/google.
func GoogleLogin(svc *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in googleRequest
		if !decode(w, r, &in) {
			return
		}

		pair, err := svc.GoogleLogin(r.Context(), in.IDToken, in.DeviceIdentifier, userAgent(r))
		if err != nil {
			writeError(w, err)
			return
		}
		respond.JSON(w, http.StatusOK, pair)
	}
}

// LinkGoogle handles POST /api/v1/auth/link/google.
func LinkGoogle(svc *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authmw.UserIDFrom(r.Context())
		if !ok {
			respond.ErrorCode(w, http.StatusUnauthorized, "invalid_token", "authentication required")
			return
		}

		var in linkGoogleRequest
		if !decode(w, r, &in) {
			return
		}

		if err := svc.LinkGoogle(r.Context(), userID, in.IDToken); err != nil {
			writeError(w, err)
			return
		}
		respond.JSON(w, http.StatusOK, map[string]string{"status": "linked"})
	}
}
