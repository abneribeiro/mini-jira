package task

import (
	"context"
	"fmt"

	"github.com/abneribeiro/internal/authz"
	"github.com/google/uuid"
)

type Service interface {
	Create(ctx context.Context, scope authz.Scope, input CreateInput) (Task, error)
	GetByID(ctx context.Context, id uuid.UUID, scope authz.Scope) (Task, error)
	List(ctx context.Context, scope authz.Scope, filter ListFilter) ([]Task, error)
	Update(ctx context.Context, id uuid.UUID, scope authz.Scope, input UpdateInput) (Task, error)
	Delete(ctx context.Context, id uuid.UUID, scope authz.Scope) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Create(ctx context.Context, scope authz.Scope, input CreateInput) (Task, error) {
	if scope.UserID == uuid.Nil {
		return Task{}, fmt.Errorf("%w: owner is required", ErrInvalidTask)
	}

	if err := validateTitle(input.Title); err != nil {
		return Task{}, err
	}

	if err := validateDescription(input.Description); err != nil {
		return Task{}, err
	}

	if input.ProjectID == uuid.Nil {
		return Task{}, fmt.Errorf("%w: project_id is required", ErrInvalidTask)
	}

	// An all-zero assignee would reach the FK check and come back as a
	// confusing 422; reject it as the client error it is.
	if input.AssigneeID != nil && *input.AssigneeID == uuid.Nil {
		return Task{}, fmt.Errorf("%w: assignee_id must be a valid UUID", ErrInvalidTask)
	}

	newID, err := uuid.NewV7()
	if err != nil {
		return Task{}, fmt.Errorf("service.Create: generate uuid: %w", err)
	}

	return s.repo.Create(ctx, newID, scope, input)
}

func (s *service) GetByID(ctx context.Context, id uuid.UUID, scope authz.Scope) (Task, error) {
	if id == uuid.Nil {
		return Task{}, fmt.Errorf("%w: invalid task ID", ErrInvalidTask)
	}

	return s.repo.GetByID(ctx, id, scope)
}

func (s *service) List(ctx context.Context, scope authz.Scope, filter ListFilter) ([]Task, error) {
	if filter.Status != nil {
		if err := validateStatus(*filter.Status); err != nil {
			return nil, err
		}
	}

	switch {
	case filter.Limit <= 0:
		filter.Limit = DefaultListLimit
	case filter.Limit > MaxListLimit:
		return nil, fmt.Errorf("%w: limit must be at most %d", ErrInvalidTask, MaxListLimit)
	}

	if filter.Offset < 0 {
		return nil, fmt.Errorf("%w: offset must not be negative", ErrInvalidTask)
	}

	return s.repo.List(ctx, scope, filter)
}

func (s *service) Update(ctx context.Context, id uuid.UUID, scope authz.Scope, input UpdateInput) (Task, error) {
	if id == uuid.Nil {
		return Task{}, fmt.Errorf("%w: invalid task ID", ErrInvalidTask)
	}

	if input.Status != nil {
		if err := validateStatus(*input.Status); err != nil {
			return Task{}, err
		}
	}

	if input.Title != nil {
		if err := validateTitle(*input.Title); err != nil {
			return Task{}, err
		}
	}

	if input.Description != nil {
		if err := validateDescription(*input.Description); err != nil {
			return Task{}, err
		}
	}

	if input.AssigneeID.Set && input.AssigneeID.Value != nil && *input.AssigneeID.Value == uuid.Nil {
		return Task{}, fmt.Errorf("%w: assignee_id must be a valid UUID", ErrInvalidTask)
	}

	if input.ProjectID != nil && *input.ProjectID == uuid.Nil {
		return Task{}, fmt.Errorf("%w: project_id must be a valid UUID", ErrInvalidTask)
	}

	return s.repo.Update(ctx, id, scope, input)
}

func (s *service) Delete(ctx context.Context, id uuid.UUID, scope authz.Scope) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: invalid task ID", ErrInvalidTask)
	}

	return s.repo.Delete(ctx, id, scope)
}
