package project

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/abneribeiro/internal/authz"
	"github.com/google/uuid"
)

// fakeRepo records the ownerID the service forwards, which is what the
// authorization scoping depends on.
type fakeRepo struct {
	createdID, createdOwner uuid.UUID
	createdInput            CreateInput

	gotID    uuid.UUID
	gotScope authz.Scope
	gotInput UpdateInput

	listScope  authz.Scope
	listFilter ListFilter
	err        error
}

func (f *fakeRepo) Create(_ context.Context, id, ownerID uuid.UUID, input CreateInput) (Project, error) {
	f.createdID, f.createdOwner, f.createdInput = id, ownerID, input
	return Project{ID: id, OwnerID: ownerID}, f.err
}

func (f *fakeRepo) GetByID(_ context.Context, id uuid.UUID, scope authz.Scope) (Project, error) {
	f.gotID, f.gotScope = id, scope
	return Project{ID: id, OwnerID: scope.UserID}, f.err
}

func (f *fakeRepo) List(_ context.Context, scope authz.Scope, filter ListFilter) ([]Project, error) {
	f.listScope, f.listFilter = scope, filter
	return []Project{}, f.err
}

func (f *fakeRepo) Update(_ context.Context, id uuid.UUID, scope authz.Scope, input UpdateInput) (Project, error) {
	f.gotID, f.gotScope, f.gotInput = id, scope, input
	return Project{ID: id, OwnerID: scope.UserID}, f.err
}

func (f *fakeRepo) Delete(_ context.Context, id uuid.UUID, scope authz.Scope) error {
	f.gotID, f.gotScope = id, scope
	return f.err
}

func TestCreateValidation(t *testing.T) {
	tests := []struct {
		name     string
		input    CreateInput
		owner    uuid.UUID
		contains string
	}{
		{"empty name", CreateInput{Name: ""}, uuid.New(), "name is required"},
		{"blank name", CreateInput{Name: "   "}, uuid.New(), "name is required"},
		{"name too long", CreateInput{Name: strings.Repeat("a", 101)}, uuid.New(), "at most 100"},
		{"description too long", CreateInput{Name: "ok", Description: strings.Repeat("a", 2001)}, uuid.New(), "at most 2000"},
		{"missing owner", CreateInput{Name: "ok"}, uuid.Nil, "owner is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewService(&fakeRepo{}).Create(context.Background(), tt.owner, tt.input)

			if err == nil {
				t.Fatal("got nil error, want a validation failure")
			}
			if !errors.Is(err, ErrInvalidProject) {
				t.Errorf("errors.Is(err, ErrInvalidProject) = false, err = %v", err)
			}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("error = %q, want it to mention %q", err, tt.contains)
			}
		})
	}
}

// The owner must reach the repository on every call; that argument is the
// whole authorization boundary.
func TestOwnerIsForwardedToRepository(t *testing.T) {
	owner := uuid.New()
	scope := authz.Scope{UserID: owner}
	id := uuid.New()
	ctx := context.Background()

	t.Run("create", func(t *testing.T) {
		repo := &fakeRepo{}
		if _, err := NewService(repo).Create(ctx, owner, CreateInput{Name: "P"}); err != nil {
			t.Fatal(err)
		}
		if repo.createdOwner != owner {
			t.Errorf("owner = %v, want %v", repo.createdOwner, owner)
		}
		if repo.createdID == uuid.Nil {
			t.Error("service did not generate an ID")
		}
	})

	for _, tt := range []struct {
		name string
		call func(Service) error
	}{
		{"getByID", func(s Service) error { _, err := s.GetByID(ctx, id, scope); return err }},
		{"update", func(s Service) error {
			n := "x"
			_, err := s.Update(ctx, id, scope, UpdateInput{Name: &n})
			return err
		}},
		{"delete", func(s Service) error { return s.Delete(ctx, id, scope) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			if err := tt.call(NewService(repo)); err != nil {
				t.Fatal(err)
			}
			if repo.gotScope.UserID != owner {
				t.Errorf("owner = %v, want %v", repo.gotScope.UserID, owner)
			}
			if repo.gotID != id {
				t.Errorf("id = %v, want %v", repo.gotID, id)
			}
		})
	}

	t.Run("list", func(t *testing.T) {
		repo := &fakeRepo{}
		if _, err := NewService(repo).List(ctx, scope, ListFilter{}); err != nil {
			t.Fatal(err)
		}
		if repo.listScope.UserID != owner {
			t.Errorf("owner = %v, want %v", repo.listScope.UserID, owner)
		}
		if repo.listFilter.Limit != DefaultListLimit {
			t.Errorf("limit = %d, want the default %d", repo.listFilter.Limit, DefaultListLimit)
		}
	})
}

func TestUpdateValidation(t *testing.T) {
	ptr := func(s string) *string { return &s }

	tests := []struct {
		name     string
		input    UpdateInput
		contains string
	}{
		{"empty name", UpdateInput{Name: ptr("")}, "name is required"},
		{"name too long", UpdateInput{Name: ptr(strings.Repeat("a", 101))}, "at most 100"},
		{"description too long", UpdateInput{Description: ptr(strings.Repeat("a", 2001))}, "at most 2000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewService(&fakeRepo{}).Update(context.Background(), uuid.New(), authz.Scope{UserID: uuid.New()}, tt.input)

			if !errors.Is(err, ErrInvalidProject) {
				t.Fatalf("errors.Is(err, ErrInvalidProject) = false, err = %v", err)
			}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("error = %q, want it to mention %q", err, tt.contains)
			}
		})
	}
}

func TestNotFoundPropagates(t *testing.T) {
	repo := &fakeRepo{err: ErrProjectNotFound}

	_, err := NewService(repo).GetByID(context.Background(), uuid.New(), authz.Scope{UserID: uuid.New()})

	if !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("errors.Is(err, ErrProjectNotFound) = false, err = %v", err)
	}
}
