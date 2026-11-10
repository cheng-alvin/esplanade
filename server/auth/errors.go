// Package auth holds sentinel errors shared across the auth module's
// packages; errors specific to one package are declared in that package.
package auth

import "errors"

// ErrInvalidCredentials indicates authentication failed. Deliberately
// non-specific: an unknown account, a wrong password, and any token failure
// (expired, malformed, tampered) all report the same error.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")
