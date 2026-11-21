package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	autherr "github.com/cheng-alvin/esplanade/server/auth/errors"
	emailprovider "github.com/cheng-alvin/esplanade/server/auth/providers/email"
	authrepo "github.com/cheng-alvin/esplanade/server/auth/repository"
	"github.com/cheng-alvin/esplanade/server/auth/token"
	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
	"go.uber.org/zap"
)

// ForgotPassword issues a single-use reset token. Responses are identical
// whether or not the account exists (enumeration-safe), and the limiter runs
// before any lookup for the same reason.
func (s *AuthService) ForgotPassword(ctx context.Context, email string) error {
	normalized, err := emailprovider.NormalizeEmail(email)
	if err != nil {
		return err
	}
	if !s.limiter.Allow("forgot:" + normalized) {
		return autherr.ErrRateLimited
	}

	user, err := s.users.FindByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, esmongo.ErrNotFound) {
			return nil
		}
		return err
	}
	if user.EmailAuth == nil {
		return nil
	}

	now := time.Now().UTC()
	if _, err := s.resetTokens.InvalidateAllForUser(ctx, user.ID().Hex(), now.Add(s.cfg.PurgeRetention)); err != nil {
		return err
	}

	raw, err := token.Generate()
	if err != nil {
		return err
	}
	expiresAt := now.Add(s.cfg.PasswordResetTokenTTL)
	reset := &authrepo.PasswordResetToken{
		UserID:    user.ID(),
		TokenHash: token.Hash(raw),
		ExpiresAt: expiresAt,
		CreatedAt: now,
		PurgeAt:   expiresAt.Add(s.cfg.PurgeRetention),
	}
	if err := s.resetTokens.Insert(ctx, reset); err != nil {
		return err
	}

	s.sendPasswordResetEmail(ctx, user.Email, raw)
	return nil
}

// ResetPassword consumes a reset token (CAS-marked used, so a double submit
// can only win once), sets the new password hash, and revokes every session
// of the user (R2) — a reset invalidates any session an attacker may hold.
func (s *AuthService) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	if err := emailprovider.ValidatePassword(newPassword); err != nil {
		return err
	}

	reset, err := s.resetTokens.FindByTokenHash(ctx, token.Hash(rawToken))
	if err != nil {
		if errors.Is(err, esmongo.ErrNotFound) {
			return autherr.ErrTokenInvalid
		}
		return err
	}

	now := time.Now().UTC()
	if reset.UsedAt != nil || !reset.ExpiresAt.After(now) {
		return autherr.ErrTokenInvalid
	}

	user, err := s.users.FindByID(ctx, reset.UserID.Hex())
	if err != nil {
		if errors.Is(err, esmongo.ErrNotFound) {
			return autherr.ErrTokenInvalid
		}
		return err
	}
	if user.EmailAuth == nil {
		return autherr.ErrTokenInvalid
	}

	passwordHash, err := emailprovider.HashPassword(newPassword)
	if err != nil {
		return err
	}

	ok, err := s.resetTokens.MarkUsed(ctx, reset.ID().Hex(), now.Add(s.cfg.PurgeRetention))
	if err != nil {
		return err
	}
	if !ok {
		return autherr.ErrTokenInvalid
	}

	if err := s.users.SetPasswordHash(ctx, user.ID().Hex(), passwordHash); err != nil {
		return err
	}

	_, err = s.sessions.RevokeAllForUser(ctx, user.ID().Hex(), authrepo.RevokedReasonPasswordReset, now.Add(s.cfg.PurgeRetention))
	return err
}

// sendPasswordResetEmail mirrors the verification dispatch: detached context,
// background goroutine, errors logged without the token or the address.
func (s *AuthService) sendPasswordResetEmail(ctx context.Context, to, rawToken string) {
	link := strings.TrimRight(s.cfg.AppBaseURL, "/") + "/reset?token=" + url.QueryEscape(rawToken)
	expires := fmt.Sprintf("%.0f minutes", s.cfg.PasswordResetTokenTTL.Minutes())
	body := fmt.Sprintf(
		"Hi,\n\nUse this link to reset your Esplanade password:\n\n%s\n\nIf the link does not work, enter this code in the app:\n\n%s\n\nThis code expires in %s. If you did not request this, you can ignore this email.\n",
		link, rawToken, expires,
	)
	sendCtx := context.WithoutCancel(ctx)

	go func() {
		if err := s.emails.Send(sendCtx, to, "Reset your Esplanade password", body); err != nil {
			s.logger.Error("sending password reset email", zap.Error(err))
		}
	}()
}
