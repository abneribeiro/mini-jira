package task

import (
	"errors"
	"testing"

	"github.com/abneribeiro/internal/authz"
	"github.com/abneribeiro/internal/testdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedUser inserts a user directly; the task package has no user repository.
func seedUser(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()

	ctx := testdb.Context(t)
	id := uuid.New()

	_, err := db.Exec(ctx,
		`INSERT INTO users (id, name, email, password) VALUES ($1, 'Test', $2, 'x')`,
		id, id.String()+"@test.invalid")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// Cascades to the user's projects and their tasks.
	t.Cleanup(func() {
		_, _ = db.Exec(testdb.Context(t), `DELETE FROM users WHERE id = $1`, id)
	})

	return id
}

func seedProject(t *testing.T, db *pgxpool.Pool, owner uuid.UUID) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := db.Exec(testdb.Context(t),
		`INSERT INTO projects (id, owner_id, name) VALUES ($1, $2, 'Test project')`, id, owner)
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}

	return id
}

func TestRepositoryCreateAndGet(t *testing.T) {
	db := testdb.Pool(t)
	ctx := testdb.Context(t)
	repo := NewRepository(db)

	owner := seedUser(t, db)
	project := seedProject(t, db, owner)
	scope := authz.Scope{UserID: owner}

	created, err := repo.Create(ctx, uuid.New(), scope, CreateInput{
		Title: "Integration task", Description: "d", ProjectID: project,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.Status != "TODO" {
		t.Errorf("status = %q, want the TODO default", created.Status)
	}
	if created.AssigneeID != nil {
		t.Errorf("assignee = %v, want nil", created.AssigneeID)
	}

	got, err := repo.GetByID(ctx, created.ID, scope)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID != created.ID || got.Title != "Integration task" {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

// The SQL enforces ownership; this is the layer the unit tests cannot reach.
func TestRepositoryEnforcesOwnership(t *testing.T) {
	db := testdb.Pool(t)
	ctx := testdb.Context(t)
	repo := NewRepository(db)

	owner := seedUser(t, db)
	intruder := seedUser(t, db)
	project := seedProject(t, db, owner)

	ownerScope := authz.Scope{UserID: owner}
	intruderScope := authz.Scope{UserID: intruder}

	task, err := repo.Create(ctx, uuid.New(), ownerScope, CreateInput{Title: "Private", ProjectID: project})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	t.Run("get is refused", func(t *testing.T) {
		if _, err := repo.GetByID(ctx, task.ID, intruderScope); !errors.Is(err, ErrTaskNotFound) {
			t.Errorf("err = %v, want ErrTaskNotFound", err)
		}
	})

	t.Run("update is refused", func(t *testing.T) {
		title := "hijacked"
		if _, err := repo.Update(ctx, task.ID, intruderScope, UpdateInput{Title: &title}); !errors.Is(err, ErrTaskNotFound) {
			t.Errorf("err = %v, want ErrTaskNotFound", err)
		}
	})

	t.Run("delete is refused", func(t *testing.T) {
		if err := repo.Delete(ctx, task.ID, intruderScope); !errors.Is(err, ErrTaskNotFound) {
			t.Errorf("err = %v, want ErrTaskNotFound", err)
		}
	})

	t.Run("create in another owner's project is refused", func(t *testing.T) {
		_, err := repo.Create(ctx, uuid.New(), intruderScope, CreateInput{Title: "sneak", ProjectID: project})
		if !errors.Is(err, ErrProjectNotFound) {
			t.Errorf("err = %v, want ErrProjectNotFound", err)
		}
	})

	t.Run("list excludes it", func(t *testing.T) {
		tasks, err := repo.List(ctx, intruderScope, ListFilter{Limit: 100})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, got := range tasks {
			if got.ID == task.ID {
				t.Error("another owner's task appeared in the list")
			}
		}
	})

	t.Run("admin reaches it", func(t *testing.T) {
		if _, err := repo.GetByID(ctx, task.ID, authz.Scope{UserID: intruder, IsAdmin: true}); err != nil {
			t.Errorf("admin was refused: %v", err)
		}
	})
}

// The behaviour the tri-state exists for, verified against real SQL rather
// than against the clause builder.
func TestRepositoryAssigneeClearing(t *testing.T) {
	db := testdb.Pool(t)
	ctx := testdb.Context(t)
	repo := NewRepository(db)

	owner := seedUser(t, db)
	project := seedProject(t, db, owner)
	scope := authz.Scope{UserID: owner}

	task, err := repo.Create(ctx, uuid.New(), scope, CreateInput{
		Title: "Assigned", ProjectID: project, AssigneeID: &owner,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if task.AssigneeID == nil || *task.AssigneeID != owner {
		t.Fatalf("assignee = %v, want %v", task.AssigneeID, owner)
	}

	cleared, err := repo.Update(ctx, task.ID, scope, UpdateInput{
		AssigneeID: NullableUUID{Set: true, Value: nil},
	})
	if err != nil {
		t.Fatalf("clear assignee: %v", err)
	}
	if cleared.AssigneeID != nil {
		t.Errorf("assignee = %v after an explicit null, want nil", cleared.AssigneeID)
	}

	title := "Renamed"
	untouched, err := repo.Update(ctx, task.ID, scope, UpdateInput{Title: &title})
	if err != nil {
		t.Fatalf("partial update: %v", err)
	}
	if untouched.AssigneeID != nil {
		t.Error("an absent assignee_id changed the column")
	}

	reassigned, err := repo.Update(ctx, task.ID, scope, UpdateInput{
		AssigneeID: NullableUUID{Set: true, Value: &owner},
	})
	if err != nil {
		t.Fatalf("reassign: %v", err)
	}
	if reassigned.AssigneeID == nil || *reassigned.AssigneeID != owner {
		t.Errorf("assignee = %v, want %v", reassigned.AssigneeID, owner)
	}
}

// Moving a task must respect ownership of the DESTINATION project, not just
// the current one.
func TestRepositoryMoveBetweenProjects(t *testing.T) {
	db := testdb.Pool(t)
	ctx := testdb.Context(t)
	repo := NewRepository(db)

	owner := seedUser(t, db)
	stranger := seedUser(t, db)
	source := seedProject(t, db, owner)
	destination := seedProject(t, db, owner)
	foreign := seedProject(t, db, stranger)

	scope := authz.Scope{UserID: owner}

	task, err := repo.Create(ctx, uuid.New(), scope, CreateInput{Title: "Movable", ProjectID: source})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	t.Run("moves within the caller's own projects", func(t *testing.T) {
		moved, err := repo.Update(ctx, task.ID, scope, UpdateInput{ProjectID: &destination})
		if err != nil {
			t.Fatalf("move: %v", err)
		}
		if moved.ProjectID != destination {
			t.Errorf("project = %v, want %v", moved.ProjectID, destination)
		}
	})

	t.Run("refuses a destination the caller does not own", func(t *testing.T) {
		if _, err := repo.Update(ctx, task.ID, scope, UpdateInput{ProjectID: &foreign}); !errors.Is(err, ErrTaskNotFound) {
			t.Errorf("err = %v, want the move to be refused", err)
		}

		// And the task must not have moved.
		got, err := repo.GetByID(ctx, task.ID, scope)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.ProjectID == foreign {
			t.Error("task was pushed into another owner's project")
		}
	})
}

func TestRepositoryListFilters(t *testing.T) {
	db := testdb.Pool(t)
	ctx := testdb.Context(t)
	repo := NewRepository(db)

	owner := seedUser(t, db)
	projectA := seedProject(t, db, owner)
	projectB := seedProject(t, db, owner)
	scope := authz.Scope{UserID: owner}

	for i := 0; i < 3; i++ {
		if _, err := repo.Create(ctx, uuid.New(), scope, CreateInput{Title: "A", ProjectID: projectA, AssigneeID: &owner}); err != nil {
			t.Fatalf("seed task: %v", err)
		}
	}
	inB, err := repo.Create(ctx, uuid.New(), scope, CreateInput{Title: "B", ProjectID: projectB})
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}

	done := "DONE"
	if _, err := repo.Update(ctx, inB.ID, scope, UpdateInput{Status: &done}); err != nil {
		t.Fatalf("set status: %v", err)
	}

	count := func(t *testing.T, filter ListFilter) int {
		t.Helper()

		filter.Limit = 100
		tasks, err := repo.List(ctx, scope, filter)
		if err != nil {
			t.Fatalf("List: %v", err)
		}

		return len(tasks)
	}

	todo := "TODO"
	if got := count(t, ListFilter{ProjectID: &projectA}); got != 3 {
		t.Errorf("project filter returned %d, want 3", got)
	}
	if got := count(t, ListFilter{Status: &done}); got != 1 {
		t.Errorf("status=DONE returned %d, want 1", got)
	}
	if got := count(t, ListFilter{Status: &todo}); got != 3 {
		t.Errorf("status=TODO returned %d, want 3", got)
	}
	if got := count(t, ListFilter{AssigneeID: &owner}); got != 3 {
		t.Errorf("assignee filter returned %d, want 3", got)
	}
	if got := count(t, ListFilter{ProjectID: &projectB, Status: &todo}); got != 0 {
		t.Errorf("combined filter returned %d, want 0", got)
	}

	t.Run("limit and offset paginate", func(t *testing.T) {
		page, err := repo.List(ctx, scope, ListFilter{Limit: 2})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(page) != 2 {
			t.Errorf("limit=2 returned %d rows", len(page))
		}

		rest, err := repo.List(ctx, scope, ListFilter{Limit: 100, Offset: 2})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(rest) != 2 {
			t.Errorf("offset=2 returned %d rows, want 2", len(rest))
		}
	})
}

func TestRepositoryNoFieldsToUpdate(t *testing.T) {
	db := testdb.Pool(t)
	ctx := testdb.Context(t)
	repo := NewRepository(db)

	owner := seedUser(t, db)

	if _, err := repo.Update(ctx, uuid.New(), authz.Scope{UserID: owner}, UpdateInput{}); !errors.Is(err, ErrNoFieldsToUpdate) {
		t.Errorf("err = %v, want ErrNoFieldsToUpdate", err)
	}
}

func TestRepositoryForeignKeyTranslation(t *testing.T) {
	db := testdb.Pool(t)
	ctx := testdb.Context(t)
	repo := NewRepository(db)

	owner := seedUser(t, db)
	project := seedProject(t, db, owner)
	scope := authz.Scope{UserID: owner}
	ghost := uuid.New()

	t.Run("unknown project", func(t *testing.T) {
		_, err := repo.Create(ctx, uuid.New(), scope, CreateInput{Title: "t", ProjectID: ghost})
		if !errors.Is(err, ErrProjectNotFound) {
			t.Errorf("err = %v, want ErrProjectNotFound", err)
		}
	})

	t.Run("unknown assignee", func(t *testing.T) {
		_, err := repo.Create(ctx, uuid.New(), scope, CreateInput{Title: "t", ProjectID: project, AssigneeID: &ghost})
		if !errors.Is(err, ErrAssigneeNotFound) {
			t.Errorf("err = %v, want ErrAssigneeNotFound", err)
		}
	})
}

// description is NOT NULL DEFAULT (empty string) since migration 00004; scanning a NULL
// into a plain string used to fail at runtime.
func TestRepositoryDescriptionNeverNull(t *testing.T) {
	db := testdb.Pool(t)
	ctx := testdb.Context(t)
	repo := NewRepository(db)

	owner := seedUser(t, db)
	project := seedProject(t, db, owner)
	scope := authz.Scope{UserID: owner}

	created, err := repo.Create(ctx, uuid.New(), scope, CreateInput{Title: "No description", ProjectID: project})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Description != "" {
		t.Errorf("description = %q, want the empty default", created.Description)
	}

	// An insert that bypasses the API must still be readable.
	direct := uuid.New()
	if _, err := db.Exec(ctx,
		`INSERT INTO tasks (id, title, project_id) VALUES ($1, 'Direct insert', $2)`, direct, project); err != nil {
		t.Fatalf("direct insert: %v", err)
	}

	got, err := repo.GetByID(ctx, direct, scope)
	if err != nil {
		t.Fatalf("GetByID on a directly inserted row: %v", err)
	}
	if got.Description != "" {
		t.Errorf("description = %q, want empty", got.Description)
	}
}
