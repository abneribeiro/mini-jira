package project

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/abneribeiro/internal/authz"
	"github.com/abneribeiro/internal/metrics"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Reads and writes are scoped by authz.Scope: a member only reaches rows
// they own, an admin reaches every row. A project belonging to someone else
// is reported as not found rather than forbidden, so the API cannot be used
// to probe which project IDs exist.
type Repository interface {
	Create(ctx context.Context, id, ownerID uuid.UUID, input CreateInput) (Project, error)
	GetByID(ctx context.Context, id uuid.UUID, scope authz.Scope) (Project, error)
	List(ctx context.Context, scope authz.Scope, filter ListFilter) ([]Project, error)
	Update(ctx context.Context, id uuid.UUID, scope authz.Scope, input UpdateInput) (Project, error)
	Delete(ctx context.Context, id uuid.UUID, scope authz.Scope) error
}

type repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) Repository {
	return &repository{db: db}
}

const projectColumns = `id, owner_id, name, description, created_at, updated_at`

func scanProject(row pgx.Row) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)

	return p, err
}

func (r *repository) Create(ctx context.Context, id, ownerID uuid.UUID, input CreateInput) (Project, error) {
	defer metrics.ObserveQuery("insert", "projects")()

	const query = `
		INSERT INTO projects (id, owner_id, name, description)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + projectColumns

	p, err := scanProject(r.db.QueryRow(ctx, query, id, ownerID, input.Name, input.Description))
	if err != nil {
		return Project{}, fmt.Errorf("repository.Create: failed to insert project: %w", err)
	}

	return p, nil
}

func (r *repository) GetByID(ctx context.Context, id uuid.UUID, scope authz.Scope) (Project, error) {
	defer metrics.ObserveQuery("select", "projects")()

	const query = `
		SELECT ` + projectColumns + `
		FROM projects
		WHERE id = $1 AND (owner_id = $2 OR $3)
	`

	p, err := scanProject(r.db.QueryRow(ctx, query, id, scope.UserID, scope.IsAdmin))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, fmt.Errorf("repository.GetByID: %w", ErrProjectNotFound)
		}
		return Project{}, fmt.Errorf("repository.GetByID: failed to get project: %w", err)
	}

	return p, nil
}

func (r *repository) List(ctx context.Context, scope authz.Scope, filter ListFilter) ([]Project, error) {
	defer metrics.ObserveQuery("select", "projects")()

	const query = `
		SELECT ` + projectColumns + `
		FROM projects
		WHERE owner_id = $1 OR $2
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`

	rows, err := r.db.Query(ctx, query, scope.UserID, scope.IsAdmin, filter.Limit, filter.Offset)
	if err != nil {
		return nil, fmt.Errorf("repository.List: failed to list projects: %w", err)
	}
	defer rows.Close()

	projects := make([]Project, 0)
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("repository.List: failed to scan project: %w", err)
		}
		projects = append(projects, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository.List: error iterating projects: %w", err)
	}

	return projects, nil
}

func (r *repository) Update(ctx context.Context, id uuid.UUID, scope authz.Scope, input UpdateInput) (Project, error) {
	defer metrics.ObserveQuery("update", "projects")()

	var setClauses []string
	var args []any
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
		return Project{}, fmt.Errorf("repository.Update: %w", ErrNoFieldsToUpdate)
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`
		UPDATE projects
		SET %s
		WHERE id = $%d AND (owner_id = $%d OR $%d)
		RETURNING `+projectColumns, strings.Join(setClauses, ", "), argID, argID+1, argID+2)

	args = append(args, id, scope.UserID, scope.IsAdmin)

	p, err := scanProject(r.db.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, fmt.Errorf("repository.Update: %w", ErrProjectNotFound)
		}
		return Project{}, fmt.Errorf("repository.Update: failed to update project: %w", err)
	}

	return p, nil
}

func (r *repository) Delete(ctx context.Context, id uuid.UUID, scope authz.Scope) error {
	defer metrics.ObserveQuery("delete", "projects")()

	const query = `
		DELETE FROM projects
		WHERE id = $1 AND (owner_id = $2 OR $3)
	`

	result, err := r.db.Exec(ctx, query, id, scope.UserID, scope.IsAdmin)
	if err != nil {
		return fmt.Errorf("repository.Delete: failed to delete project: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("repository.Delete: %w", ErrProjectNotFound)
	}

	return nil
}
