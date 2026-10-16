package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/abneribeiro/internal/jwt"
	"github.com/abneribeiro/internal/metrics"
	"github.com/abneribeiro/internal/user"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// dummyHash is compared against when the account does not exist, so a missing
// account and a wrong password cost the same bcrypt work. Without it the
// response time alone reveals which emails are registered.
var dummyHash = mustHash("timing-equalisation-placeholder")

func mustHash(s string) []byte {
	h, err := bcrypt.GenerateFromPassword([]byte(s), bcrypt.DefaultCost)
	if err != nil {
		panic("auth: cannot hash timing placeholder: " + err.Error())
	}

	return h
}

type Service struct {
	userRepo    user.Repository
	refreshRepo RefreshRepository
	jwtManager  *jwt.Manager
}

func NewService(userRepo user.Repository, refreshRepo RefreshRepository, jwtManager *jwt.Manager) *Service {
	return &Service{
		userRepo:    userRepo,
		refreshRepo: refreshRepo,
		jwtManager:  jwtManager,
	}
}

func (s *Service) Login(ctx context.Context, input LoginInput) (LoginResponse, error) {
	u, err := s.userRepo.GetByEmail(ctx, user.NormalizeEmail(input.Email))
	if err != nil {
		// Only a genuinely absent row is a credential failure. A dead pool or
		// a cancelled context must not be reported to the client as a 401.
		if errors.Is(err, user.ErrUserNotFound) {
			bcrypt.CompareHashAndPassword(dummyHash, []byte(input.Password))
			metrics.RecordLoginAttempt("unknown_user")
			return LoginResponse{}, ErrUserNotFound
		}

		metrics.RecordLoginAttempt("error")

		return LoginResponse{}, fmt.Errorf("auth.Login: look up user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(input.Password)); err != nil {
		metrics.RecordLoginAttempt("bad_password")
		return LoginResponse{}, ErrInvalidCredentials
	}

	// A fresh login starts a new rotation family.
	resp, err := s.issue(ctx, u, uuid.New())
	if err != nil {
		metrics.RecordLoginAttempt("error")
		return LoginResponse{}, err
	}

	metrics.RecordLoginAttempt("success")

	return resp, nil
}

// Refresh rotates a refresh token: the presented one is revoked and a new one
// from the same family is issued.
func (s *Service) Refresh(ctx context.Context, input RefreshInput) (LoginResponse, error) {
	if input.RefreshToken == "" {
		return LoginResponse{}, ErrInvalidRefresh
	}

	stored, err := s.refreshRepo.FindByHash(ctx, hashRefreshToken(input.RefreshToken))
	if err != nil {
		if errors.Is(err, ErrInvalidRefresh) {
			metrics.RecordLoginAttempt("bad_refresh")
			return LoginResponse{}, ErrInvalidRefresh
		}

		return LoginResponse{}, fmt.Errorf("auth.Refresh: look up token: %w", err)
	}

	// A revoked token being presented means it was rotated already and then
	// replayed: the holder is not the only one with it. Kill the whole chain
	// rather than just refusing this one request.
	if stored.revoked() {
		slog.WarnContext(ctx, "refresh token reuse detected, revoking family",
			"user_id", stored.UserID, "family_id", stored.FamilyID)

		if err := s.refreshRepo.RevokeFamily(ctx, stored.FamilyID); err != nil {
			return LoginResponse{}, fmt.Errorf("auth.Refresh: revoke family: %w", err)
		}

		metrics.RecordLoginAttempt("refresh_reuse")

		return LoginResponse{}, ErrInvalidRefresh
	}

	if stored.expired(time.Now()) {
		metrics.RecordLoginAttempt("expired_refresh")
		return LoginResponse{}, ErrInvalidRefresh
	}

	u, err := s.userRepo.GetByID(ctx, stored.UserID)
	if err != nil {
		if errors.Is(err, user.ErrUserNotFound) {
			return LoginResponse{}, ErrInvalidRefresh
		}

		return LoginResponse{}, fmt.Errorf("auth.Refresh: look up user: %w", err)
	}

	if err := s.refreshRepo.Revoke(ctx, stored.ID); err != nil {
		return LoginResponse{}, fmt.Errorf("auth.Refresh: revoke rotated token: %w", err)
	}

	resp, err := s.issue(ctx, u, stored.FamilyID)
	if err != nil {
		return LoginResponse{}, err
	}

	metrics.RecordLoginAttempt("refresh")

	return resp, nil
}

// Logout revokes the presented token's whole family, so the session cannot be
// resumed from an older link in the chain.
func (s *Service) Logout(ctx context.Context, input RefreshInput) error {
	if input.RefreshToken == "" {
		return ErrInvalidRefresh
	}

	stored, err := s.refreshRepo.FindByHash(ctx, hashRefreshToken(input.RefreshToken))
	if err != nil {
		if errors.Is(err, ErrInvalidRefresh) {
			// Logging out with an unknown token is not an error worth
			// reporting: the caller ends up logged out either way.
			return nil
		}

		return fmt.Errorf("auth.Logout: look up token: %w", err)
	}

	if err := s.refreshRepo.RevokeFamily(ctx, stored.FamilyID); err != nil {
		return fmt.Errorf("auth.Logout: revoke family: %w", err)
	}

	return nil
}

// LogoutAll revokes every session for a user, used after a password change or
// a suspected compromise.
func (s *Service) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	if err := s.refreshRepo.RevokeAllForUser(ctx, userID); err != nil {
		return fmt.Errorf("auth.LogoutAll: %w", err)
	}

	return nil
}

// issue mints an access token plus a refresh token in the given family.
func (s *Service) issue(ctx context.Context, u user.User, familyID uuid.UUID) (LoginResponse, error) {
	accessToken, err := s.jwtManager.GenerateToken(u.ID, u.Email, u.Role)
	if err != nil {
		return LoginResponse{}, fmt.Errorf("auth.issue: generate access token: %w", err)
	}

	refreshToken, hash, err := newRefreshToken()
	if err != nil {
		return LoginResponse{}, fmt.Errorf("auth.issue: %w", err)
	}

	tokenID, err := uuid.NewV7()
	if err != nil {
		return LoginResponse{}, fmt.Errorf("auth.issue: generate token id: %w", err)
	}

	expiresAt := time.Now().Add(RefreshTokenTTL)
	if err := s.refreshRepo.Store(ctx, tokenID, u.ID, familyID, hash, expiresAt); err != nil {
		return LoginResponse{}, fmt.Errorf("auth.issue: %w", err)
	}

	return LoginResponse{
		Token:        accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(jwt.AccessTokenTTL.Seconds()),
		UserID:       u.ID,
		Email:        u.Email,
		Role:         u.Role,
	}, nil
}
