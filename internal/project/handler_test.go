package project

import (
	"context"
	"encoding/json"
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
	project  Project
	projects []Project
	err      error

	gotOwner  uuid.UUID
	gotScope  authz.Scope
	gotFilter ListFilter
}

func (s *stubService) Create(_ context.Context, scope authz.Scope, _ CreateInput) (Project, error) {
	s.gotOwner, s.gotScope = scope.UserID, scope
	return s.project, s.err
}

func (s *stubService) GetByID(_ context.Context, _ uuid.UUID, scope authz.Scope) (Project, error) {
	s.gotOwner, s.gotScope = scope.UserID, scope
	return s.project, s.err
}

func (s *stubService) List(_ context.Context, scope authz.Scope, filter ListFilter) ([]Project, error) {
	s.gotOwner, s.gotScope, s.gotFilter = scope.UserID, scope, filter
	return s.projects, s.err
}

func (s *stubService) Update(_ context.Context, _ uuid.UUID, scope authz.Scope, _ UpdateInput) (Project, error) {
	s.gotOwner, s.gotScope = scope.UserID, scope
	return s.project, s.err
}

func (s *stubService) Delete(_ context.Context, _ uuid.UUID, scope authz.Scope) error {
	s.gotOwner, s.gotScope = scope.UserID, scope
	return s.err
}

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

func TestOwnerComesFromTheToken(t *testing.T) {
	caller := uuid.New()
	svc := &stubService{project: Project{ID: uuid.New()}}
	h := testRouter(svc)
	token := tokenFor(t, caller)

	for _, tt := range []struct{ name, method, target, body string }{
		{"create", http.MethodPost, "/", `{"name":"P"}`},
		{"list", http.MethodGet, "/", ""},
		{"get", http.MethodGet, "/" + uuid.New().String(), ""},
		{"update", http.MethodPut, "/" + uuid.New().String(), `{"name":"P2"}`},
		{"delete", http.MethodDelete, "/" + uuid.New().String(), ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc.gotOwner = uuid.Nil

			do(t, h, tt.method, tt.target, tt.body, token)

			if svc.gotOwner != caller {
				t.Errorf("owner = %v, want the token subject %v", svc.gotOwner, caller)
			}
		})
	}
}

// A project owned by someone else is reported as missing, not forbidden, so
// the API cannot be used to discover which IDs exist.
func TestOtherOwnersProjectLooksMissing(t *testing.T) {
	h := testRouter(&stubService{err: ErrProjectNotFound})

	rec := do(t, h, http.MethodGet, "/"+uuid.New().String(), "", tokenFor(t, uuid.New()))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}

	var body struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON (%v): %s", err, rec.Body.String())
	}
	if body.Error.Code != "project_not_found" {
		t.Errorf("code = %q, want project_not_found", body.Error.Code)
	}
}

func TestEmptyListIsAnArray(t *testing.T) {
	h := testRouter(&stubService{projects: []Project{}})

	rec := do(t, h, http.MethodGet, "/", "", tokenFor(t, uuid.New()))

	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("body = %s, want []", got)
	}
}
