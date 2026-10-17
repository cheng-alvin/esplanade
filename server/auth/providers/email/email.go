// Package email provides password hashing and credential validation for the
// email authentication method.
package email

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/mail"
	"strings"

	autherr "github.com/cheng-alvin/esplanade/server/auth/errors"
	"golang.org/x/crypto/argon2"
)

const (
	MinPasswordLength = 8
	MaxPasswordLength = 128
	MaxEmailLength    = 254

	// argon2id parameters follow OWASP's baseline (19 MiB, t=2, p=1).
	// argonMemory is expressed in KiB as required by argon2.IDKey.
	argonTime    = 2
	argonMemory  = 19456
	argonThreads = 1
	argonKeyLen  = 32
	saltLen      = 16
)

// NormalizeEmail trims, lowercases and validates a bare email address.
// Display-name forms ("Name <a@b>") are rejected: the address doubles as the
// account's unique identifier, so only the canonical bare form is accepted.
func NormalizeEmail(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > MaxEmailLength {
		return "", autherr.ErrInvalidEmail
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil || parsed.Address != trimmed {
		return "", autherr.ErrInvalidEmail
	}
	return strings.ToLower(parsed.Address), nil
}

// ValidatePassword enforces the password length policy in bytes.
func ValidatePassword(plain string) error {
	if len(plain) < MinPasswordLength || len(plain) > MaxPasswordLength {
		return autherr.ErrWeakPassword
	}
	return nil
}

// HashPassword hashes plain with argon2id and returns a PHC-format string:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<b64raw(salt)>$<b64raw(key)>
func HashPassword(plain string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("email: generating password salt: %w", err)
	}

	key := argon2.IDKey([]byte(plain), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword recomputes the hash from the parameters encoded in the
// stored value and compares in constant time. Malformed encodings return
// false: callers only need a yes/no credential check.
func VerifyPassword(plain, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}

	var memory, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(salt) == 0 || len(key) == 0 {
		return false
	}

	computed := argon2.IDKey([]byte(plain), salt, timeCost, memory, threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(computed, key) == 1
}
