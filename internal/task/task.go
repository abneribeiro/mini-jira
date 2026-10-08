package task

import (
	"time"

	"github.com/google/uuid"
)

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

type UpdateInput struct {
	Title       *string     `json:"title"`
	Description *string     `json:"description"`
	Status      *string     `json:"status"`
	AssigneeID  *uuid.UUID `json:"assignee_id"`
}
