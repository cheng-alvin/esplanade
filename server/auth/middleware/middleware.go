// Package middleware provides the auth-aware HTTP middleware: access-token
// authentication, optional authentication, and the verified-email gate.
package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	autherr "github.com/cheng-alvin/esplanade/server/auth/errors"
	"github.com/cheng-alvin/esplanade/server/auth/jwt"
	authrepo "github.com/cheng-alvin/esplanade/server/auth/repository"
	"github.com/cheng-alvin/esplanade/server/ctxkey"
	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
	"github.com/cheng-alvin/esplanade/server/respond"
)

// UserFetcher is the narrow user-lookup surface RequireVerifiedEmail needs.
type UserFetcher interface {
	FindByID(ctx context.Context, id string) (*authrepo.User, error)
}

// RequireAuth rejects requests without a valid bearer access token and
// injects the token's user and device identifiers into the request context.
func RequireAuth(verifier *jwt.Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, status, code, message := bearerClaims(r, verifier)
			if status != 0 {
				respond.ErrorCode(w, status, code, message)
				return
			}
			next.ServeHTTP(w, r.WithContext(withClaims(r.Context(), claims)))
		})
	}
}

// OptionalAuth authenticates when a bearer token is present. A present but
// invalid token is still rejected (fail closed); absence passes through.
func OptionalAuth(verifier *jwt.Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") == "" {
				next.ServeHTTP(w, r)
				return
			}

			claims, status, code, message := bearerClaims(r, verifier)
			if status != 0 {
				respond.ErrorCode(w, status, code, message)
				return
			}
			next.ServeHTTP(w, r.WithContext(withClaims(r.Context(), claims)))
		})
	}
}

// RequireVerifiedEmail gates on R6's combined verified notion — a linked
// Google account counts as verified. It must be stacked on RequireAuth; it
// currently has no consumer (future pairing/transfer endpoints).
func RequireVerifiedEmail(users UserFetcher) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := UserIDFrom(r.Context())
			if !ok {
				respond.ErrorCode(w, http.StatusUnauthorized, "invalid_token", "authentication required")
				return
			}

			user, err := users.FindByID(r.Context(), userID)
			if err != nil {
				if errors.Is(err, esmongo.ErrNotFound) {
					respond.ErrorCode(w, http.StatusUnauthorized, "invalid_token", "invalid token")
					return
				}
				respond.ErrorCode(w, http.StatusInternalServerError, "internal_error", "internal server error")
				return
			}

			if user.GoogleAuth == nil && (user.EmailAuth == nil || !user.EmailAuth.EmailVerified) {
				respond.ErrorCode(w, http.StatusForbidden, "email_not_verified", "email verification required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserIDFrom returns the authenticated user's ID injected by RequireAuth or
// OptionalAuth.
func UserIDFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxkey.UserID).(string)
	return id, ok
}

// DeviceIDFrom returns the authenticated device's ID injected by RequireAuth
// or OptionalAuth.
func DeviceIDFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxkey.DeviceID).(string)
	return id, ok
}

// bearerClaims verifies the request's bearer token. On failure it returns the
// status/code/message the caller should respond with; on success status is 0.
func bearerClaims(r *http.Request, verifier *jwt.Verifier) (*jwt.Claims, int, string, string) {
	fields := strings.Fields(r.Header.Get("Authorization"))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "bearer") {
		return nil, http.StatusUnauthorized, "invalid_token", "authentication required"
	}

	claims, err := verifier.Verify(fields[1])
	switch {
	case errors.Is(err, autherr.ErrTokenExpired):
		return nil, http.StatusUnauthorized, "token_expired", "token expired"
	case err != nil:
		return nil, http.StatusUnauthorized, "invalid_token", "invalid token"
	}
	return claims, 0, "", ""
}

func withClaims(ctx context.Context, claims *jwt.Claims) context.Context {
	ctx = context.WithValue(ctx, ctxkey.UserID, claims.UserID)
	return context.WithValue(ctx, ctxkey.DeviceID, claims.DeviceID)
}
