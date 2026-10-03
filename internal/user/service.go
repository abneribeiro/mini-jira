package user

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidUser        = errors.New("invalid user data")
	ErrEmailAlreadyExists = errors.New("unable to create account")
)

type Service interface {
	Create(ctx context.Context, input CreateInput) (User, error)
	Update(ctx context.Context, id int64, input UpdateInput) (User, error)
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

	createdUser, err := s.repo.Create(ctx, input)
	if err != nil {
		if strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "users_email_key") {
			return User{}, ErrEmailAlreadyExists
		}
		return User{}, fmt.Errorf("failed to save user: %w", err)
	}

	return createdUser, nil
}


func (s *service) Update(ctx context.Context, id int64,  input UpdateInput) (User, error) {

	if id <= 0 {
		return User{}, fmt.Errorf("%w: invalid user ID", ErrInvalidUser)
	}

	if input.Password != nil {
		if len(strings.TrimSpace(*input.Password)) < 8 {
			return User{}, fmt.Errorf("%w: password must be at least 8 characters", ErrInvalidUser)
		}

		hashBytes, err := bcrypt.GenerateFromPassword([]byte(*input.Password), bcrypt.DefaultCost)
		if err != nil {
			return User{}, fmt.Errorf("service.Update: failed to hash new password: %w", err)
		}
		hashedStr := string(hashBytes)
		input.Password = &hashedStr
	}
	
	updateUser, err := s.repo.Update(ctx, id , input);
	if err != nil {
		return User{}, fmt.Errorf("failed to save user: %w", err)
	}

	return  updateUser, nil
}