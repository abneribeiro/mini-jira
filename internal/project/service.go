package project

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type Service interface {
	Create(ctx context.Context, input CreateInput) (Project, error)
	GetByID(ctx context.Context, id uuid.UUID) (Project, error)
	List(ctx context.Context) ([]Project, error)
	Update(ctx context.Context, id uuid.UUID, input UpdateInput) (Project, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Create(ctx context.Context, input CreateInput) (Project, error) {
	if err := validateProject(input.Name); err != nil {
		return Project{}, err
	}
	newID, err := uuid.NewV7()

	if err != nil {
		return Project{}, fmt.Errorf("failed to generate uuid: %w", err)
	}
	return s.repo.Create(ctx, newID, input)
}

func (s *service) GetByID(ctx context.Context, id uuid.UUID) (Project, error) {
	if id == uuid.Nil {
		return Project{}, fmt.Errorf("%w: invalid project ID", ErrInvalidProject)
	}

	return s.repo.GetByID(ctx, id)
}

func (s *service) List(ctx context.Context) ([]Project, error) {
	return s.repo.List(ctx)
}

func (s *service) Update(ctx context.Context, id uuid.UUID, input UpdateInput) (Project, error) {
	if id == uuid.Nil {
		return Project{}, fmt.Errorf("%w: invalid project ID", ErrInvalidProject)
	}

	if input.Name != nil {
		if err := validateProject(*input.Name); err != nil {
			return Project{}, err
		}
	}

	return s.repo.Update(ctx, id, input)
}

func (s *service) Delete(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: invalid project ID", ErrInvalidProject)
	}

	return s.repo.Delete(ctx, id)
}
