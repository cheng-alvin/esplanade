// Package jwt issues and verifies the service's HS256 access tokens.
package jwt

import (
	"errors"
	"fmt"
	"time"

	autherr "github.com/cheng-alvin/esplanade/server/auth/errors"
	jwtlib "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// HS256 was chosen for the single-service deployment; RS256 stays an option
// behind this package if a public verification key ever needs distributing.
const signingMethod = "HS256"

// Claims are the verified contents of an access token.
type Claims struct {
	UserID    string
	Email     string
	DeviceID  string
	JTI       string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type accessClaims struct {
	Email    string `json:"email"`
	DeviceID string `json:"device_id"`
	jwtlib.RegisteredClaims
}

// Issuer signs access tokens with the configured secret.
type Issuer struct {
	secret []byte
	ttl    time.Duration
}

func NewIssuer(secret string, ttl time.Duration) *Issuer {
	return &Issuer{secret: []byte(secret), ttl: ttl}
}

// Issue returns the signed access token and its lifetime in seconds.
func (i *Issuer) Issue(userID, email, deviceID string) (string, int, error) {
	now := time.Now().UTC()
	claims := accessClaims{
		Email:    email,
		DeviceID: deviceID,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(now.Add(i.ttl)),
			ID:        uuid.NewString(),
		},
	}

	signed, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims).SignedString(i.secret)
	if err != nil {
		return "", 0, fmt.Errorf("jwt: signing access token: %w", err)
	}
	return signed, int(i.ttl.Seconds()), nil
}

// Verifier validates access tokens against the configured secret.
type Verifier struct {
	secret []byte
}

func NewVerifier(secret string) *Verifier {
	return &Verifier{secret: []byte(secret)}
}

// Verify parses and validates a raw access token. It returns
// autherr.ErrTokenExpired for an expired token and autherr.ErrTokenInvalid
// for every other failure (bad signature, malformed, wrong algorithm).
func (v *Verifier) Verify(raw string) (*Claims, error) {
	claims := &accessClaims{}
	parsed, err := jwtlib.ParseWithClaims(raw, claims, func(t *jwtlib.Token) (any, error) {
		if _, ok := t.Method.(*jwtlib.SigningMethodHMAC); !ok {
			return nil, autherr.ErrTokenInvalid
		}
		return v.secret, nil
	}, jwtlib.WithValidMethods([]string{signingMethod}), jwtlib.WithExpirationRequired())
	if err != nil {
		if errors.Is(err, jwtlib.ErrTokenExpired) {
			return nil, autherr.ErrTokenExpired
		}
		return nil, autherr.ErrTokenInvalid
	}
	if !parsed.Valid {
		return nil, autherr.ErrTokenInvalid
	}

	verified := &Claims{
		UserID:   claims.Subject,
		Email:    claims.Email,
		DeviceID: claims.DeviceID,
		JTI:      claims.ID,
	}
	if claims.IssuedAt != nil {
		verified.IssuedAt = claims.IssuedAt.Time
	}
	if claims.ExpiresAt != nil {
		verified.ExpiresAt = claims.ExpiresAt.Time
	}
	return verified, nil
}
