package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	autherr "github.com/cheng-alvin/esplanade/server/auth/errors"
	authmw "github.com/cheng-alvin/esplanade/server/auth/middleware"
	"github.com/cheng-alvin/esplanade/server/auth/service"
	"github.com/cheng-alvin/esplanade/server/respond"
)

// VerifyEmail handles GET /api/v1/auth/verify-email?token=...
func VerifyEmail(svc *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rawToken := r.URL.Query().Get("token")

		if err := svc.VerifyEmail(r.Context(), rawToken); err != nil {
			if errors.Is(err, autherr.ErrTokenInvalid) {
				respond.ErrorCode(w, http.StatusBadRequest, "invalid_token", "invalid or expired token")
				return
			}
			writeError(w, err)
			return
		}
		respond.JSON(w, http.StatusOK, map[string]string{"status": "verified"})
	}
}

// ResendVerification handles POST /api/v1/auth/resend-verification. The body
// is optional for authenticated callers (their own address is used).
func ResendVerification(svc *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in resendRequest
		_ = json.NewDecoder(r.Body).Decode(&in)

		userID, _ := authmw.UserIDFrom(r.Context())
		if userID == "" && in.Email == "" {
			respond.ErrorCode(w, http.StatusBadRequest, "invalid_request", "email is required")
			return
		}

		if err := svc.ResendVerification(r.Context(), userID, in.Email); err != nil {
			writeError(w, err)
			return
		}
		respond.JSON(w, http.StatusOK, map[string]string{"message": genericEmailMessage})
	}
}
