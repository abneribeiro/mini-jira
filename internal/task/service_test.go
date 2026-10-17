package task

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/abneribeiro/internal/authz"
	"github.com/google/uuid"
)

type fakeRepo struct {
	createdID, createdOwner uuid.UUID
	createdInput            CreateInput

	gotID    uuid.UUID
	gotScope authz.Scope
	gotInput UpdateInput

	listScope  authz.Scope
	listFilter ListFilter

	err error
}

func (f *fakeRepo) Create(_ context.Context, id uuid.UUID, scope authz.Scope, input CreateInput) (Task, error) {
	f.createdID, f.createdOwner, f.createdInput = id, scope.UserID, input
	return Task{ID: id}, f.err
}

func (f *fakeRepo) GetByID(_ context.Context, id uuid.UUID, scope authz.Scope) (Task, error) {
	f.gotID, f.gotScope = id, scope
	return Task{ID: id}, f.err
}

func (f *fakeRepo) List(_ context.Context, scope authz.Scope, filter ListFilter) ([]Task, error) {
	f.listScope, f.listFilter = scope, filter
	return []Task{}, f.err
}

func (f *fakeRepo) Update(_ context.Context, id uuid.UUID, scope authz.Scope, input UpdateInput) (Task, error) {
	f.gotID, f.gotScope, f.gotInput = id, scope, input
	return Task{ID: id}, f.err
}

func (f *fakeRepo) Delete(_ context.Context, id uuid.UUID, scope authz.Scope) error {
	f.gotID, f.gotScope = id, scope
	return f.err
}

func TestCreateValidation(t *testing.T) {
	project := uuid.New()
	nilAssignee := uuid.Nil

	tests := []struct {
		name     string
		owner    uuid.UUID
		input    CreateInput
		contains string
	}{
		{"empty title", uuid.New(), CreateInput{ProjectID: project}, "title is required"},
		{"blank title", uuid.New(), CreateInput{Title: "  ", ProjectID: project}, "title is required"},
		{"title too long", uuid.New(), CreateInput{Title: strings.Repeat("a", 201), ProjectID: project}, "at most 200"},
		{"description too long", uuid.New(), CreateInput{Title: "t", Description: strings.Repeat("a", 2001), ProjectID: project}, "at most 2000"},
		{"missing project", uuid.New(), CreateInput{Title: "t"}, "project_id is required"},
		{"zero assignee", uuid.New(), CreateInput{Title: "t", ProjectID: project, AssigneeID: &nilAssignee}, "assignee_id must be a valid UUID"},
		{"missing owner", uuid.Nil, CreateInput{Title: "t", ProjectID: project}, "owner is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewService(&fakeRepo{}).Create(context.Background(), authz.Scope{UserID: tt.owner}, tt.input)

			if !errors.Is(err, ErrInvalidTask) {
				t.Fatalf("errors.Is(err, ErrInvalidTask) = false, err = %v", err)
			}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("error = %q, want it to mention %q", err, tt.contains)
			}
		})
	}
}

func TestUpdateStatusValidation(t *testing.T) {
	for _, status := range []string{"TODO", "IN_PROGRESS", "DONE"} {
		t.Run("accepts "+status, func(t *testing.T) {
			if _, err := NewService(&fakeRepo{}).Update(context.Background(), uuid.New(), authz.Scope{UserID: uuid.New()}, UpdateInput{Status: &status}); err != nil {
				t.Errorf("status %q rejected: %v", status, err)
			}
		})
	}

	for _, status := range []string{"todo", "BOGUS", "", "Done"} {
		t.Run("rejects "+status, func(t *testing.T) {
			_, err := NewService(&fakeRepo{}).Update(context.Background(), uuid.New(), authz.Scope{UserID: uuid.New()}, UpdateInput{Status: &status})
			if !errors.Is(err, ErrInvalidTask) {
				t.Errorf("status %q accepted, want rejection", status)
			}
		})
	}
}

// The tri-state is the fix for "assignee_id can never be cleared": absent and
// explicit null must reach the repository as different things.
func TestAssigneeTriStateReachesRepository(t *testing.T) {
	assignee := uuid.New()

	tests := []struct {
		name    string
		body    string
		wantSet bool
		wantNil bool
	}{
		{"absent leaves it alone", `{"title":"t"}`, false, true},
		{"explicit null clears it", `{"assignee_id":null}`, true, true},
		{"explicit uuid assigns it", `{"assignee_id":"` + assignee.String() + `"}`, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var input UpdateInput
			if err := json.Unmarshal([]byte(tt.body), &input); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			repo := &fakeRepo{}
			if _, err := NewService(repo).Update(context.Background(), uuid.New(), authz.Scope{UserID: uuid.New()}, input); err != nil {
				t.Fatalf("Update: %v", err)
			}

			if got := repo.gotInput.AssigneeID.Set; got != tt.wantSet {
				t.Errorf("AssigneeID.Set = %v, want %v", got, tt.wantSet)
			}
			if got := repo.gotInput.AssigneeID.Value == nil; got != tt.wantNil {
				t.Errorf("AssigneeID.Value == nil is %v, want %v", got, tt.wantNil)
			}
		})
	}
}

func TestListFilterDefaultsAndLimits(t *testing.T) {
	owner := uuid.New()
	scope := authz.Scope{UserID: owner}
	ctx := context.Background()

	t.Run("applies the default limit", func(t *testing.T) {
		repo := &fakeRepo{}
		if _, err := NewService(repo).List(ctx, scope, ListFilter{}); err != nil {
			t.Fatal(err)
		}
		if repo.listFilter.Limit != DefaultListLimit {
			t.Errorf("limit = %d, want the default %d", repo.listFilter.Limit, DefaultListLimit)
		}
		if repo.listScope.UserID != owner {
			t.Errorf("owner = %v, want %v", repo.listScope.UserID, owner)
		}
	})

	t.Run("rejects a limit above the cap", func(t *testing.T) {
		_, err := NewService(&fakeRepo{}).List(ctx, scope, ListFilter{Limit: MaxListLimit + 1})
		if !errors.Is(err, ErrInvalidTask) {
			t.Errorf("err = %v, want ErrInvalidTask", err)
		}
	})

	t.Run("rejects a negative offset", func(t *testing.T) {
		_, err := NewService(&fakeRepo{}).List(ctx, scope, ListFilter{Offset: -1})
		if !errors.Is(err, ErrInvalidTask) {
			t.Errorf("err = %v, want ErrInvalidTask", err)
		}
	})

	t.Run("rejects an unknown status", func(t *testing.T) {
		bogus := "BOGUS"
		_, err := NewService(&fakeRepo{}).List(ctx, scope, ListFilter{Status: &bogus})
		if !errors.Is(err, ErrInvalidTask) {
			t.Errorf("err = %v, want ErrInvalidTask", err)
		}
	})

	t.Run("forwards the filters it was given", func(t *testing.T) {
		repo := &fakeRepo{}
		project, assignee, status := uuid.New(), uuid.New(), "DONE"

		_, err := NewService(repo).List(ctx, scope, ListFilter{
			ProjectID: &project, AssigneeID: &assignee, Status: &status, Limit: 10, Offset: 5,
		})
		if err != nil {
			t.Fatal(err)
		}

		got := repo.listFilter
		if *got.ProjectID != project || *got.AssigneeID != assignee || *got.Status != status {
			t.Errorf("filters were altered in transit: %+v", got)
		}
		if got.Limit != 10 || got.Offset != 5 {
			t.Errorf("limit/offset = %d/%d, want 10/5", got.Limit, got.Offset)
		}
	})
}

func TestOwnerIsForwardedToRepository(t *testing.T) {
	owner, id := uuid.New(), uuid.New()
	scope := authz.Scope{UserID: owner}
	ctx := context.Background()

	for _, tt := range []struct {
		name string
		call func(Service) error
	}{
		{"getByID", func(s Service) error { _, err := s.GetByID(ctx, id, scope); return err }},
		{"update", func(s Service) error {
			title := "x"
			_, err := s.Update(ctx, id, scope, UpdateInput{Title: &title})
			return err
		}},
		{"delete", func(s Service) error { return s.Delete(ctx, id, scope) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			if err := tt.call(NewService(repo)); err != nil {
				t.Fatal(err)
			}
			if repo.gotScope.UserID != owner || repo.gotID != id {
				t.Errorf("got id=%v owner=%v, want id=%v owner=%v", repo.gotID, repo.gotScope.UserID, id, owner)
			}
		})
	}
}

func TestNotFoundPropagates(t *testing.T) {
	for name, sentinel := range map[string]error{
		"task":     ErrTaskNotFound,
		"project":  ErrProjectNotFound,
		"assignee": ErrAssigneeNotFound,
	} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepo{err: sentinel}
			_, err := NewService(repo).Create(context.Background(), authz.Scope{UserID: uuid.New()}, CreateInput{Title: "t", ProjectID: uuid.New()})
			if !errors.Is(err, sentinel) {
				t.Fatalf("errors.Is(err, %v) = false, err = %v", sentinel, err)
			}
		})
	}
}
