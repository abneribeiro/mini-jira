package project

import (
	"context"
	"fmt"

	"github.com/abneribeiro/internal/authz"
	"github.com/google/uuid"
)

type Service interface {
	Create(ctx context.Context, ownerID uuid.UUID, input CreateInput) (Project, error)
	GetByID(ctx context.Context, id uuid.UUID, scope authz.Scope) (Project, error)
	List(ctx context.Context, scope authz.Scope, filter ListFilter) ([]Project, error)
	Update(ctx context.Context, id uuid.UUID, scope authz.Scope, input UpdateInput) (Project, error)
	Delete(ctx context.Context, id uuid.UUID, scope authz.Scope) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Create(ctx context.Context, ownerID uuid.UUID, input CreateInput) (Project, error) {
	if ownerID == uuid.Nil {
		return Project{}, fmt.Errorf("%w: owner is required", ErrInvalidProject)
	}

	if err := validateProject(input.Name); err != nil {
		return Project{}, err
	}

	if err := validateDescription(input.Description); err != nil {
		return Project{}, err
	}

	newID, err := uuid.NewV7()
	if err != nil {
		return Project{}, fmt.Errorf("service.Create: generate uuid: %w", err)
	}

	return s.repo.Create(ctx, newID, ownerID, input)
}

func (s *service) GetByID(ctx context.Context, id uuid.UUID, scope authz.Scope) (Project, error) {
	if id == uuid.Nil {
		return Project{}, fmt.Errorf("%w: invalid project ID", ErrInvalidProject)
	}

	return s.repo.GetByID(ctx, id, scope)
}

func (s *service) List(ctx context.Context, scope authz.Scope, filter ListFilter) ([]Project, error) {
	switch {
	case filter.Limit <= 0:
		filter.Limit = DefaultListLimit
	case filter.Limit > MaxListLimit:
		return nil, fmt.Errorf("%w: limit must be at most %d", ErrInvalidProject, MaxListLimit)
	}

	if filter.Offset < 0 {
		return nil, fmt.Errorf("%w: offset must not be negative", ErrInvalidProject)
	}

	return s.repo.List(ctx, scope, filter)
}

func (s *service) Update(ctx context.Context, id uuid.UUID, scope authz.Scope, input UpdateInput) (Project, error) {
	if id == uuid.Nil {
		return Project{}, fmt.Errorf("%w: invalid project ID", ErrInvalidProject)
	}

	if input.Name != nil {
		if err := validateProject(*input.Name); err != nil {
			return Project{}, err
		}
	}

	if input.Description != nil {
		if err := validateDescription(*input.Description); err != nil {
			return Project{}, err
		}
	}

	return s.repo.Update(ctx, id, scope, input)
}

func (s *service) Delete(ctx context.Context, id uuid.UUID, scope authz.Scope) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: invalid project ID", ErrInvalidProject)
	}

	return s.repo.Delete(ctx, id, scope)
}
