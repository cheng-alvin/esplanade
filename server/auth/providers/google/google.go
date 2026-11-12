// Package google verifies Google OIDC ID tokens for sign-in and account linking.
package google

import (
	"context"
	"fmt"
	"sync"
	"time"

	autherr "github.com/cheng-alvin/esplanade/server/auth/errors"
	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	googleIssuer     = "https://accounts.google.com"
	discoveryTimeout = 10 * time.Second
)

// Identity is the Google profile extracted from a verified ID token.
type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool // captured but not used for gating (R6)
	Name          string
	Picture       string
}

// Verifier verifies a raw Google ID token and returns the identity it asserts.
type Verifier interface {
	Verify(ctx context.Context, rawIDToken string) (*Identity, error)
}

// OIDCVerifier is a Verifier backed by Google's OIDC discovery document. The
// discovery fetch is deferred to the first Verify so the server can start
// without network access.
type OIDCVerifier struct {
	clientID string

	// once synchronizes the lazy init: the effects of its closure are
	// visible to every caller once Do returns, so no separate mutex is
	// needed to read verifier and initErr.
	once     sync.Once
	verifier *oidc.IDTokenVerifier
	initErr  error
}

// New returns an OIDCVerifier that accepts ID tokens issued to clientID.
func New(clientID string) *OIDCVerifier {
	return &OIDCVerifier{clientID: clientID}
}

// Verify validates the ID token's signature, issuer, and audience, and
// returns the identity it asserts.
func (v *OIDCVerifier) Verify(ctx context.Context, rawIDToken string) (*Identity, error) {
	v.once.Do(func() {
		// The fetch is bounded by its own context, not the request's: once
		// is one-shot, so a canceled request during discovery would poison
		// the verifier for the process lifetime.
		initCtx, cancel := context.WithTimeout(context.Background(), discoveryTimeout)
		defer cancel()

		provider, err := oidc.NewProvider(initCtx, googleIssuer)
		if err != nil {
			v.initErr = fmt.Errorf("google: initializing oidc provider: %w", err)
			return
		}
		v.verifier = provider.Verifier(&oidc.Config{ClientID: v.clientID})
	})

	// Discovery failures are infrastructure errors, not bad credentials:
	// they carry no autherr sentinel, so the service maps them to a 500,
	// while the token-level failures below wrap autherr.ErrTokenInvalid.
	if v.initErr != nil {
		return nil, v.initErr
	}

	idToken, err := v.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("google: verifying id token: %w", autherr.ErrTokenInvalid)
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("google: decoding id token claims: %w", autherr.ErrTokenInvalid)
	}
	if idToken.Subject == "" || claims.Email == "" {
		return nil, fmt.Errorf("google: id token missing subject or email: %w", autherr.ErrTokenInvalid)
	}

	return &Identity{
		Subject:       idToken.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		Name:          claims.Name,
		Picture:       claims.Picture,
	}, nil
}
