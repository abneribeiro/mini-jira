package project

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidProject   = errors.New("invalid project data")
	ErrProjectNotFound  = errors.New("project not found")
	ErrNoFieldsToUpdate = errors.New("no fields to update")
)

const (
	maxNameLen        = 100
	maxDescriptionLen = 2000

	DefaultListLimit = 50
	MaxListLimit     = 200
)

type Project struct {
	ID          uuid.UUID `json:"id"`
	OwnerID     uuid.UUID `json:"owner_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListFilter paginates GET /projects.
type ListFilter struct {
	Limit  int
	Offset int
}

type UpdateInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func validateProject(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return fmt.Errorf("%w: name is required", ErrInvalidProject)
	case len([]rune(name)) > maxNameLen:
		return fmt.Errorf("%w: name must be at most %d characters", ErrInvalidProject, maxNameLen)
	}

	return nil
}

func validateDescription(description string) error {
	if len([]rune(description)) > maxDescriptionLen {
		return fmt.Errorf("%w: description must be at most %d characters", ErrInvalidProject, maxDescriptionLen)
	}

	return nil
}
