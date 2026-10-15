package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/abneribeiro/internal/jwt"
	"github.com/abneribeiro/internal/user"
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
	userRepo   user.Repository
	jwtManager *jwt.Manager
}

func NewService(userRepo user.Repository, jwtManager *jwt.Manager) *Service {
	return &Service{
		userRepo:   userRepo,
		jwtManager: jwtManager,
	}
}

func (s *Service) Login(ctx context.Context, input LoginInput) (LoginResponse, error) {
	u, err := s.userRepo.GetByEmail(ctx, user.NormalizeEmail(input.Email))
	if err != nil {
		// Only a genuinely absent row is a credential failure. A dead pool or
		// a cancelled context must not be reported to the client as a 401.
		if errors.Is(err, user.ErrUserNotFound) {
			bcrypt.CompareHashAndPassword(dummyHash, []byte(input.Password))
			return LoginResponse{}, ErrUserNotFound
		}

		return LoginResponse{}, fmt.Errorf("auth.Login: look up user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(input.Password)); err != nil {
		return LoginResponse{}, ErrInvalidCredentials
	}

	token, err := s.jwtManager.GenerateToken(u.ID, u.Email)
	if err != nil {
		return LoginResponse{}, fmt.Errorf("auth.Login: generate token: %w", err)
	}

	return LoginResponse{
		Token:  token,
		UserID: u.ID,
		Email:  u.Email,
	}, nil
}
