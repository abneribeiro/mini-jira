package user

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	Create(ctx context.Context, id uuid.UUID, input CreateInput) (User, error)
	GetByID(ctx context.Context, id uuid.UUID) (User, error)
	GetByEmail(ctx context.Context, email string) (User, error)
	List(ctx context.Context) ([]User, error)
	Update(ctx context.Context, id uuid.UUID, input UpdateInput) (User, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) Repository {
	return &repository{db: db}
}

func (r *repository) Create(ctx context.Context, id uuid.UUID, input CreateInput) (User, error) {
	const query = `
		INSERT INTO users (id, name, email, password)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, email, created_at, updated_at
	`

	var u User
	err := r.db.QueryRow(ctx, query,
		id,
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
		return User{}, fmt.Errorf("repository.Create: failed to insert user: %w", err)
	}

	return u, nil
}

func (r *repository) Update(ctx context.Context, id uuid.UUID, input UpdateInput) (User, error) {
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
		args = append(args, *input.Email)
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
			return User{}, fmt.Errorf("repository.Update: user with id %s not found", id)
		}
		return User{}, fmt.Errorf("repository.Update: failed to update user: %w", err)
	}

	return u, nil
}

func (r *repository) GetByEmail(ctx context.Context, email string) (User, error) {
	const query = `
		SELECT id, name, email, password, created_at, updated_at
		FROM users
		WHERE email = $1
	`

	var u User
	err := r.db.QueryRow(ctx, query, email).Scan(
		&u.ID,
		&u.Name,
		&u.Email,
		&u.Password,
		&u.CreatedAt,
		&u.UpdatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return User{}, fmt.Errorf("repository.GetByEmail: user with email %s not found", email)
		}
		return User{}, fmt.Errorf("repository.GetByEmail: failed to get user: %w", err)
	}

	return u, nil
}

func (r *repository) GetByID(ctx context.Context, id uuid.UUID) (User, error) {
	const query = `
		SELECT id, name, email, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	var u User
	err := r.db.QueryRow(ctx, query, id).Scan(
		&u.ID,
		&u.Name,
		&u.Email,
		&u.CreatedAt,
		&u.UpdatedAt,
	)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return User{}, fmt.Errorf("repository.GetByID: user with id %s not found", id)
		}
		return User{}, fmt.Errorf("repository.GetByID: failed to get user: %w", err)
	}

	return u, nil
}

func (r *repository) List(ctx context.Context) ([]User, error) {
	const query = `
		SELECT id, name, email, created_at, updated_at
		FROM users
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("repository.List: failed to list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(
			&u.ID,
			&u.Name,
			&u.Email,
			&u.CreatedAt,
			&u.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("repository.List: failed to scan user: %w", err)
		}
		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository.List: error iterating users: %w", err)
	}

	return users, nil
}

func (r *repository) Delete(ctx context.Context, id uuid.UUID) error {
	const query = `
		DELETE FROM users
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("repository.Delete: failed to delete user: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("repository.Delete: user with id %s not found", id)
	}

	return nil
}
