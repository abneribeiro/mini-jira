package task

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	Create(ctx context.Context, id uuid.UUID, input CreateInput) (Task, error)
	GetByID(ctx context.Context, id uuid.UUID) (Task, error)
	List(ctx context.Context) ([]Task, error)
	Update(ctx context.Context, id uuid.UUID, input UpdateInput) (Task, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) Repository {
	return &repository{db: db}
}

func (r *repository) Create(ctx context.Context, id uuid.UUID, input CreateInput) (Task, error) {
	const query = `
		INSERT INTO tasks (id, title, description, status, project_id, assignee_id)
		VALUES ($1, $2, $3, 'TODO',$4, $5)
		RETURNING id, title, description, status, project_id, assignee_id, created_at, updated_at
	`

	var t Task
	err := r.db.QueryRow(ctx, query,
		id,
		input.Title,
		input.Description,
		input.ProjectID,
		input.AssigneeID,
	).Scan(
		&t.ID,
		&t.Title,
		&t.Description,
		&t.Status,
		&t.ProjectID,
		&t.AssigneeID,
		&t.CreatedAt,
		&t.UpdatedAt,
	)

	if err != nil {
		return Task{}, fmt.Errorf("repository.Create: failed to insert task: %w", err)
	}

	return t, nil
}

func (r *repository) GetByID(ctx context.Context, id uuid.UUID) (Task, error) {
	const query = `
		SELECT id, title, description, status, project_id, assignee_id, created_at, updated_at
		FROM tasks
		WHERE id = $1
	`

	var t Task
	err := r.db.QueryRow(ctx, query, id).Scan(
		&t.ID,
		&t.Title,
		&t.Description,
		&t.Status,
		&t.ProjectID,
		&t.AssigneeID,
		&t.CreatedAt,
		&t.UpdatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return Task{}, fmt.Errorf("repository.GetByID: task with id %s not found", id)
		}
		return Task{}, fmt.Errorf("repository.GetByID: failed to get task: %w", err)
	}

	return t, nil
}

func (r *repository) List(ctx context.Context) ([]Task, error) {
	const query = `
		SELECT id, title, description, status, project_id, assignee_id, created_at, updated_at
		FROM tasks
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("repository.List: failed to list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(
			&t.ID,
			&t.Title,
			&t.Description,
			&t.Status,
			&t.ProjectID,
			&t.AssigneeID,
			&t.CreatedAt,
			&t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("repository.List: failed to scan task: %w", err)
		}
		tasks = append(tasks, t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository.List: error iterating tasks: %w", err)
	}

	return tasks, nil
}

func (r *repository) Update(ctx context.Context, id uuid.UUID, input UpdateInput) (Task, error) {
	var setClauses []string
	var args []interface{}
	argID := 1

	if input.Title != nil {
		setClauses = append(setClauses, fmt.Sprintf("title = $%d", argID))
		args = append(args, *input.Title)
		argID++
	}

	if input.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argID))
		args = append(args, *input.Description)
		argID++
	}

	if input.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argID))
		args = append(args, *input.Status)
		argID++
	}

	if input.AssigneeID != nil {
		setClauses = append(setClauses, fmt.Sprintf("assignee_id = $%d", argID))
		args = append(args, *input.AssigneeID)
		argID++
	}

	if len(setClauses) == 0 {
		return Task{}, fmt.Errorf("repository.Update: no fields to update")
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`
		UPDATE tasks
		SET %s
		WHERE id = $%d
		RETURNING id, title, description, status, project_id, assignee_id, created_at, updated_at
	`, strings.Join(setClauses, ", "), argID)

	args = append(args, id)

	var t Task
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&t.ID,
		&t.Title,
		&t.Description,
		&t.Status,
		&t.ProjectID,
		&t.AssigneeID,
		&t.CreatedAt,
		&t.UpdatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return Task{}, fmt.Errorf("repository.Update: task with id %s not found", id)
		}
		return Task{}, fmt.Errorf("repository.Update: failed to update task: %w", err)
	}

	return t, nil
}

func (r *repository) Delete(ctx context.Context, id uuid.UUID) error {
	const query = `
		DELETE FROM tasks
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("repository.Delete: failed to delete task: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("repository.Delete: task with id %s not found", id)
	}

	return nil
}
