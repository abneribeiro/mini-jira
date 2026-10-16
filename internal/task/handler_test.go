package task

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abneribeiro/internal/authz"
	"github.com/abneribeiro/internal/jwt"
	authmiddleware "github.com/abneribeiro/internal/middleware"
	"github.com/google/uuid"
)

const testSecret = "test-secret-at-least-32-characters-long"

type stubService struct {
	task  Task
	tasks []Task
	err   error

	gotOwner  uuid.UUID
	gotFilter ListFilter
}

func (s *stubService) Create(_ context.Context, scope authz.Scope, _ CreateInput) (Task, error) {
	s.gotOwner = scope.UserID
	return s.task, s.err
}

func (s *stubService) GetByID(_ context.Context, _ uuid.UUID, scope authz.Scope) (Task, error) {
	s.gotOwner = scope.UserID
	return s.task, s.err
}

func (s *stubService) List(_ context.Context, scope authz.Scope, filter ListFilter) ([]Task, error) {
	s.gotOwner, s.gotFilter = scope.UserID, filter
	return s.tasks, s.err
}

func (s *stubService) Update(_ context.Context, _ uuid.UUID, scope authz.Scope, _ UpdateInput) (Task, error) {
	s.gotOwner = scope.UserID
	return s.task, s.err
}

func (s *stubService) Delete(_ context.Context, _ uuid.UUID, scope authz.Scope) error {
	s.gotOwner = scope.UserID
	return s.err
}

// Tasks are protected by the parent router in server.New, so the test mounts
// the same middleware to reproduce production routing.
func testRouter(svc Service) http.Handler {
	return authmiddleware.AuthMiddleware(jwt.NewManager(testSecret))(routes(NewHandler(svc)))
}

func tokenFor(t *testing.T, id uuid.UUID) string {
	t.Helper()

	token, err := jwt.NewManager(testSecret).GenerateToken(id, "caller@example.com", authz.RoleMember)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	return token
}

func do(t *testing.T, h http.Handler, method, target, body, token string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req := httptest.NewRequest(method, target, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON (%v): %s", err, rec.Body.String())
	}

	return body.Error.Code
}

func TestEveryRouteRequiresAuth(t *testing.T) {
	h := testRouter(&stubService{})
	id := uuid.New().String()

	for _, tt := range []struct{ method, target string }{
		{http.MethodGet, "/"},
		{http.MethodPost, "/"},
		{http.MethodGet, "/" + id},
		{http.MethodPut, "/" + id},
		{http.MethodDelete, "/" + id},
	} {
		t.Run(tt.method+" "+tt.target, func(t *testing.T) {
			if rec := do(t, h, tt.method, tt.target, "", ""); rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})
	}
}

// The caller's ID must come from the token, never from the request body or
// query string.
func TestOwnerComesFromTheToken(t *testing.T) {
	caller := uuid.New()
	svc := &stubService{task: Task{ID: uuid.New()}}
	h := testRouter(svc)

	if rec := do(t, h, http.MethodGet, "/", "", tokenFor(t, caller)); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	if svc.gotOwner != caller {
		t.Errorf("owner = %v, want the token subject %v", svc.gotOwner, caller)
	}
}

func TestListFilterParsing(t *testing.T) {
	project, assignee := uuid.New(), uuid.New()
	caller := uuid.New()

	t.Run("parses every supported filter", func(t *testing.T) {
		svc := &stubService{}
		h := testRouter(svc)

		target := "/?project_id=" + project.String() +
			"&assignee_id=" + assignee.String() +
			"&status=DONE&limit=10&offset=5"

		if rec := do(t, h, http.MethodGet, target, "", tokenFor(t, caller)); rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		got := svc.gotFilter
		if got.ProjectID == nil || *got.ProjectID != project {
			t.Errorf("ProjectID = %v, want %v", got.ProjectID, project)
		}
		if got.AssigneeID == nil || *got.AssigneeID != assignee {
			t.Errorf("AssigneeID = %v, want %v", got.AssigneeID, assignee)
		}
		if got.Status == nil || *got.Status != "DONE" {
			t.Errorf("Status = %v, want DONE", got.Status)
		}
		if got.Limit != 10 || got.Offset != 5 {
			t.Errorf("limit/offset = %d/%d, want 10/5", got.Limit, got.Offset)
		}
	})

	t.Run("defaults to no filters", func(t *testing.T) {
		svc := &stubService{}
		h := testRouter(svc)

		do(t, h, http.MethodGet, "/", "", tokenFor(t, caller))

		got := svc.gotFilter
		if got.ProjectID != nil || got.AssigneeID != nil || got.Status != nil {
			t.Errorf("unset query params produced filters: %+v", got)
		}
		if got.Limit != DefaultListLimit {
			t.Errorf("limit = %d, want the default %d", got.Limit, DefaultListLimit)
		}
	})

	for _, tt := range []struct{ name, query string }{
		{"non-uuid project_id", "?project_id=nope"},
		{"non-uuid assignee_id", "?assignee_id=nope"},
		{"non-numeric limit", "?limit=lots"},
		{"non-numeric offset", "?offset=far"},
	} {
		t.Run(tt.name+" is rejected", func(t *testing.T) {
			rec := do(t, testRouter(&stubService{}), http.MethodGet, "/"+tt.query, "", tokenFor(t, caller))

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
			if code := errorCode(t, rec); code != "validation_failed" {
				t.Errorf("code = %q, want validation_failed", code)
			}
		})
	}
}

// A bad project_id or assignee_id is an unprocessable reference in the body,
// not a missing task.
func TestErrorStatusMapping(t *testing.T) {
	caller := uuid.New()

	for _, tt := range []struct {
		name     string
		err      error
		want     int
		wantCode string
	}{
		{"task not found", ErrTaskNotFound, http.StatusNotFound, "task_not_found"},
		{"project not found", ErrProjectNotFound, http.StatusUnprocessableEntity, "project_not_found"},
		{"assignee not found", ErrAssigneeNotFound, http.StatusUnprocessableEntity, "assignee_not_found"},
		{"no fields", ErrNoFieldsToUpdate, http.StatusBadRequest, "no_fields_to_update"},
		{"unexpected", errors.New("boom"), http.StatusInternalServerError, "internal_error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := testRouter(&stubService{err: tt.err})

			rec := do(t, h, http.MethodGet, "/"+uuid.New().String(), "", tokenFor(t, caller))

			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
			if code := errorCode(t, rec); code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
		})
	}
}

// An empty result must serialise as [] so clients can iterate it.
func TestEmptyListIsAnArray(t *testing.T) {
	h := testRouter(&stubService{tasks: []Task{}})

	rec := do(t, h, http.MethodGet, "/", "", tokenFor(t, uuid.New()))

	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("body = %s, want []", got)
	}
}
