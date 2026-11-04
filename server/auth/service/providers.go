package service

import (
	"context"
	"errors"
	"time"

	autherr "github.com/cheng-alvin/esplanade/server/auth/errors"
	authrepo "github.com/cheng-alvin/esplanade/server/auth/repository"
	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
)

// Identities lists the user's linked authentication methods, read straight
// off the already-loaded document.
func (s *AuthService) Identities(ctx context.Context, userID string) ([]Identity, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, esmongo.ErrNotFound) {
			return nil, autherr.ErrUserNotFound
		}
		return nil, err
	}

	identities := make([]Identity, 0, 2)
	if user.EmailAuth != nil {
		identities = append(identities, Identity{
			Provider:  string(authrepo.AuthProviderEmail),
			CreatedAt: user.EmailAuth.CreatedAt,
		})
	}
	if user.GoogleAuth != nil {
		identities = append(identities, Identity{
			Provider:  string(authrepo.AuthProviderGoogle),
			CreatedAt: user.GoogleAuth.CreatedAt,
		})
	}
	return identities, nil
}

// UnlinkProvider removes a linked method. A provider that is not linked is a
// 404 even when the account has a single method; removing the only method is
// a 409. Unsetting email_auth also invalidates outstanding reset tokens and
// discards any pending verification token implicitly. No session revocation
// is needed: sessions authenticate the user, not the provider.
func (s *AuthService) UnlinkProvider(ctx context.Context, userID, provider string) error {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, esmongo.ErrNotFound) {
			return autherr.ErrUserNotFound
		}
		return err
	}

	switch provider {
	case string(authrepo.AuthProviderEmail):
		if user.EmailAuth == nil {
			return autherr.ErrProviderNotFound
		}
	case string(authrepo.AuthProviderGoogle):
		if user.GoogleAuth == nil {
			return autherr.ErrProviderNotFound
		}
	default:
		return autherr.ErrUnsupportedProvider
	}

	linked := 0
	if user.EmailAuth != nil {
		linked++
	}
	if user.GoogleAuth != nil {
		linked++
	}
	if linked <= 1 {
		return autherr.ErrLastAuthMethod
	}

	if provider == string(authrepo.AuthProviderEmail) {
		if err := s.users.UnsetEmailAuth(ctx, user.ID().Hex()); err != nil {
			return err
		}
		_, err := s.resetTokens.InvalidateAllForUser(ctx, user.ID().Hex(), time.Now().UTC().Add(s.cfg.PurgeRetention))
		return err
	}
	return s.users.UnsetGoogleAuth(ctx, user.ID().Hex())
}
