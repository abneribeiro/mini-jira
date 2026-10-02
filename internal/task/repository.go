package task

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	Create(ctx context.Context, input CreateInput) (Task, error)
}

type repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) Repository {
	return &repository{db: db}
}

func (r *repository) Create(ctx context.Context, input CreateInput) (Task, error) {
	// A query SQL está isolada aqui. Usamos RETURNING para obter o ID e as datas geradas.
	const query = `
		INSERT INTO tasks (title, description, status, project_id, assignee_id) 
		VALUES ($1, $2, 'TODO', $3, $4) 
		RETURNING id, title, description, status, project_id, assignee_id, created_at, updated_at
	`

	var t Task
	// QueryRow é usado para retornar apenas 1 linha
	err := r.db.QueryRow(ctx, query,
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
		// Retornamos um objecto Task vazio e o erro formatado
		return Task{}, fmt.Errorf("repository.Create: failed to insert task: %w", err)
	}

	return t, nil
}