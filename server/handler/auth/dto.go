// Package auth implements the authentication HTTP endpoints. Handlers depend
// on the service layer and translate its sentinels into stable HTTP error
// codes; all responses go through the respond package.
package auth

import (
	"encoding/json"
	"net/http"

	"github.com/cheng-alvin/esplanade/server/respond"
)

// genericEmailMessage is the enumeration-safe response for forgot-password
// and unauthenticated resend-verification: identical whether or not the
// account exists.
const genericEmailMessage = "If an account exists for that email address, a message has been sent."

// maxUserAgentLength bounds the user agent stored on a session.
const maxUserAgentLength = 512

type registerRequest struct {
	Email            string `json:"email"`
	Password         string `json:"password"`
	DeviceIdentifier string `json:"device_identifier"`
}

type loginRequest struct {
	Email            string `json:"email"`
	Password         string `json:"password"`
	DeviceIdentifier string `json:"device_identifier"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type resendRequest struct {
	Email string `json:"email"`
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return false
	}
	return true
}

func userAgent(r *http.Request) string {
	agent := r.UserAgent()
	if len(agent) > maxUserAgentLength {
		return agent[:maxUserAgentLength]
	}
	return agent
}
