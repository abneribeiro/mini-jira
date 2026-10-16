package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidTask      = errors.New("invalid task data")
	ErrTaskNotFound     = errors.New("task not found")
	ErrProjectNotFound  = errors.New("project not found")
	ErrAssigneeNotFound = errors.New("assignee not found")
	ErrNoFieldsToUpdate = errors.New("no fields to update")
)

const (
	maxTitleLen       = 200
	maxDescriptionLen = 2000

	DefaultListLimit = 50
	MaxListLimit     = 200
)

// validStatuses mirrors the tasks_status_check constraint.
var validStatuses = map[string]bool{
	"TODO":        true,
	"IN_PROGRESS": true,
	"DONE":        true,
}

// NullableUUID distinguishes "field absent from the JSON body" from an
// explicit "field": null. A plain *uuid.UUID collapses both to nil, which
// makes clearing a nullable column impossible through a PATCH-style update.
type NullableUUID struct {
	Set   bool
	Value *uuid.UUID
}

func (n *NullableUUID) UnmarshalJSON(b []byte) error {
	n.Set = true

	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		n.Value = nil
		return nil
	}

	var id uuid.UUID
	if err := json.Unmarshal(b, &id); err != nil {
		return fmt.Errorf("assignee_id: %w", err)
	}

	n.Value = &id
	return nil
}

func (n NullableUUID) MarshalJSON() ([]byte, error) {
	return json.Marshal(n.Value)
}

type Task struct {
	ID          uuid.UUID  `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	ProjectID   uuid.UUID  `json:"project_id"`
	AssigneeID  *uuid.UUID `json:"assignee_id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CreateInput struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	ProjectID   uuid.UUID  `json:"project_id"`
	AssigneeID  *uuid.UUID `json:"assignee_id"`
}

// ListFilter narrows GET /tasks. A nil pointer means "no filter on this
// field"; Limit/Offset are always applied.
type ListFilter struct {
	ProjectID  *uuid.UUID
	Status     *string
	AssigneeID *uuid.UUID
	Limit      int
	Offset     int
}

type UpdateInput struct {
	Title       *string      `json:"title"`
	Description *string      `json:"description"`
	Status      *string      `json:"status"`
	AssigneeID  NullableUUID `json:"assignee_id"`
	// ProjectID moves a task to another project. The target must also be
	// reachable by the caller, or a member could push a task into someone
	// else's project.
	ProjectID *uuid.UUID `json:"project_id"`
}

func validateTitle(title string) error {
	switch {
	case strings.TrimSpace(title) == "":
		return fmt.Errorf("%w: title is required", ErrInvalidTask)
	case len([]rune(title)) > maxTitleLen:
		return fmt.Errorf("%w: title must be at most %d characters", ErrInvalidTask, maxTitleLen)
	}

	return nil
}

func validateDescription(description string) error {
	if len([]rune(description)) > maxDescriptionLen {
		return fmt.Errorf("%w: description must be at most %d characters", ErrInvalidTask, maxDescriptionLen)
	}

	return nil
}

func validateStatus(status string) error {
	if !validStatuses[status] {
		return fmt.Errorf("%w: invalid status. Must be TODO, IN_PROGRESS, or DONE", ErrInvalidTask)
	}

	return nil
}
