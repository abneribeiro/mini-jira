package user

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	Create(ctx context.Context, input CreateInput) (User, error)
	Update(ctx context.Context, id int64, input UpdateInput) (User, error)
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

func (r *repository) Update(ctx context.Context, id int64, input UpdateInput) (User, error) {
	var setClauses []string
	var args []interface{}
	argID := 1 

	if input.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argID))
		args = append(args, *input.Name)
		argID++
	}

	if input.Email != nil {
		setClauses = append(setClauses, fmt.Sprintf("email = $%d", argID))
		args = append(args,  *input.Email)
		argID++
	}

	if input.Password != nil {
		setClauses = append(setClauses, fmt.Sprintf("password = $%d", argID))
		args = append(args, *input.Password)
		argID++
	}

	if len(setClauses) == 0 {
		return User{}, fmt.Errorf("repository.Update: no fields to update")
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`
		UPDATE users
		SET %s
		WHERE id = $%d
		RETURNING id, name, email, password, created_at, updated_at
	`, strings.Join(setClauses, ", "), argID)

	args = append(args, id)

	var u User
	err := r.db.QueryRow(ctx, query, args...).Scan(
		&u.ID,
		&u.Name,
		&u.Email,
		&u.Password,
		&u.CreatedAt,
		&u.UpdatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return User{}, fmt.Errorf("repository.Update: user with id %d not found", id)
		}
		return User{}, fmt.Errorf("repository.Update: failed to update user: %w", err)
	}

	return u, nil
}