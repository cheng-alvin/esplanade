package auth

import (
	"net/http"

	"github.com/cheng-alvin/esplanade/server/auth/service"
	"github.com/cheng-alvin/esplanade/server/respond"
)

// Register handles POST /api/v1/auth/register.
func Register(svc *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in registerRequest
		if !decode(w, r, &in) {
			return
		}

		pair, err := svc.Register(r.Context(), in.Email, in.Password, in.DeviceIdentifier, userAgent(r))
		if err != nil {
			writeError(w, err)
			return
		}
		respond.JSON(w, http.StatusCreated, pair)
	}
}
