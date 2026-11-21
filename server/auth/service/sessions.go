package service

import (
	"context"
	"errors"
	"time"

	autherr "github.com/cheng-alvin/esplanade/server/auth/errors"
	emailprovider "github.com/cheng-alvin/esplanade/server/auth/providers/email"
	authrepo "github.com/cheng-alvin/esplanade/server/auth/repository"
	"github.com/cheng-alvin/esplanade/server/auth/token"
	"github.com/cheng-alvin/esplanade/server/devices"
	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
)

// newSession resolves the caller's device, stores the refresh-token session,
// and returns the access/refresh pair.
func (s *AuthService) newSession(ctx context.Context, user *authrepo.User, deviceIdentifier, userAgent string) (*TokenPair, error) {
	deviceID, err := s.devices.FindOrRegister(ctx, user.ID(), deviceIdentifier)
	if err != nil {
		return nil, err
	}

	raw, err := token.Generate()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	expiresAt := now.Add(s.cfg.RefreshTokenTTL)
	session := &authrepo.Session{
		UserID:           user.ID(),
		DeviceID:         deviceID,
		RefreshTokenHash: token.Hash(raw),
		UserAgent:        userAgent,
		IssuedAt:         now,
		LastRefreshedAt:  now,
		ExpiresAt:        expiresAt,
		PurgeAt:          expiresAt.Add(s.cfg.PurgeRetention),
	}
	if err := s.sessions.Insert(ctx, session); err != nil {
		return nil, err
	}

	return s.issuePair(user, deviceID.Hex(), raw)
}

// issuePair signs an access token for the user/device and pairs it with the
// supplied refresh token.
func (s *AuthService) issuePair(user *authrepo.User, deviceID, refreshToken string) (*TokenPair, error) {
	access, expiresIn, err := s.issuer.Issue(user.ID().Hex(), user.Email, deviceID)
	if err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, RefreshToken: refreshToken, ExpiresIn: expiresIn}, nil
}

// Register creates an email/password account, stores the pending verification
// token on the same document (single write), dispatches the verification
// email, and signs the new device in.
func (s *AuthService) Register(ctx context.Context, email, password, deviceIdentifier, userAgent string) (*TokenPair, error) {
	normalized, err := emailprovider.NormalizeEmail(email)
	if err != nil {
		return nil, err
	}
	if err := emailprovider.ValidatePassword(password); err != nil {
		return nil, err
	}
	if _, err := devices.ValidateIdentifier(deviceIdentifier); err != nil {
		return nil, err
	}

	if _, err := s.users.FindByEmail(ctx, normalized); err == nil {
		return nil, autherr.ErrUserExists
	} else if !errors.Is(err, esmongo.ErrNotFound) {
		return nil, err
	}

	passwordHash, err := emailprovider.HashPassword(password)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	rawToken, verification, err := s.newVerificationToken(now)
	if err != nil {
		return nil, err
	}

	user := &authrepo.User{
		Email:       normalized,
		DisplayName: localPart(normalized),
		Status:      authrepo.UserStatusActive,
		EmailAuth: &authrepo.EmailAuthMethod{
			AuthMethodCommon: authrepo.AuthMethodCommon{
				Provider:  authrepo.AuthProviderEmail,
				CreatedAt: now,
				UpdatedAt: now,
			},
			PasswordHash:      passwordHash,
			EmailVerified:     false,
			VerificationToken: verification,
		},
	}
	if err := s.users.Insert(ctx, user); err != nil {
		if errors.Is(err, esmongo.ErrConflict) {
			return nil, autherr.ErrUserExists
		}
		return nil, err
	}

	s.sendVerificationEmail(ctx, user.Email, rawToken)
	return s.newSession(ctx, user, deviceIdentifier, userAgent)
}

// Login verifies email/password credentials. Unknown accounts, accounts
// without an email method, and wrong passwords all return the same sentinel;
// a dummy hash verification equalizes timing (R12). Unverified accounts still
// log in — action-level gating happens in RequireVerifiedEmail (R6).
func (s *AuthService) Login(ctx context.Context, email, password, deviceIdentifier, userAgent string) (*TokenPair, error) {
	normalized, err := emailprovider.NormalizeEmail(email)
	if err != nil {
		return nil, err
	}

	user, err := s.users.FindByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, esmongo.ErrNotFound) {
			emailprovider.VerifyPassword(password, s.dummyHash)
			return nil, autherr.ErrInvalidCredentials
		}
		return nil, err
	}
	if user.EmailAuth == nil {
		emailprovider.VerifyPassword(password, s.dummyHash)
		return nil, autherr.ErrInvalidCredentials
	}
	if !emailprovider.VerifyPassword(password, user.EmailAuth.PasswordHash) {
		return nil, autherr.ErrInvalidCredentials
	}
	if user.Status != authrepo.UserStatusActive {
		return nil, autherr.ErrInvalidCredentials
	}

	return s.newSession(ctx, user, deviceIdentifier, userAgent)
}

// Refresh implements the rotation flow of AUTH.md §8: the presented token's
// hash must match the session's current hash, validity is checked explicitly
// (revocation and expiry), the rotation is a CAS so a concurrent refresh
// cannot mint two current tokens, and any replay of a rotated-out token
// triggers R1's revoke-everything response.
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	hash := token.Hash(refreshToken)
	now := time.Now().UTC()

	session, err := s.sessions.FindByRefreshTokenHash(ctx, hash)
	switch {
	case err == nil:
		if session.RevokedAt != nil || !session.ExpiresAt.After(now) {
			return nil, autherr.ErrTokenInvalid
		}

		user, err := s.users.FindByID(ctx, session.UserID.Hex())
		if err != nil {
			if !errors.Is(err, esmongo.ErrNotFound) {
				return nil, err
			}
			_ = s.sessions.Revoke(ctx, session.ID().Hex(), authrepo.RevokedReasonUserInactive, now.Add(s.cfg.PurgeRetention))
			return nil, autherr.ErrTokenInvalid
		}
		if user.Status != authrepo.UserStatusActive {
			_ = s.sessions.Revoke(ctx, session.ID().Hex(), authrepo.RevokedReasonUserInactive, now.Add(s.cfg.PurgeRetention))
			return nil, autherr.ErrTokenInvalid
		}

		newRaw, err := token.Generate()
		if err != nil {
			return nil, err
		}
		expiresAt := now.Add(s.cfg.RefreshTokenTTL)
		ok, err := s.sessions.Rotate(ctx, session.ID().Hex(), hash, token.Hash(newRaw), expiresAt, expiresAt.Add(s.cfg.PurgeRetention))
		if err != nil {
			return nil, err
		}
		if !ok {
			// A concurrent refresh won the CAS; classify the presented
			// token against the rotation history like any other replay.
			return nil, s.handleReuseDetection(ctx, hash)
		}
		return s.issuePair(user, session.DeviceID.Hex(), newRaw)

	case errors.Is(err, esmongo.ErrNotFound):
		return nil, s.handleReuseDetection(ctx, hash)

	default:
		return nil, err
	}
}

// handleReuseDetection applies R1: a rotated-out token presented again means
// interception, so the implicated session and all of the user's other
// sessions are revoked before the 401 is returned. Revocation errors must not
// mask the 401.
func (s *AuthService) handleReuseDetection(ctx context.Context, hash string) error {
	session, err := s.sessions.FindByPreviousTokenHash(ctx, hash)
	if err != nil {
		return autherr.ErrTokenInvalid
	}

	now := time.Now().UTC()
	purgeAt := now.Add(s.cfg.PurgeRetention)
	_ = s.sessions.Revoke(ctx, session.ID().Hex(), authrepo.RevokedReasonReuseDetected, purgeAt)
	_, _ = s.sessions.RevokeAllForUser(ctx, session.UserID.Hex(), authrepo.RevokedReasonReuseDetected, purgeAt)
	return autherr.ErrTokenInvalid
}

// Logout is idempotent: when the refresh token identifies the caller's
// session, that session is deleted; otherwise every session for the caller's
// device is deleted.
func (s *AuthService) Logout(ctx context.Context, userID, deviceID, refreshToken string) error {
	if refreshToken != "" {
		hash := token.Hash(refreshToken)
		if session, err := s.sessions.FindByRefreshTokenHash(ctx, hash); err == nil && session.UserID.Hex() == userID {
			_ = s.sessions.Delete(ctx, session.ID().Hex())
			return nil
		}
	}

	if deviceID != "" {
		_, _ = s.sessions.DeleteForUserDevice(ctx, userID, deviceID)
	}
	return nil
}

// LogoutAll revokes every session of the user ("sign out of all devices").
func (s *AuthService) LogoutAll(ctx context.Context, userID string) error {
	_, err := s.sessions.RevokeAllForUser(ctx, userID, authrepo.RevokedReasonLogoutAll, time.Now().UTC().Add(s.cfg.PurgeRetention))
	return err
}
