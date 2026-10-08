package project

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidProject = errors.New("invalid project data")

type Project struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type UpdateInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func validateProject(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return fmt.Errorf("%w: name is required", ErrInvalidProject)
	case len([]rune(name)) > 100:
		return fmt.Errorf("%w: name must be at most 100 characters", ErrInvalidProject)
	}

	return nil
}
