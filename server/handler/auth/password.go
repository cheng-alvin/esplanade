package auth

import (
	"errors"
	"net/http"

	autherr "github.com/cheng-alvin/esplanade/server/auth/errors"
	"github.com/cheng-alvin/esplanade/server/auth/service"
	"github.com/cheng-alvin/esplanade/server/respond"
)

// ForgotPassword handles POST /api/v1/auth/password/forgot.
func ForgotPassword(svc *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in forgotRequest
		if !decode(w, r, &in) {
			return
		}

		if err := svc.ForgotPassword(r.Context(), in.Email); err != nil {
			writeError(w, err)
			return
		}
		respond.JSON(w, http.StatusOK, map[string]string{"message": genericEmailMessage})
	}
}

// ResetPassword handles POST /api/v1/auth/password/reset.
func ResetPassword(svc *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in resetRequest
		if !decode(w, r, &in) {
			return
		}

		if err := svc.ResetPassword(r.Context(), in.Token, in.NewPassword); err != nil {
			if errors.Is(err, autherr.ErrTokenInvalid) {
				respond.ErrorCode(w, http.StatusBadRequest, "invalid_token", "invalid or expired token")
				return
			}
			writeError(w, err)
			return
		}
		respond.JSON(w, http.StatusOK, map[string]string{"status": "password_reset"})
	}
}
