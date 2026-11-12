package service

import (
	"context"
	"errors"
	"time"

	autherr "github.com/cheng-alvin/esplanade/server/auth/errors"
	emailprovider "github.com/cheng-alvin/esplanade/server/auth/providers/email"
	authrepo "github.com/cheng-alvin/esplanade/server/auth/repository"
	"github.com/cheng-alvin/esplanade/server/auth/token"
	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
)

// VerifyEmail consumes a verification token: reverse lookup by hash, explicit
// expiry check, then a single atomic update that sets email_verified and
// unsets the token. A consumed token no longer resolves — indistinguishable
// from garbage (accepted tradeoff of not retaining consumed tokens).
func (s *AuthService) VerifyEmail(ctx context.Context, rawToken string) error {
	user, err := s.users.FindByVerificationTokenHash(ctx, token.Hash(rawToken))
	if err != nil {
		if errors.Is(err, esmongo.ErrNotFound) {
			return autherr.ErrTokenInvalid
		}
		return err
	}

	pending := user.EmailAuth
	if pending == nil || pending.VerificationToken == nil || !pending.VerificationToken.ExpiresAt.After(time.Now().UTC()) {
		return autherr.ErrTokenInvalid
	}

	return s.users.MarkEmailVerified(ctx, user.ID().Hex())
}

// ResendVerification reissues a verification token. Authenticated callers
// resend to their own address; unauthenticated callers submit an address and
// receive the enumeration-safe response either way. The limiter is applied
// before any lookup so a rejection cannot reveal whether the account exists,
// and it is the primary abuse control for this email-sending endpoint (R3).
func (s *AuthService) ResendVerification(ctx context.Context, userID, email string) error {
	now := time.Now().UTC()

	if userID != "" {
		user, err := s.users.FindByID(ctx, userID)
		if err != nil {
			return err
		}
		if user.EmailAuth == nil || user.EmailAuth.EmailVerified || user.GoogleAuth != nil {
			return autherr.ErrEmailAlreadyVerified
		}
		if !s.limiter.Allow("resend:" + user.Email) {
			return autherr.ErrRateLimited
		}
		return s.issueVerification(ctx, user, now)
	}

	normalized, err := emailprovider.NormalizeEmail(email)
	if err != nil {
		return err
	}
	if !s.limiter.Allow("resend:" + normalized) {
		return autherr.ErrRateLimited
	}

	user, err := s.users.FindByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, esmongo.ErrNotFound) {
			return nil
		}
		return err
	}
	if user.EmailAuth == nil || user.EmailAuth.EmailVerified || user.GoogleAuth != nil {
		return nil
	}
	return s.issueVerification(ctx, user, now)
}

// issueVerification mints and stores a fresh token — the single $set replaces
// any prior value wholesale, so there is nothing to invalidate separately.
func (s *AuthService) issueVerification(ctx context.Context, user *authrepo.User, now time.Time) error {
	raw, verification, err := s.newVerificationToken(now)
	if err != nil {
		return err
	}
	if err := s.users.SetVerificationToken(ctx, user.ID().Hex(), verification); err != nil {
		return err
	}
	s.sendVerificationEmail(ctx, user.Email, raw)
	return nil
}
