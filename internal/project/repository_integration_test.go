package project

import (
	"errors"
	"testing"

	"github.com/abneribeiro/internal/authz"
	"github.com/abneribeiro/internal/testdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedUser(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := db.Exec(testdb.Context(t),
		`INSERT INTO users (id, name, email, password) VALUES ($1, 'Test', $2, 'x')`,
		id, id.String()+"@test.invalid")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// Cascades to the user's projects.
	t.Cleanup(func() {
		_, _ = db.Exec(testdb.Context(t), `DELETE FROM users WHERE id = $1`, id)
	})

	return id
}

func TestRepositoryCreateSetsOwner(t *testing.T) {
	db := testdb.Pool(t)
	repo := NewRepository(db)
	owner := seedUser(t, db)

	p, err := repo.Create(testdb.Context(t), uuid.New(), owner, CreateInput{Name: "Owned"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if p.OwnerID != owner {
		t.Errorf("owner = %v, want %v", p.OwnerID, owner)
	}
	if p.Description != "" {
		t.Errorf("description = %q, want the empty default", p.Description)
	}
}

func TestRepositoryOwnershipIsEnforcedInSQL(t *testing.T) {
	db := testdb.Pool(t)
	ctx := testdb.Context(t)
	repo := NewRepository(db)

	owner := seedUser(t, db)
	intruder := seedUser(t, db)
	ownerScope := authz.Scope{UserID: owner}
	intruderScope := authz.Scope{UserID: intruder}

	p, err := repo.Create(ctx, uuid.New(), owner, CreateInput{Name: "Private"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := repo.GetByID(ctx, p.ID, intruderScope); !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("GetByID = %v, want ErrProjectNotFound", err)
	}

	name := "hijacked"
	if _, err := repo.Update(ctx, p.ID, intruderScope, UpdateInput{Name: &name}); !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("Update = %v, want ErrProjectNotFound", err)
	}

	if err := repo.Delete(ctx, p.ID, intruderScope); !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("Delete = %v, want ErrProjectNotFound", err)
	}

	// The owner still reaches it, so the filter is not simply rejecting all.
	if _, err := repo.GetByID(ctx, p.ID, ownerScope); err != nil {
		t.Errorf("the owner was refused their own project: %v", err)
	}

	// An admin is not restricted by ownership.
	if _, err := repo.GetByID(ctx, p.ID, authz.Scope{UserID: intruder, IsAdmin: true}); err != nil {
		t.Errorf("admin was refused: %v", err)
	}
}

func TestRepositoryListIsScopedAndPaginated(t *testing.T) {
	db := testdb.Pool(t)
	ctx := testdb.Context(t)
	repo := NewRepository(db)

	owner := seedUser(t, db)
	other := seedUser(t, db)

	for i := 0; i < 3; i++ {
		if _, err := repo.Create(ctx, uuid.New(), owner, CreateInput{Name: "Mine"}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if _, err := repo.Create(ctx, uuid.New(), other, CreateInput{Name: "Theirs"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	mine, err := repo.List(ctx, authz.Scope{UserID: owner}, ListFilter{Limit: 100})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(mine) != 3 {
		t.Errorf("owner sees %d projects, want 3", len(mine))
	}
	for _, p := range mine {
		if p.OwnerID != owner {
			t.Errorf("list returned a project owned by %v", p.OwnerID)
		}
	}

	page, err := repo.List(ctx, authz.Scope{UserID: owner}, ListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page) != 2 {
		t.Errorf("limit=2 returned %d rows", len(page))
	}
}

// Tasks hang off projects with ON DELETE CASCADE.
func TestRepositoryDeleteCascadesToTasks(t *testing.T) {
	db := testdb.Pool(t)
	ctx := testdb.Context(t)
	repo := NewRepository(db)

	owner := seedUser(t, db)
	p, err := repo.Create(ctx, uuid.New(), owner, CreateInput{Name: "With tasks"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	taskID := uuid.New()
	if _, err := db.Exec(ctx,
		`INSERT INTO tasks (id, title, project_id) VALUES ($1, 'child', $2)`, taskID, p.ID); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	if err := repo.Delete(ctx, p.ID, authz.Scope{UserID: owner}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var remaining int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE id = $1`, taskID).Scan(&remaining); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if remaining != 0 {
		t.Error("deleting a project left its tasks behind")
	}
}
