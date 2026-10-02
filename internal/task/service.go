package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidTask é um erro de domínio genérico (como descrito no minijira.md)
var ErrInvalidTask = errors.New("invalid task data")

type Service interface {
	Create(ctx context.Context, input CreateInput) (Task, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) Create(ctx context.Context, input CreateInput) (Task, error) {
	// Regras de Negócio simples (Baseado no seu minijira.md)
	if strings.TrimSpace(input.Title) == "" {
		return Task{}, fmt.Errorf("%w: title is required", ErrInvalidTask)
	}

	if input.ProjectID <= 0 {
		return Task{}, fmt.Errorf("%w: project_id is required", ErrInvalidTask)
	}

	// Se estiver tudo ok, o Service manda o Repository gravar.
	return s.repo.Create(ctx, input)
}