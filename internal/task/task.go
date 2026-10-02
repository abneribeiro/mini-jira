package task

import (
	"time"
)

type Task struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	ProjectID   int64     `json:"project_id"`  
	AssigneeID  *int64    `json:"assignee_id"`  
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	ProjectID   int64  `json:"project_id"`
	AssigneeID  *int64 `json:"assignee_id"`
}

type UpdateInput struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
	AssigneeID  *int64  `json:"assignee_id"`
}
