package auth

import (
	"encoding/json"
	"net/http"

	authmw "github.com/cheng-alvin/esplanade/server/auth/middleware"
	"github.com/cheng-alvin/esplanade/server/auth/service"
	"github.com/cheng-alvin/esplanade/server/respond"
)

// Logout handles POST /api/v1/auth/logout. The body is optional: without a
// refresh token the caller's device sessions are deleted instead.
func Logout(svc *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authmw.UserIDFrom(r.Context())
		if !ok {
			respond.ErrorCode(w, http.StatusUnauthorized, "invalid_token", "authentication required")
			return
		}
		deviceID, _ := authmw.DeviceIDFrom(r.Context())

		var in logoutRequest
		_ = json.NewDecoder(r.Body).Decode(&in)

		if err := svc.Logout(r.Context(), userID, deviceID, in.RefreshToken); err != nil {
			writeError(w, err)
			return
		}
		respond.JSON(w, http.StatusNoContent, nil)
	}
}

// LogoutAll handles POST /api/v1/auth/logout-all: revokes every session of
// the authenticated user.
func LogoutAll(svc *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := authmw.UserIDFrom(r.Context())
		if !ok {
			respond.ErrorCode(w, http.StatusUnauthorized, "invalid_token", "authentication required")
			return
		}

		if err := svc.LogoutAll(r.Context(), userID); err != nil {
			writeError(w, err)
			return
		}
		respond.JSON(w, http.StatusNoContent, nil)
	}
}
