// Package email provides password hashing and credential validation for the
// email authentication method.
package email

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/alexedwards/argon2id"
)

const (
	MinPasswordLength = 8
	MaxPasswordLength = 128
	MaxEmailLength    = 254

	// argon2id parameters follow OWASP's baseline (19 MiB, t=2, p=1).
	// argonMemory is expressed in KiB, matching argon2id.Params.
	argonTime    = 2
	argonMemory  = 19456
	argonThreads = 1
	argonKeyLen  = 32
	saltLen      = 16
)

var (
	// Indicates the submitted email is not a valid address.
	ErrInvalidEmail = errors.New("auth: invalid email address")

	// Indicates the submitted password does not meet the length policy.
	ErrWeakPassword = errors.New("auth: password does not meet requirements")
)

// argonParams govern hashing of new credentials. Verification reads the
// parameters embedded in each stored hash, so retuning these does not
// invalidate existing hashes.
var argonParams = &argon2id.Params{
	Memory:      argonMemory,
	Iterations:  argonTime,
	Parallelism: argonThreads,
	SaltLength:  saltLen,
	KeyLength:   argonKeyLen,
}

// NormalizeEmail trims, lowercases and validates a bare email address.
// Display-name forms ("Name <a@b>") are rejected: the address doubles as the
// account's unique identifier, so only the canonical bare form is accepted.
func NormalizeEmail(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > MaxEmailLength {
		return "", ErrInvalidEmail
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil || parsed.Address != trimmed {
		return "", ErrInvalidEmail
	}
	return strings.ToLower(parsed.Address), nil
}

// ValidatePassword enforces the password length policy in bytes.
func ValidatePassword(plain string) error {
	if len(plain) < MinPasswordLength || len(plain) > MaxPasswordLength {
		return ErrWeakPassword
	}
	return nil
}

// HashPassword hashes plain with argon2id and returns a PHC-format string:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<b64raw(salt)>$<b64raw(key)>
func HashPassword(plain string) (string, error) {
	encoded, err := argon2id.CreateHash(plain, argonParams)
	if err != nil {
		return "", fmt.Errorf("email: hashing password: %w", err)
	}
	return encoded, nil
}

// VerifyPassword recomputes the hash from the parameters encoded in the
// stored value and compares in constant time. Malformed encodings return
// false: callers only need a yes/no credential check.
func VerifyPassword(plain, encoded string) bool {
	match, err := argon2id.ComparePasswordAndHash(plain, encoded)
	if err != nil {
		return false
	}
	return match
}
