package user

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	Create(ctx context.Context, input CreateInput) (User, error)
}

type repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) Repository {
	return &repository{db: db}
}

func (r *repository) Create(ctx context.Context, input CreateInput) (User, error) {
	const query = `
		INSERT INTO users (name, email, password) 
		VALUES ($1, $2, $3) 
		RETURNING id, name, email, created_at, updated_at
	`

	var u User
	// QueryRow é usado para retornar apenas 1 linha
	err := r.db.QueryRow(ctx, query,
		input.Name,
		input.Email,
		input.Password,
	).Scan(
		&u.ID,
		&u.Name,
		&u.Email,
		&u.CreatedAt,
		&u.UpdatedAt,
	)

	if err != nil {
		// Retornamos um objecto Task vazio e o erro formatado
		return User{}, fmt.Errorf("repository.Create: failed to insert user: %w", err)
	}

	return u, nil
}