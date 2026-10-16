package user

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type Service interface {
	Create(ctx context.Context, input CreateInput) (User, error)
	GetByID(ctx context.Context, id uuid.UUID) (User, error)
	List(ctx context.Context, filter ListFilter) ([]User, error)
	Update(ctx context.Context, id uuid.UUID, input UpdateInput) (User, error)
	UpdateRole(ctx context.Context, id uuid.UUID, role string) (User, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Create(ctx context.Context, input CreateInput) (User, error) {
	input.Email = NormalizeEmail(input.Email)

	if err := validateUser(input.Name, input.Email); err != nil {
		return User{}, err
	}

	if err := validatePassword(input.Password); err != nil {
		return User{}, err
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, fmt.Errorf("service.Create: hash password: %w", err)
	}
	input.Password = string(hashedPassword)

	newID, err := uuid.NewV7()
	if err != nil {
		return User{}, fmt.Errorf("service.Create: generate uuid: %w", err)
	}

	return s.repo.Create(ctx, newID, input)
}

func (s *service) Update(ctx context.Context, id uuid.UUID, input UpdateInput) (User, error) {
	if id == uuid.Nil {
		return User{}, fmt.Errorf("%w: invalid user ID", ErrInvalidUser)
	}

	// Every rule enforced on Create applies here too; each field is validated
	// independently because a partial update only carries the fields it sets.
	if input.Name != nil {
		if err := validateName(*input.Name); err != nil {
			return User{}, err
		}
	}

	if input.Email != nil {
		normalized := NormalizeEmail(*input.Email)
		if err := validateEmail(normalized); err != nil {
			return User{}, err
		}
		input.Email = &normalized
	}

	if input.Password != nil {
		if err := validatePassword(*input.Password); err != nil {
			return User{}, err
		}

		hashBytes, err := bcrypt.GenerateFromPassword([]byte(*input.Password), bcrypt.DefaultCost)
		if err != nil {
			return User{}, fmt.Errorf("service.Update: hash new password: %w", err)
		}
		hashedStr := string(hashBytes)
		input.Password = &hashedStr
	}

	return s.repo.Update(ctx, id, input)
}

func (s *service) GetByID(ctx context.Context, id uuid.UUID) (User, error) {
	if id == uuid.Nil {
		return User{}, fmt.Errorf("%w: invalid user ID", ErrInvalidUser)
	}

	return s.repo.GetByID(ctx, id)
}

func (s *service) List(ctx context.Context, filter ListFilter) ([]User, error) {
	switch {
	case filter.Limit <= 0:
		filter.Limit = DefaultListLimit
	case filter.Limit > MaxListLimit:
		return nil, fmt.Errorf("%w: limit must be at most %d", ErrInvalidUser, MaxListLimit)
	}

	if filter.Offset < 0 {
		return nil, fmt.Errorf("%w: offset must not be negative", ErrInvalidUser)
	}

	return s.repo.List(ctx, filter)
}

func (s *service) UpdateRole(ctx context.Context, id uuid.UUID, role string) (User, error) {
	if id == uuid.Nil {
		return User{}, fmt.Errorf("%w: invalid user ID", ErrInvalidUser)
	}

	if err := validateRole(role); err != nil {
		return User{}, err
	}

	return s.repo.UpdateRole(ctx, id, role)
}

func (s *service) Delete(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: invalid user ID", ErrInvalidUser)
	}

	return s.repo.Delete(ctx, id)
}
