// Package service orchestrates the authentication flows: credentials,
// sessions and token rotation, email verification, password reset, and
// provider linking.
package service

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/cheng-alvin/esplanade/server/auth/jwt"
	emailprovider "github.com/cheng-alvin/esplanade/server/auth/providers/email"
	"github.com/cheng-alvin/esplanade/server/auth/providers/google"
	authrepo "github.com/cheng-alvin/esplanade/server/auth/repository"
	"github.com/cheng-alvin/esplanade/server/auth/token"
	"github.com/cheng-alvin/esplanade/server/config"
	"github.com/cheng-alvin/esplanade/server/devices"
	"github.com/cheng-alvin/esplanade/server/email"
	"github.com/cheng-alvin/esplanade/server/ratelimit"
	"go.uber.org/zap"
)

// TokenPair is the access/refresh pair returned by the credential flows.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// Profile is the authenticated user's public profile.
type Profile struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	DisplayName   string    `json:"display_name"`
	AvatarURL     *string   `json:"avatar_url"`
	Status        string    `json:"status"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
}

// Identity is one linked authentication method.
type Identity struct {
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"created_at"`
}

// Deps are the collaborators an AuthService is constructed with.
type Deps struct {
	Config      *config.Config
	Logger      *zap.Logger
	Users       *authrepo.UserRepository
	Sessions    *authrepo.SessionRepository
	ResetTokens *authrepo.PasswordResetRepository
	Devices     *devices.Store
	Emails      email.Sender
	Google      google.Verifier
	Issuer      *jwt.Issuer
	Limiter     *ratelimit.Limiter
}

// AuthService orchestrates the authentication flows.
type AuthService struct {
	cfg         *config.Config
	logger      *zap.Logger
	users       *authrepo.UserRepository
	sessions    *authrepo.SessionRepository
	resetTokens *authrepo.PasswordResetRepository
	devices     *devices.Store
	emails      email.Sender
	google      google.Verifier
	issuer      *jwt.Issuer
	limiter     *ratelimit.Limiter

	// dummyHash gives the login path a comparable cost when the account or
	// its password method does not exist (R12).
	dummyHash string
}

func New(deps Deps) *AuthService {
	dummy, _ := emailprovider.HashPassword("esplanade-timing-equalizer")
	return &AuthService{
		cfg:         deps.Config,
		logger:      deps.Logger,
		users:       deps.Users,
		sessions:    deps.Sessions,
		resetTokens: deps.ResetTokens,
		devices:     deps.Devices,
		emails:      deps.Emails,
		google:      deps.Google,
		issuer:      deps.Issuer,
		limiter:     deps.Limiter,
		dummyHash:   dummy,
	}
}

// sendVerificationEmail composes and dispatches the verification message in
// the background: the request context is detached (WithoutCancel) so a slow
// SMTP provider or a returned response can never cancel the send, and a
// failed send never fails registration.
func (s *AuthService) sendVerificationEmail(ctx context.Context, to, rawToken string) {
	link := strings.TrimRight(s.cfg.AppBaseURL, "/") + "/verify?token=" + url.QueryEscape(rawToken)
	expires := fmt.Sprintf("%.0f hours", s.cfg.VerificationTokenTTL.Hours())
	body := fmt.Sprintf(
		"Hi,\n\nConfirm your Esplanade email address by opening this link:\n\n%s\n\nIf the link does not work, enter this code in the app:\n\n%s\n\nThis code expires in %s.\n",
		link, rawToken, expires,
	)
	sendCtx := context.WithoutCancel(ctx)

	go func() {
		if err := s.emails.Send(sendCtx, to, "Verify your Esplanade email", body); err != nil {
			// Neither the token nor the address belongs in logs.
			s.logger.Error("sending verification email", zap.Error(err))
		}
	}()
}

// newVerificationToken mints a raw verification token and its persisted form.
// The attempt budget is reserved for a future code-entry endpoint (R9).
func (s *AuthService) newVerificationToken(now time.Time) (string, *authrepo.EmailVerificationToken, error) {
	raw, err := token.Generate()
	if err != nil {
		return "", nil, err
	}
	return raw, &authrepo.EmailVerificationToken{
		TokenHash:    token.Hash(raw),
		ExpiresAt:    now.Add(s.cfg.VerificationTokenTTL),
		CreatedAt:    now,
		AttemptsLeft: 5,
	}, nil
}

// localPart returns the part of an address before the first "@".
func localPart(address string) string {
	if at := strings.IndexByte(address, '@'); at >= 0 {
		return address[:at]
	}
	return address
}
