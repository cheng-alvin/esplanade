package auth

import (
	"net/http"

	"github.com/cheng-alvin/esplanade/server/auth/service"
	"github.com/cheng-alvin/esplanade/server/respond"
)

// Login handles POST /api/v1/auth/login.
func Login(svc *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in loginRequest
		if !decode(w, r, &in) {
			return
		}

		pair, err := svc.Login(r.Context(), in.Email, in.Password, in.DeviceIdentifier, userAgent(r))
		if err != nil {
			writeError(w, err)
			return
		}
		respond.JSON(w, http.StatusOK, pair)
	}
}
