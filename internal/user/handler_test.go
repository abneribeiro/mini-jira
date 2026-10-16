package user

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
	"github.com/google/uuid"
)

const testSecret = "test-secret-at-least-32-characters-long"

// stubService lets a handler test drive any outcome without a database.
type stubService struct {
	user      User
	users     []User
	createErr error
	updateErr error
	getErr    error
	deleteErr error
	listErr   error
}

func (s *stubService) Create(context.Context, CreateInput) (User, error) {
	return s.user, s.createErr
}
func (s *stubService) GetByID(context.Context, uuid.UUID) (User, error) { return s.user, s.getErr }
func (s *stubService) List(context.Context, ListFilter) ([]User, error) { return s.users, s.listErr }
func (s *stubService) UpdateRole(_ context.Context, _ uuid.UUID, role string) (User, error) {
	u := s.user
	u.Role = role
	return u, s.updateErr
}
func (s *stubService) Update(context.Context, uuid.UUID, UpdateInput) (User, error) {
	return s.user, s.updateErr
}
func (s *stubService) Delete(context.Context, uuid.UUID) error { return s.deleteErr }

func testRouter(svc Service) http.Handler {
	return routes(NewHandler(svc), jwt.NewManager(testSecret))
}

func tokenFor(t *testing.T, id uuid.UUID) string {
	t.Helper()

	return tokenForRole(t, id, authz.RoleMember)
}

func tokenForRole(t *testing.T, id uuid.UUID, role string) string {
	t.Helper()

	token, err := jwt.NewManager(testSecret).GenerateToken(id, "caller@example.com", role)
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
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

// errorCode decodes the shared envelope and fails if the body is not valid
// JSON, which is what the old fmt.Fprintf handlers produced for any error
// containing a quote.
func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not valid JSON (%v): %s", err, rec.Body.String())
	}
	if body.Error.Message == "" {
		t.Errorf("error envelope has no message: %s", rec.Body.String())
	}

	return body.Error.Code
}

// The defect that motivated the error-handling rework: a missing user on PUT
// returned 400 with the internal repository string in the body.
func TestUpdateMissingUserReturns404(t *testing.T) {
	caller := uuid.New()
	h := testRouter(&stubService{updateErr: ErrUserNotFound})

	rec := do(t, h, http.MethodPut, "/"+caller.String(), `{"name":"x"}`, tokenFor(t, caller))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if code := errorCode(t, rec); code != "user_not_found" {
		t.Errorf("code = %q, want user_not_found", code)
	}
	if strings.Contains(rec.Body.String(), "repository.") {
		t.Errorf("internal layer name leaked to the client: %s", rec.Body.String())
	}
}

func TestDuplicateEmailReturns409(t *testing.T) {
	h := testRouter(&stubService{createErr: ErrEmailAlreadyExists})

	rec := do(t, h, http.MethodPost, "/", `{"name":"A","email":"a@b.co","password":"password123"}`, "")

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
	if code := errorCode(t, rec); code != "email_already_exists" {
		t.Errorf("code = %q, want email_already_exists", code)
	}
}

// A caller must not be able to act on another account.
func TestCrossUserMutationIsForbidden(t *testing.T) {
	caller, victim := uuid.New(), uuid.New()
	h := testRouter(&stubService{})
	token := tokenFor(t, caller)

	for _, tt := range []struct {
		method, body string
	}{
		{http.MethodPut, `{"name":"hijacked"}`},
		{http.MethodDelete, ""},
	} {
		t.Run(tt.method+" another user", func(t *testing.T) {
			rec := do(t, h, tt.method, "/"+victim.String(), tt.body, token)

			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", rec.Code)
			}
			if code := errorCode(t, rec); code != "forbidden" {
				t.Errorf("code = %q, want forbidden", code)
			}
		})

		t.Run(tt.method+" own account", func(t *testing.T) {
			rec := do(t, h, tt.method, "/"+caller.String(), tt.body, token)

			if rec.Code == http.StatusForbidden {
				t.Errorf("caller was blocked from their own account (status %d)", rec.Code)
			}
		})
	}
}

func TestProtectedRoutesRequireAuth(t *testing.T) {
	h := testRouter(&stubService{})
	id := uuid.New().String()

	for _, tt := range []struct{ method, target string }{
		{http.MethodGet, "/"},
		{http.MethodGet, "/" + id},
		{http.MethodPut, "/" + id},
		{http.MethodDelete, "/" + id},
	} {
		t.Run(tt.method+" "+tt.target, func(t *testing.T) {
			rec := do(t, h, tt.method, tt.target, "", "")

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
		})
	}
}

// Registration is deliberately public.
func TestCreateIsPublic(t *testing.T) {
	h := testRouter(&stubService{user: User{ID: uuid.New()}})

	rec := do(t, h, http.MethodPost, "/", `{"name":"A","email":"a@b.co","password":"password123"}`, "")

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc == "" {
		t.Error("201 response is missing a Location header")
	}
}

func TestMalformedInputs(t *testing.T) {
	caller := uuid.New()
	h := testRouter(&stubService{})
	token := tokenFor(t, caller)

	t.Run("non-uuid path param", func(t *testing.T) {
		rec := do(t, h, http.MethodGet, "/not-a-uuid", "", token)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
		if code := errorCode(t, rec); code != "invalid_id" {
			t.Errorf("code = %q, want invalid_id", code)
		}
	})

	t.Run("malformed JSON body", func(t *testing.T) {
		rec := do(t, h, http.MethodPut, "/"+caller.String(), `{"name":`, token)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
		if code := errorCode(t, rec); code != "invalid_request" {
			t.Errorf("code = %q, want invalid_request", code)
		}
	})
}

// An unrecognised error must never reach the client as text.
func TestUnexpectedErrorIsGeneric(t *testing.T) {
	h := testRouter(&stubService{listErr: errors.New(`boom: constraint "users_email_key"`)})

	rec := do(t, h, http.MethodGet, "/", "", tokenFor(t, uuid.New()))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if code := errorCode(t, rec); code != "internal_error" {
		t.Errorf("code = %q, want internal_error", code)
	}
	if strings.Contains(rec.Body.String(), "users_email_key") {
		t.Errorf("internal detail leaked on a 500: %s", rec.Body.String())
	}
}

// Role changes must never ride along in an ordinary profile update, or any
// member could promote themselves with PUT /users/{self}.
func TestUpdateCannotChangeRole(t *testing.T) {
	caller := uuid.New()
	svc := &stubService{user: User{ID: caller, Role: authz.RoleMember}}
	h := testRouter(svc)

	rec := do(t, h, http.MethodPut, "/"+caller.String(),
		`{"name":"x","role":"admin"}`, tokenFor(t, caller))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body User
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Role == authz.RoleAdmin {
		t.Error("a profile update promoted the caller to admin")
	}
}

func TestRoleEndpointIsAdminOnly(t *testing.T) {
	target := uuid.New()
	h := testRouter(&stubService{user: User{ID: target}})

	t.Run("member is refused", func(t *testing.T) {
		rec := do(t, h, http.MethodPatch, "/"+target.String()+"/role",
			`{"role":"admin"}`, tokenForRole(t, uuid.New(), authz.RoleMember))

		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec.Code)
		}
		if code := errorCode(t, rec); code != "forbidden" {
			t.Errorf("code = %q, want forbidden", code)
		}
	})

	t.Run("unauthenticated is refused", func(t *testing.T) {
		rec := do(t, h, http.MethodPatch, "/"+target.String()+"/role", `{"role":"admin"}`, "")

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("admin may promote", func(t *testing.T) {
		rec := do(t, h, http.MethodPatch, "/"+target.String()+"/role",
			`{"role":"admin"}`, tokenForRole(t, uuid.New(), authz.RoleAdmin))

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	})
}

// Demoting yourself could leave the system with no admin at all.
func TestAdminCannotDemoteSelf(t *testing.T) {
	admin := uuid.New()
	h := testRouter(&stubService{user: User{ID: admin}})

	rec := do(t, h, http.MethodPatch, "/"+admin.String()+"/role",
		`{"role":"member"}`, tokenForRole(t, admin, authz.RoleAdmin))

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

// An admin is not restricted to their own account.
func TestAdminMayActOnAnyAccount(t *testing.T) {
	admin, victim := uuid.New(), uuid.New()
	h := testRouter(&stubService{user: User{ID: victim}})
	token := tokenForRole(t, admin, authz.RoleAdmin)

	if rec := do(t, h, http.MethodPut, "/"+victim.String(), `{"name":"x"}`, token); rec.Code != http.StatusOK {
		t.Errorf("admin PUT on another account: status = %d, want 200", rec.Code)
	}
	if rec := do(t, h, http.MethodDelete, "/"+victim.String(), "", token); rec.Code != http.StatusNoContent {
		t.Errorf("admin DELETE on another account: status = %d, want 204", rec.Code)
	}
}

func TestListPagination(t *testing.T) {
	h := testRouter(&stubService{users: []User{}})
	token := tokenFor(t, uuid.New())

	t.Run("non-numeric limit is rejected", func(t *testing.T) {
		rec := do(t, h, http.MethodGet, "/?limit=lots", "", token)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("valid pagination is accepted", func(t *testing.T) {
		if rec := do(t, h, http.MethodGet, "/?limit=10&offset=5", "", token); rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})
}
