package project

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	Create(ctx context.Context, id uuid.UUID, input CreateInput) (Project, error)
	GetByID(ctx context.Context, id uuid.UUID) (Project, error)
	List(ctx context.Context) ([]Project, error)
	Update(ctx context.Context, id uuid.UUID, input UpdateInput) (Project, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) Repository {
	return &repository{db: db}
}

func (r *repository) Create(ctx context.Context, id uuid.UUID, input CreateInput) (Project, error) {
	const query = `
		INSERT INTO projects (id, name, description)
		VALUES ($1, $2, $3)
		RETURNING id, name, description, created_at, updated_at
	`

	var p Project
	err := r.db.QueryRow(ctx, query,
		id,
		input.Name,
		input.Description,
	).Scan(
		&p.ID,
		&p.Name,
		&p.Description,
		&p.CreatedAt,
		&p.UpdatedAt,
	)

	if err != nil {
		return Project{}, fmt.Errorf("repository.Create: failed to insert project: %w", err)
	}

	return p, nil
}

func (r *repository) GetByID(ctx context.Context, id uuid.UUID) (Project, error) {
	const query = `
		SELECT id, name, description, created_at, updated_at
		FROM projects
		WHERE id = $1
	`

	var p Project
	err := r.db.QueryRow(ctx, query, id).Scan(
		&p.ID,
		&p.Name,
		&p.Description,
		&p.CreatedAt,
		&p.UpdatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return Project{}, fmt.Errorf("repository.GetByID: project with id %s not found", id)
		}
		return Project{}, fmt.Errorf("repository.GetByID: failed to get project: %w", err)
	}

	return p, nil
}

func (r *repository) List(ctx context.Context) ([]Project, error) {
	const query = `
		SELECT id, name, description, created_at, updated_at
		FROM projects
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("repository.List: failed to list projects: %w", err)
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(
			&p.ID,
			&p.Name,
			&p.Description,
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("repository.List: failed to scan project: %w", err)
		}
		projects = append(projects, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository.List: error iterating projects: %w", err)
	}

	return projects, nil
}

func (r *repository) Update(ctx context.Context, id uuid.UUID, input UpdateInput) (Project, error) {
	var setClauses []string
	var args []interface{}
	argID := 1

	if input.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argID))
		args = append(args, *input.Name)
		argID++
	}

	if input.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argID))
		args = append(args, *input.Description)
		argID++
	}

	if len(setClauses) == 0 {
		return Project{}, fmt.Errorf("repository.Update: no fields to update")
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`
		UPDATE projects
		SET %s
		WHERE id = $%d
		RETURNING id, name, description, created_at, updated_at
	`, strings.Join(setClauses, ", "), argID)

	args = append(args, id)

	var p Project
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&p.ID,
		&p.Name,
		&p.Description,
		&p.CreatedAt,
		&p.UpdatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return Project{}, fmt.Errorf("repository.Update: project with id %s not found", id)
		}
		return Project{}, fmt.Errorf("repository.Update: failed to update project: %w", err)
	}

	return p, nil
}

func (r *repository) Delete(ctx context.Context, id uuid.UUID) error {
	const query = `
		DELETE FROM projects
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("repository.Delete: failed to delete project: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("repository.Delete: project with id %s not found", id)
	}

	return nil
}
