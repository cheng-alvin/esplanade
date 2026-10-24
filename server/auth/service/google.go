package service

import (
	"context"
	"errors"
	"strings"
	"time"

	autherr "github.com/cheng-alvin/esplanade/server/auth/errors"
	"github.com/cheng-alvin/esplanade/server/auth/providers/google"
	authrepo "github.com/cheng-alvin/esplanade/server/auth/repository"
	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
)

// GoogleLogin exchanges a verified Google ID token for a session. Identity
// comes from the sub claim; per R6 the email_verified claim is never gated
// on, and an unknown subject whose email matches an existing account is NOT
// auto-linked — the caller must sign in and link explicitly.
func (s *AuthService) GoogleLogin(ctx context.Context, idToken, deviceIdentifier, userAgent string) (*TokenPair, error) {
	identity, err := s.verifyGoogle(ctx, idToken)
	if err != nil {
		return nil, err
	}
	email := strings.ToLower(strings.TrimSpace(identity.Email))

	user, err := s.users.FindByGoogleSubject(ctx, identity.Subject)
	switch {
	case err == nil:
		if user.Status != authrepo.UserStatusActive {
			return nil, autherr.ErrInvalidCredentials
		}
		return s.newSession(ctx, user, deviceIdentifier, userAgent)

	case !errors.Is(err, esmongo.ErrNotFound):
		return nil, err
	}

	if _, err := s.users.FindByEmail(ctx, email); err == nil {
		return nil, autherr.ErrLinkRequired
	} else if !errors.Is(err, esmongo.ErrNotFound) {
		return nil, err
	}

	now := time.Now().UTC()
	user = &authrepo.User{
		Email:       email,
		DisplayName: googleDisplayName(identity.Name, email),
		AvatarURL:   picturePointer(identity.Picture),
		Status:      authrepo.UserStatusActive,
		GoogleAuth: &authrepo.GoogleAuthMethod{
			AuthMethodCommon: authrepo.AuthMethodCommon{
				Provider:  authrepo.AuthProviderGoogle,
				CreatedAt: now,
				UpdatedAt: now,
			},
			Subject: identity.Subject,
			Email:   email,
			Picture: picturePointer(identity.Picture),
		},
	}
	if err := s.users.Insert(ctx, user); err != nil {
		if errors.Is(err, esmongo.ErrConflict) {
			// Re-resolve once: a concurrent sign-in or registration won the
			// unique index between the lookup and the insert.
			if existing, ferr := s.users.FindByGoogleSubject(ctx, identity.Subject); ferr == nil {
				if existing.Status != authrepo.UserStatusActive {
					return nil, autherr.ErrInvalidCredentials
				}
				return s.newSession(ctx, existing, deviceIdentifier, userAgent)
			}
			if _, ferr := s.users.FindByEmail(ctx, email); ferr == nil {
				return nil, autherr.ErrLinkRequired
			}
		}
		return nil, err
	}

	return s.newSession(ctx, user, deviceIdentifier, userAgent)
}

// LinkGoogle attaches a Google account to the authenticated user. Linking is
// only ever explicit (R6); the Google-reported email may differ from the
// account email and is kept on the Google method.
func (s *AuthService) LinkGoogle(ctx context.Context, userID, idToken string) error {
	identity, err := s.verifyGoogle(ctx, idToken)
	if err != nil {
		return err
	}
	email := strings.ToLower(strings.TrimSpace(identity.Email))

	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, esmongo.ErrNotFound) {
			return autherr.ErrUserNotFound
		}
		return err
	}

	if user.GoogleAuth != nil {
		if user.GoogleAuth.Subject == identity.Subject {
			return nil
		}
		return autherr.ErrGoogleAlreadyLinked
	}

	if _, err := s.users.FindByGoogleSubject(ctx, identity.Subject); err == nil {
		return autherr.ErrGoogleAlreadyLinked
	} else if !errors.Is(err, esmongo.ErrNotFound) {
		return err
	}

	now := time.Now().UTC()
	method := &authrepo.GoogleAuthMethod{
		AuthMethodCommon: authrepo.AuthMethodCommon{
			Provider:  authrepo.AuthProviderGoogle,
			CreatedAt: now,
			UpdatedAt: now,
		},
		Subject: identity.Subject,
		Email:   email,
		Picture: picturePointer(identity.Picture),
	}
	if err := s.users.SetGoogleAuth(ctx, user.ID().Hex(), method); err != nil {
		if errors.Is(err, esmongo.ErrConflict) {
			return autherr.ErrGoogleAlreadyLinked
		}
		return err
	}
	return nil
}

// verifyGoogle verifies an ID token and maps token-level failures onto the
// API sentinel; infrastructure failures (discovery/network) propagate
// unchanged so they surface as a 500 rather than a credential rejection.
func (s *AuthService) verifyGoogle(ctx context.Context, rawIDToken string) (*google.Identity, error) {
	if rawIDToken == "" {
		return nil, autherr.ErrInvalidGoogleToken
	}
	identity, err := s.google.Verify(ctx, rawIDToken)
	if err != nil {
		if errors.Is(err, autherr.ErrTokenInvalid) {
			return nil, autherr.ErrInvalidGoogleToken
		}
		return nil, err
	}
	return identity, nil
}

func googleDisplayName(name, email string) string {
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		return trimmed
	}
	return localPart(email)
}

func picturePointer(picture string) *string {
	if picture == "" {
		return nil
	}
	return &picture
}
