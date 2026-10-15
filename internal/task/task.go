package task

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

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

type UpdateInput struct {
	Title       *string      `json:"title"`
	Description *string      `json:"description"`
	Status      *string      `json:"status"`
	AssigneeID  NullableUUID `json:"assignee_id"`
}
