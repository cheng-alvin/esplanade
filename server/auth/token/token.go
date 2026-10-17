// Package token generates and hashes opaque bearer tokens.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// tokenBytes is the raw entropy per token: 256 bits.
const tokenBytes = 32

// Generate returns a new opaque token (base64url, unpadded). The raw value is
// only ever handed to the client; only its hash is persisted.
func Generate() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Hash returns the hex-encoded SHA-256 digest of a raw token — the only form
// ever stored. A plain digest is appropriate because the input is
// full-entropy; unlike a password, it is not enumerable.
func Hash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
