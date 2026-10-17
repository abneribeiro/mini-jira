package user

import (
	"errors"
	"testing"

	"github.com/abneribeiro/internal/authz"
	"github.com/abneribeiro/internal/testdb"
	"github.com/google/uuid"
)

func newUserInput(suffix string) CreateInput {
	return CreateInput{Name: "Integration", Email: suffix + "@test.invalid", Password: "hashed-placeholder"}
}

func createUser(t *testing.T, repo Repository) User {
	t.Helper()

	u, err := repo.Create(testdb.Context(t), uuid.New(), newUserInput(uuid.NewString()))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	t.Cleanup(func() { _ = repo.Delete(testdb.Context(t), u.ID) })

	return u
}

func TestRepositoryCreateDefaultsToMember(t *testing.T) {
	repo := NewRepository(testdb.Pool(t))

	u := createUser(t, repo)

	if u.Role != authz.RoleMember {
		t.Errorf("role = %q, want %q for a new account", u.Role, authz.RoleMember)
	}
	if u.Password != "" {
		t.Error("Create returned the password hash; it must not leave the repository")
	}
}

// The unique index is what stops duplicate accounts; the repository has to
// turn it into a domain error rather than leaking the driver message.
func TestRepositoryDuplicateEmail(t *testing.T) {
	repo := NewRepository(testdb.Pool(t))
	ctx := testdb.Context(t)

	first := createUser(t, repo)

	_, err := repo.Create(ctx, uuid.New(), CreateInput{
		Name: "Clash", Email: first.Email, Password: "x",
	})
	if !errors.Is(err, ErrEmailAlreadyExists) {
		t.Fatalf("err = %v, want ErrEmailAlreadyExists", err)
	}

	t.Run("also on update", func(t *testing.T) {
		second := createUser(t, repo)

		email := first.Email
		_, err := repo.Update(ctx, second.ID, UpdateInput{Email: &email})
		if !errors.Is(err, ErrEmailAlreadyExists) {
			t.Errorf("err = %v, want ErrEmailAlreadyExists", err)
		}
	})
}

func TestRepositoryUpdateAndRole(t *testing.T) {
	repo := NewRepository(testdb.Pool(t))
	ctx := testdb.Context(t)

	u := createUser(t, repo)

	name := "Renamed"
	updated, err := repo.Update(ctx, u.ID, UpdateInput{Name: &name})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != name {
		t.Errorf("name = %q, want %q", updated.Name, name)
	}
	if !updated.UpdatedAt.After(u.UpdatedAt) {
		t.Error("updated_at did not move")
	}

	promoted, err := repo.UpdateRole(ctx, u.ID, authz.RoleAdmin)
	if err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	if promoted.Role != authz.RoleAdmin {
		t.Errorf("role = %q, want admin", promoted.Role)
	}

	// GetByEmail is the login path and is the only one that may see the hash.
	withSecret, err := repo.GetByEmail(ctx, u.Email)
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if withSecret.Password == "" {
		t.Error("GetByEmail did not return the password hash, so login cannot work")
	}
	if withSecret.Role != authz.RoleAdmin {
		t.Errorf("GetByEmail role = %q, want admin", withSecret.Role)
	}
}

func TestRepositoryNotFound(t *testing.T) {
	repo := NewRepository(testdb.Pool(t))
	ctx := testdb.Context(t)
	ghost := uuid.New()
	name := "x"

	if _, err := repo.GetByID(ctx, ghost); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("GetByID = %v, want ErrUserNotFound", err)
	}
	if _, err := repo.GetByEmail(ctx, "nobody@test.invalid"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("GetByEmail = %v, want ErrUserNotFound", err)
	}
	if _, err := repo.Update(ctx, ghost, UpdateInput{Name: &name}); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("Update = %v, want ErrUserNotFound", err)
	}
	if _, err := repo.UpdateRole(ctx, ghost, authz.RoleAdmin); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("UpdateRole = %v, want ErrUserNotFound", err)
	}
	if err := repo.Delete(ctx, ghost); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("Delete = %v, want ErrUserNotFound", err)
	}
	if _, err := repo.Update(ctx, ghost, UpdateInput{}); !errors.Is(err, ErrNoFieldsToUpdate) {
		t.Errorf("empty update = %v, want ErrNoFieldsToUpdate", err)
	}
}

func TestRepositoryListPagination(t *testing.T) {
	repo := NewRepository(testdb.Pool(t))
	ctx := testdb.Context(t)

	for range 3 {
		createUser(t, repo)
	}

	page, err := repo.List(ctx, ListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page) != 2 {
		t.Errorf("limit=2 returned %d rows", len(page))
	}
	for _, u := range page {
		if u.Password != "" {
			t.Error("List leaked a password hash")
		}
	}

	offset, err := repo.List(ctx, ListFilter{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(offset) > 0 && len(page) > 0 && offset[0].ID == page[0].ID {
		t.Error("offset did not shift the window")
	}
}
