package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/abneribeiro/internal/authz"
	"github.com/abneribeiro/internal/database"
	"github.com/abneribeiro/internal/metrics"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Tasks have no owner column; they inherit ownership from their project.
// Every method therefore joins or sub-selects through projects.owner_id,
// widened to every row when the caller is an admin.
type Repository interface {
	Create(ctx context.Context, id uuid.UUID, scope authz.Scope, input CreateInput) (Task, error)
	GetByID(ctx context.Context, id uuid.UUID, scope authz.Scope) (Task, error)
	List(ctx context.Context, scope authz.Scope, filter ListFilter) ([]Task, error)
	Update(ctx context.Context, id uuid.UUID, scope authz.Scope, input UpdateInput) (Task, error)
	Delete(ctx context.Context, id uuid.UUID, scope authz.Scope) error
}

type repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) Repository {
	return &repository{db: db}
}

const taskColumns = `id, title, description, status, project_id, assignee_id, created_at, updated_at`

func scanTask(row pgx.Row) (Task, error) {
	var t Task
	err := row.Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.ProjectID, &t.AssigneeID, &t.CreatedAt, &t.UpdatedAt)

	return t, err
}

func (r *repository) Create(ctx context.Context, id uuid.UUID, scope authz.Scope, input CreateInput) (Task, error) {
	defer metrics.ObserveQuery("insert", "tasks")()

	// INSERT ... SELECT ... WHERE EXISTS checks project ownership inside the
	// same statement, so there is no read-then-write race.
	const query = `
		INSERT INTO tasks (id, title, description, status, project_id, assignee_id)
		SELECT $1, $2, $3, 'TODO', $4, $5
		WHERE EXISTS (
			SELECT 1 FROM projects WHERE id = $4 AND (owner_id = $6 OR $7)
		)
		RETURNING ` + taskColumns

	t, err := scanTask(r.db.QueryRow(ctx, query, id, input.Title, input.Description, input.ProjectID, input.AssigneeID, scope.UserID, scope.IsAdmin))
	if err != nil {
		// No row inserted means the project does not exist or is not ours;
		// both answer "project not found" so ownership cannot be probed.
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, fmt.Errorf("repository.Create: %w", ErrProjectNotFound)
		}
		if database.ForeignKeyViolation(err, "tasks_assignee_id_fkey") {
			return Task{}, fmt.Errorf("repository.Create: %w", ErrAssigneeNotFound)
		}
		// The project passed the EXISTS check but was deleted before the
		// insert committed.
		if database.ForeignKeyViolation(err, "tasks_project_id_fkey") {
			return Task{}, fmt.Errorf("repository.Create: %w", ErrProjectNotFound)
		}
		return Task{}, fmt.Errorf("repository.Create: failed to insert task: %w", err)
	}

	return t, nil
}

func (r *repository) GetByID(ctx context.Context, id uuid.UUID, scope authz.Scope) (Task, error) {
	defer metrics.ObserveQuery("select", "tasks")()

	const query = `
		SELECT t.id, t.title, t.description, t.status, t.project_id, t.assignee_id, t.created_at, t.updated_at
		FROM tasks t
		JOIN projects p ON p.id = t.project_id
		WHERE t.id = $1 AND (p.owner_id = $2 OR $3)
	`

	t, err := scanTask(r.db.QueryRow(ctx, query, id, scope.UserID, scope.IsAdmin))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, fmt.Errorf("repository.GetByID: %w", ErrTaskNotFound)
		}
		return Task{}, fmt.Errorf("repository.GetByID: failed to get task: %w", err)
	}

	return t, nil
}

func (r *repository) List(ctx context.Context, scope authz.Scope, filter ListFilter) ([]Task, error) {
	defer metrics.ObserveQuery("select", "tasks")()

	conditions := []string{"(p.owner_id = $1 OR $2)"}
	args := []any{scope.UserID, scope.IsAdmin}
	argID := 3

	if filter.ProjectID != nil {
		conditions = append(conditions, fmt.Sprintf("t.project_id = $%d", argID))
		args = append(args, *filter.ProjectID)
		argID++
	}

	if filter.Status != nil {
		conditions = append(conditions, fmt.Sprintf("t.status = $%d", argID))
		args = append(args, *filter.Status)
		argID++
	}

	if filter.AssigneeID != nil {
		conditions = append(conditions, fmt.Sprintf("t.assignee_id = $%d", argID))
		args = append(args, *filter.AssigneeID)
		argID++
	}

	query := fmt.Sprintf(`
		SELECT t.id, t.title, t.description, t.status, t.project_id, t.assignee_id, t.created_at, t.updated_at
		FROM tasks t
		JOIN projects p ON p.id = t.project_id
		WHERE %s
		ORDER BY t.created_at DESC
		LIMIT $%d OFFSET $%d
	`, strings.Join(conditions, " AND "), argID, argID+1)

	args = append(args, filter.Limit, filter.Offset)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("repository.List: failed to list tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("repository.List: failed to scan task: %w", err)
		}
		tasks = append(tasks, t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository.List: error iterating tasks: %w", err)
	}

	return tasks, nil
}

// buildUpdateSet turns a partial update into SET clauses and their
// arguments. It is a pure function so the placeholder arithmetic and the
// assignee tri-state can be tested without a database.
func buildUpdateSet(input UpdateInput) (clauses []string, args []any) {
	argID := 1

	if input.Title != nil {
		clauses = append(clauses, fmt.Sprintf("title = $%d", argID))
		args = append(args, *input.Title)
		argID++
	}

	if input.Description != nil {
		clauses = append(clauses, fmt.Sprintf("description = $%d", argID))
		args = append(args, *input.Description)
		argID++
	}

	if input.Status != nil {
		clauses = append(clauses, fmt.Sprintf("status = $%d", argID))
		args = append(args, *input.Status)
		argID++
	}

	// .Set is true for both an explicit UUID and an explicit null, so
	// "assignee_id": null clears the column instead of being ignored.
	if input.AssigneeID.Set {
		clauses = append(clauses, fmt.Sprintf("assignee_id = $%d", argID))
		args = append(args, input.AssigneeID.Value)
		argID++
	}

	if input.ProjectID != nil {
		clauses = append(clauses, fmt.Sprintf("project_id = $%d", argID))
		args = append(args, *input.ProjectID)
		argID++
	}

	return clauses, args
}

func (r *repository) Update(ctx context.Context, id uuid.UUID, scope authz.Scope, input UpdateInput) (Task, error) {
	defer metrics.ObserveQuery("update", "tasks")()

	setClauses, args := buildUpdateSet(input)
	if len(setClauses) == 0 {
		return Task{}, fmt.Errorf("repository.Update: %w", ErrNoFieldsToUpdate)
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	idArg := len(args) + 1
	callerArg := idArg + 1
	adminArg := idArg + 2
	args = append(args, id, scope.UserID, scope.IsAdmin)

	// The task must currently live in a project the caller can reach.
	conditions := []string{
		fmt.Sprintf("id = $%d", idArg),
		fmt.Sprintf("project_id IN (SELECT id FROM projects WHERE owner_id = $%d OR $%d)", callerArg, adminArg),
	}

	// Moving a task: the DESTINATION must be reachable too, otherwise a
	// member could push a task into someone else's project. Checking it in
	// the same statement leaves no window between the check and the write.
	if input.ProjectID != nil {
		conditions = append(conditions, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM projects WHERE id = $%d AND (owner_id = $%d OR $%d))",
			len(args)+1, callerArg, adminArg))
		args = append(args, *input.ProjectID)
	}

	query := fmt.Sprintf(`
		UPDATE tasks
		SET %s
		WHERE %s
		RETURNING `+taskColumns, strings.Join(setClauses, ", "), strings.Join(conditions, "\n\t\t  AND "))

	t, err := scanTask(r.db.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Either the task is out of reach, or the destination project
			// is. Both answer "not found" so neither can be probed.
			return Task{}, fmt.Errorf("repository.Update: %w", ErrTaskNotFound)
		}
		if database.ForeignKeyViolation(err, "tasks_assignee_id_fkey") {
			return Task{}, fmt.Errorf("repository.Update: %w", ErrAssigneeNotFound)
		}
		if database.ForeignKeyViolation(err, "tasks_project_id_fkey") {
			return Task{}, fmt.Errorf("repository.Update: %w", ErrProjectNotFound)
		}
		return Task{}, fmt.Errorf("repository.Update: failed to update task: %w", err)
	}

	return t, nil
}

func (r *repository) Delete(ctx context.Context, id uuid.UUID, scope authz.Scope) error {
	defer metrics.ObserveQuery("delete", "tasks")()

	const query = `
		DELETE FROM tasks
		WHERE id = $1
		  AND project_id IN (SELECT id FROM projects WHERE owner_id = $2 OR $3)
	`

	result, err := r.db.Exec(ctx, query, id, scope.UserID, scope.IsAdmin)
	if err != nil {
		return fmt.Errorf("repository.Delete: failed to delete task: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("repository.Delete: %w", ErrTaskNotFound)
	}

	return nil
}
