package auth

import (
	"errors"
	"net/http"

	autherr "github.com/cheng-alvin/esplanade/server/auth/errors"
	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
	"github.com/cheng-alvin/esplanade/server/respond"
)

// writeError maps service sentinels onto stable HTTP status and error codes.
// The default branch never leaks internal error text.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, autherr.ErrUserExists):
		respond.ErrorCode(w, http.StatusConflict, "user_exists", "an account with that email already exists")
	case errors.Is(err, autherr.ErrInvalidEmail):
		respond.ErrorCode(w, http.StatusBadRequest, "invalid_email", "invalid email address")
	case errors.Is(err, autherr.ErrWeakPassword):
		respond.ErrorCode(w, http.StatusBadRequest, "weak_password", "password must be between 8 and 128 characters")
	case errors.Is(err, autherr.ErrInvalidCredentials):
		respond.ErrorCode(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
	case errors.Is(err, autherr.ErrEmailNotVerified):
		respond.ErrorCode(w, http.StatusForbidden, "email_not_verified", "email verification required")
	case errors.Is(err, autherr.ErrEmailAlreadyVerified):
		respond.ErrorCode(w, http.StatusBadRequest, "email_already_verified", "email is already verified")
	case errors.Is(err, autherr.ErrTokenExpired):
		respond.ErrorCode(w, http.StatusUnauthorized, "token_expired", "token expired")
	case errors.Is(err, autherr.ErrTokenInvalid):
		respond.ErrorCode(w, http.StatusUnauthorized, "invalid_token", "invalid token")
	case errors.Is(err, autherr.ErrRateLimited):
		respond.ErrorCode(w, http.StatusTooManyRequests, "rate_limited", "too many requests; try again later")
	case errors.Is(err, autherr.ErrLinkRequired):
		respond.ErrorCode(w, http.StatusConflict, "link_required", "an account with that email already exists; sign in and link Google")
	case errors.Is(err, autherr.ErrGoogleAlreadyLinked):
		respond.ErrorCode(w, http.StatusConflict, "google_already_linked", "google account already linked")
	case errors.Is(err, autherr.ErrInvalidGoogleToken):
		respond.ErrorCode(w, http.StatusUnauthorized, "invalid_google_token", "invalid google token")
	case errors.Is(err, autherr.ErrProviderNotFound):
		respond.ErrorCode(w, http.StatusNotFound, "provider_not_found", "provider not linked")
	case errors.Is(err, autherr.ErrLastAuthMethod):
		respond.ErrorCode(w, http.StatusConflict, "last_auth_method", "cannot remove the only sign-in method")
	case errors.Is(err, autherr.ErrUnsupportedProvider):
		respond.ErrorCode(w, http.StatusBadRequest, "unsupported_provider", "unsupported provider")
	case errors.Is(err, autherr.ErrInvalidDeviceIdentifier):
		respond.ErrorCode(w, http.StatusBadRequest, "invalid_device_identifier", "invalid device identifier")
	case errors.Is(err, autherr.ErrUserNotFound), errors.Is(err, esmongo.ErrNotFound):
		respond.ErrorCode(w, http.StatusNotFound, "not_found", "not found")
	default:
		respond.ErrorCode(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
