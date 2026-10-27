// Package errors defines the sentinel errors returned by the auth packages.
// The auth HTTP handlers map each sentinel to a stable status and error code.
package errors

import "errors"

var (
	// Indicates an account with the submitted email already exists.
	ErrUserExists = errors.New("auth: user already exists")

	// Indicates the requested user does not exist.
	ErrUserNotFound = errors.New("auth: user not found")

	// Indicates the submitted email is not a valid address.
	ErrInvalidEmail = errors.New("auth: invalid email address")

	// Indicates the submitted password does not meet the length policy.
	ErrWeakPassword = errors.New("auth: password does not meet requirements")

	// Indicates authentication failed. Deliberately does not distinguish an
	// unknown account from a wrong password.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")

	// Indicates the account's email address is not verified.
	ErrEmailNotVerified = errors.New("auth: email not verified")

	// Indicates verification was requested for an already-verified account.
	ErrEmailAlreadyVerified = errors.New("auth: email already verified")

	// Indicates a token failed validation, was already consumed, or is
	// otherwise unusable.
	ErrTokenInvalid = errors.New("auth: token invalid")

	// Indicates an access token failed its expiry check.
	ErrTokenExpired = errors.New("auth: token expired")

	// Indicates a request exceeded an endpoint's rate limit.
	ErrRateLimited = errors.New("auth: rate limit exceeded")

	// Indicates Google sign-in matched an existing account by email; the
	// user must sign in and link explicitly instead of being auto-linked.
	ErrLinkRequired = errors.New("auth: account exists; link required")

	// Indicates the Google account is already linked to a user.
	ErrGoogleAlreadyLinked = errors.New("auth: google account already linked")

	// Indicates the submitted Google ID token failed verification.
	ErrInvalidGoogleToken = errors.New("auth: invalid google token")

	// Indicates the requested provider is not linked to the account.
	ErrProviderNotFound = errors.New("auth: provider not linked")

	// Indicates the unlink would remove the account's only sign-in method.
	ErrLastAuthMethod = errors.New("auth: cannot remove the only sign-in method")

	// Indicates the provider name is not recognised.
	ErrUnsupportedProvider = errors.New("auth: unsupported provider")

	// Indicates the device identifier failed validation.
	ErrInvalidDeviceIdentifier = errors.New("auth: invalid device identifier")
)
