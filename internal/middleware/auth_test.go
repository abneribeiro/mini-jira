package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/abneribeiro/internal/authz"
	"github.com/google/uuid"
)

func TestRequireScope(t *testing.T) {
	t.Run("authenticated", func(t *testing.T) {
		id := uuid.New()
		ctx := context.WithValue(context.Background(), UserIDKey, id)
		ctx = context.WithValue(ctx, UserRoleKey, authz.RoleAdmin)
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		rec := httptest.NewRecorder()

		scope, ok := RequireScope(rec, req)
		if !ok {
			t.Fatal("an authenticated request was rejected")
		}
		if scope != (authz.Scope{UserID: id, IsAdmin: true}) {
			t.Errorf("scope = %+v, want admin %v", scope, id)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("success wrote a response: %s", rec.Body.String())
		}
	})

	t.Run("missing identity", func(t *testing.T) {
		rec := httptest.NewRecorder()

		if _, ok := RequireScope(rec, httptest.NewRequest(http.MethodGet, "/", nil)); ok {
			t.Fatal("a request without identity was accepted")
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})
}
