package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var ErrInvalidTask = errors.New("invalid task data")

type Service interface {
	Create(ctx context.Context, input CreateInput) (Task, error)
	GetByID(ctx context.Context, id uuid.UUID) (Task, error)
	List(ctx context.Context) ([]Task, error)
	Update(ctx context.Context, id uuid.UUID, input UpdateInput) (Task, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Create(ctx context.Context, input CreateInput) (Task, error) {

	if strings.TrimSpace(input.Title) == "" {
		return Task{}, fmt.Errorf("%w: title is required", ErrInvalidTask)
	}

	if input.ProjectID == uuid.Nil {
		return Task{}, fmt.Errorf("%w: project_id is required", ErrInvalidTask)
	}

	newID, err := uuid.NewV7()

	if err != nil {
		return Task{}, fmt.Errorf("failed to generate uuid: %w", err)
	}

	return s.repo.Create(ctx, newID, input)
}

func (s *service) GetByID(ctx context.Context, id uuid.UUID) (Task, error) {
	if id == uuid.Nil {
		return Task{}, fmt.Errorf("%w: invalid task ID", ErrInvalidTask)
	}

	return s.repo.GetByID(ctx, id)
}

func (s *service) List(ctx context.Context) ([]Task, error) {
	return s.repo.List(ctx)
}

func (s *service) Update(ctx context.Context, id uuid.UUID, input UpdateInput) (Task, error) {
	if id == uuid.Nil {
		return Task{}, fmt.Errorf("%w: invalid task ID", ErrInvalidTask)
	}

	if input.Status != nil {
		validStatuses := map[string]bool{
			"TODO":        true,
			"IN_PROGRESS": true,
			"DONE":        true,
		}
		if !validStatuses[*input.Status] {
			return Task{}, fmt.Errorf("%w: invalid status. Must be TODO, IN_PROGRESS, or DONE", ErrInvalidTask)
		}
	}

	if input.Title != nil && strings.TrimSpace(*input.Title) == "" {
		return Task{}, fmt.Errorf("%w: title cannot be empty", ErrInvalidTask)
	}

	return s.repo.Update(ctx, id, input)
}

func (s *service) Delete(ctx context.Context, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: invalid task ID", ErrInvalidTask)
	}

	return s.repo.Delete(ctx, id)
}
