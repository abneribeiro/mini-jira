package user

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/abneribeiro/internal/database"
	"github.com/abneribeiro/internal/metrics"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	Create(ctx context.Context, id uuid.UUID, input CreateInput) (User, error)
	GetByID(ctx context.Context, id uuid.UUID) (User, error)
	GetByEmail(ctx context.Context, email string) (User, error)
	List(ctx context.Context, filter ListFilter) ([]User, error)
	Update(ctx context.Context, id uuid.UUID, input UpdateInput) (User, error)
	UpdateRole(ctx context.Context, id uuid.UUID, role string) (User, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) Repository {
	return &repository{db: db}
}

// publicColumns omits password. withPassword is only for the login path.
const (
	publicColumns      = `id, name, email, role, created_at, updated_at`
	columnsWithSecrets = `id, name, email, role, password, created_at, updated_at`
)

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt, &u.UpdatedAt)

	return u, err
}

func scanUserWithPassword(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.Password, &u.CreatedAt, &u.UpdatedAt)

	return u, err
}

func (r *repository) Create(ctx context.Context, id uuid.UUID, input CreateInput) (User, error) {
	defer metrics.ObserveQuery("insert", "users")()
	const query = `
		INSERT INTO users (id, name, email, password)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + publicColumns

	u, err := scanUser(r.db.QueryRow(ctx, query, id, input.Name, input.Email, input.Password))
	if err != nil {
		if database.UniqueViolation(err, "users_email_key") {
			return User{}, fmt.Errorf("repository.Create: %w", ErrEmailAlreadyExists)
		}
		return User{}, fmt.Errorf("repository.Create: failed to insert user: %w", err)
	}

	return u, nil
}

func (r *repository) Update(ctx context.Context, id uuid.UUID, input UpdateInput) (User, error) {
	defer metrics.ObserveQuery("update", "users")()

	setClauses, args := buildUpdateSet(input)
	if len(setClauses) == 0 {
		return User{}, fmt.Errorf("repository.Update: %w", ErrNoFieldsToUpdate)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	idArg := len(args) + 1

	query := fmt.Sprintf(`
		UPDATE users
		SET %s
		WHERE id = $%d
		RETURNING `+publicColumns, strings.Join(setClauses, ", "), idArg)

	args = append(args, id)

	u, err := scanUser(r.db.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, fmt.Errorf("repository.Update: %w", ErrUserNotFound)
		}
		if database.UniqueViolation(err, "users_email_key") {
			return User{}, fmt.Errorf("repository.Update: %w", ErrEmailAlreadyExists)
		}
		return User{}, fmt.Errorf("repository.Update: failed to update user: %w", err)
	}

	return u, nil
}

// buildUpdateSet is pure so the placeholder arithmetic can be tested without
// a database.
func buildUpdateSet(input UpdateInput) (clauses []string, args []any) {
	argID := 1

	if input.Name != nil {
		clauses = append(clauses, fmt.Sprintf("name = $%d", argID))
		args = append(args, *input.Name)
		argID++
	}

	if input.Email != nil {
		clauses = append(clauses, fmt.Sprintf("email = $%d", argID))
		args = append(args, *input.Email)
		argID++
	}

	if input.Password != nil {
		clauses = append(clauses, fmt.Sprintf("password = $%d", argID))
		args = append(args, *input.Password)
		argID++
	}

	return clauses, args
}

func (r *repository) UpdateRole(ctx context.Context, id uuid.UUID, role string) (User, error) {
	defer metrics.ObserveQuery("update", "users")()

	const query = `
		UPDATE users
		SET role = $1, updated_at = NOW()
		WHERE id = $2
		RETURNING ` + publicColumns

	u, err := scanUser(r.db.QueryRow(ctx, query, role, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, fmt.Errorf("repository.UpdateRole: %w", ErrUserNotFound)
		}
		return User{}, fmt.Errorf("repository.UpdateRole: failed to update role: %w", err)
	}

	return u, nil
}

func (r *repository) GetByEmail(ctx context.Context, email string) (User, error) {
	defer metrics.ObserveQuery("select", "users")()

	const query = `
		SELECT ` + columnsWithSecrets + `
		FROM users
		WHERE email = $1
	`

	u, err := scanUserWithPassword(r.db.QueryRow(ctx, query, email))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, fmt.Errorf("repository.GetByEmail: %w", ErrUserNotFound)
		}
		return User{}, fmt.Errorf("repository.GetByEmail: failed to get user: %w", err)
	}

	return u, nil
}

func (r *repository) GetByID(ctx context.Context, id uuid.UUID) (User, error) {
	defer metrics.ObserveQuery("select", "users")()

	const query = `
		SELECT ` + publicColumns + `
		FROM users
		WHERE id = $1
	`

	u, err := scanUser(r.db.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, fmt.Errorf("repository.GetByID: %w", ErrUserNotFound)
		}
		return User{}, fmt.Errorf("repository.GetByID: failed to get user: %w", err)
	}

	return u, nil
}

func (r *repository) List(ctx context.Context, filter ListFilter) ([]User, error) {
	defer metrics.ObserveQuery("select", "users")()

	const query = `
		SELECT ` + publicColumns + `
		FROM users
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.Query(ctx, query, filter.Limit, filter.Offset)
	if err != nil {
		return nil, fmt.Errorf("repository.List: failed to list users: %w", err)
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
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
	defer metrics.ObserveQuery("delete", "users")()

	const query = `
		DELETE FROM users
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("repository.Delete: failed to delete user: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("repository.Delete: %w", ErrUserNotFound)
	}

	return nil
}
