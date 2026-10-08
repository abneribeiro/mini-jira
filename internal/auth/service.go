package auth

import (
	"context"
	"fmt"

	"github.com/abneribeiro/internal/jwt"
	"github.com/abneribeiro/internal/user"
	"golang.org/x/crypto/bcrypt"
)

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
	u, err := s.userRepo.GetByEmail(ctx, input.Email)
	if err != nil {
		return LoginResponse{}, ErrUserNotFound
	}

	err = bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(input.Password))
	if err != nil {
		return LoginResponse{}, ErrInvalidCredentials
	}

	token, err := s.jwtManager.GenerateToken(u.ID, u.Email)
	if err != nil {
		return LoginResponse{}, fmt.Errorf("failed to generate token: %w", err)
	}

	return LoginResponse{
		Token:  token,
		UserID: u.ID,
		Email:  u.Email,
	}, nil
}
