package user

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidUser = errors.New("invalid user data")

type Service interface {
	Create(ctx context.Context, input CreateInput) (User, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Create(ctx context.Context, input CreateInput) (User, error) {

	if strings.TrimSpace(input.Name) == "" || validateUser(input.Name, input.Email) != nil {
		return User{}, fmt.Errorf("%w: name is required", ErrInvalidUser)
	}

	if strings.TrimSpace(input.Email) == "" || validateUser(input.Name, input.Email) != nil {
		return User{}, fmt.Errorf("%w: invalid email or name", ErrInvalidUser)
	}

	if strings.TrimSpace(input.Password) == "" {
		return User{}, fmt.Errorf("%w: password is required", ErrInvalidUser)
	}

	if len(strings.TrimSpace(input.Password)) < 8 {
		return User{}, fmt.Errorf("%w: password must be at least 8 characters", ErrInvalidUser)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, fmt.Errorf("%w: failed to hash password", ErrInvalidUser)
	}

	input.Password = string(hashedPassword)

	return s.repo.Create(ctx, input)
}