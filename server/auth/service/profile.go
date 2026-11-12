package service

import (
	"context"
	"errors"

	autherr "github.com/cheng-alvin/esplanade/server/auth/errors"
	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
)

// Me returns the public profile of the authenticated user. EmailVerified is
// the R6 combined notion: a Google-linked account counts as verified.
func (s *AuthService) Me(ctx context.Context, userID string) (*Profile, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, esmongo.ErrNotFound) {
			return nil, autherr.ErrUserNotFound
		}
		return nil, err
	}

	return &Profile{
		ID:            user.ID().Hex(),
		Email:         user.Email,
		DisplayName:   user.DisplayName,
		AvatarURL:     user.AvatarURL,
		Status:        user.Status,
		EmailVerified: user.GoogleAuth != nil || (user.EmailAuth != nil && user.EmailAuth.EmailVerified),
		CreatedAt:     user.CreatedAt,
	}, nil
}
