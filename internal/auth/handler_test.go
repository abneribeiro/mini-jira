package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abneribeiro/internal/httpx/httpxtest"
	authmiddleware "github.com/abneribeiro/internal/middleware"
	"github.com/abneribeiro/internal/user"
)

// testHandler mounts the real routing over a Service backed by the in-memory
// fakes from service_test.go, so the full error mapping is exercised.
func testHandler(t *testing.T, userRepo *fakeUserRepo, refresh *fakeRefreshRepo, limiters Limiters) http.Handler {
	t.Helper()

	return routes(NewHandler(NewService(userRepo, refresh, testManager())), limiters)
}

func post(t *testing.T, h http.Handler, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req := httptest.NewRequest(http.MethodPost, target, reader)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func loginResponse(t *testing.T, rec *httptest.ResponseRecorder) LoginResponse {
	t.Helper()

	var resp LoginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode login response: %v: %s", err, rec.Body.String())
	}

	return resp
}

const validLogin = `{"email":"alice@example.com","password":"password123"}`

func TestLoginHandler(t *testing.T) {
	tests := []struct {
		name     string
		repo     func(t *testing.T) *fakeUserRepo
		body     string
		status   int
		wantCode string
	}{
		{
			name:   "valid credentials",
			repo:   func(t *testing.T) *fakeUserRepo { return &fakeUserRepo{user: registeredUser(t, "password123")} },
			body:   validLogin,
			status: http.StatusOK,
		},
		{
			name:     "malformed JSON",
			repo:     func(t *testing.T) *fakeUserRepo { return &fakeUserRepo{} },
			body:     `{"email":`,
			status:   http.StatusBadRequest,
			wantCode: "invalid_request",
		},
		{
			name:     "wrong password",
			repo:     func(t *testing.T) *fakeUserRepo { return &fakeUserRepo{user: registeredUser(t, "password123")} },
			body:     `{"email":"alice@example.com","password":"wrongpassword"}`,
			status:   http.StatusUnauthorized,
			wantCode: "invalid_credentials",
		},
		{
			// Same status and code as a wrong password, so the response
			// cannot be used to enumerate accounts.
			name:     "unknown user",
			repo:     func(t *testing.T) *fakeUserRepo { return &fakeUserRepo{err: user.ErrUserNotFound} },
			body:     validLogin,
			status:   http.StatusUnauthorized,
			wantCode: "invalid_credentials",
		},
		{
			name:     "database down",
			repo:     func(t *testing.T) *fakeUserRepo { return &fakeUserRepo{err: errDatabaseDown} },
			body:     validLogin,
			status:   http.StatusInternalServerError,
			wantCode: "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := testHandler(t, tt.repo(t), newFakeRefreshRepo(), Limiters{})

			rec := post(t, h, "/login", tt.body)

			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body.String())
			}
			if tt.wantCode != "" {
				if code := httpxtest.ErrorCode(t, rec); code != tt.wantCode {
					t.Errorf("code = %q, want %q", code, tt.wantCode)
				}
				if strings.Contains(rec.Body.String(), "connection refused") {
					t.Errorf("internal error leaked to the client: %s", rec.Body.String())
				}
				return
			}

			resp := loginResponse(t, rec)
			if resp.Token == "" || resp.RefreshToken == "" {
				t.Errorf("login response is missing a token: %s", rec.Body.String())
			}
		})
	}
}

func TestLoginStoreFailureIs500(t *testing.T) {
	refresh := newFakeRefreshRepo()
	refresh.err = errDatabaseDown
	h := testHandler(t, &fakeUserRepo{user: registeredUser(t, "password123")}, refresh, Limiters{})

	rec := post(t, h, "/login", validLogin)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestRefreshHandler(t *testing.T) {
	h := testHandler(t, &fakeUserRepo{user: registeredUser(t, "password123")}, newFakeRefreshRepo(), Limiters{})

	login := loginResponse(t, post(t, h, "/login", validLogin))

	rec := post(t, h, "/refresh", `{"refresh_token":"`+login.RefreshToken+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	rotated := loginResponse(t, rec)
	if rotated.RefreshToken == login.RefreshToken {
		t.Error("refresh token was not rotated")
	}

	t.Run("replayed token", func(t *testing.T) {
		rec := post(t, h, "/refresh", `{"refresh_token":"`+login.RefreshToken+`"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if code := httpxtest.ErrorCode(t, rec); code != "invalid_refresh_token" {
			t.Errorf("code = %q, want invalid_refresh_token", code)
		}
	})

	t.Run("rotated token dies with its family", func(t *testing.T) {
		rec := post(t, h, "/refresh", `{"refresh_token":"`+rotated.RefreshToken+`"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("empty token", func(t *testing.T) {
		if rec := post(t, h, "/refresh", `{}`); rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		rec := post(t, h, "/refresh", `not json`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		if code := httpxtest.ErrorCode(t, rec); code != "invalid_request" {
			t.Errorf("code = %q, want invalid_request", code)
		}
	})
}

func TestLogoutHandler(t *testing.T) {
	refresh := newFakeRefreshRepo()
	h := testHandler(t, &fakeUserRepo{user: registeredUser(t, "password123")}, refresh, Limiters{})

	login := loginResponse(t, post(t, h, "/login", validLogin))

	if rec := post(t, h, "/logout", `{"refresh_token":"`+login.RefreshToken+`"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", rec.Code)
	}
	if refresh.liveTokens() != 0 {
		t.Errorf("%d live tokens after logout, want 0", refresh.liveTokens())
	}

	t.Run("token is dead afterwards", func(t *testing.T) {
		if rec := post(t, h, "/refresh", `{"refresh_token":"`+login.RefreshToken+`"}`); rec.Code != http.StatusUnauthorized {
			t.Errorf("refresh after logout = %d, want 401", rec.Code)
		}
	})

	t.Run("unknown token is idempotent", func(t *testing.T) {
		if rec := post(t, h, "/logout", `{"refresh_token":"never-issued"}`); rec.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204", rec.Code)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		rec := post(t, h, "/logout", `{}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		if code := httpxtest.ErrorCode(t, rec); code != "invalid_request" {
			t.Errorf("code = %q, want invalid_request", code)
		}
	})

	t.Run("no body at all", func(t *testing.T) {
		if rec := post(t, h, "/logout", ""); rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}

func TestAuthRoutesAreRateLimited(t *testing.T) {
	tight := authmiddleware.RateLimitConfig{Burst: 1, Refill: time.Hour, TTL: time.Hour}
	limiters := Limiters{
		Login:   authmiddleware.NewRateLimiter(tight),
		Refresh: authmiddleware.NewRateLimiter(tight),
	}
	h := testHandler(t, &fakeUserRepo{user: registeredUser(t, "password123")}, newFakeRefreshRepo(), limiters)

	for _, route := range []string{"/login", "/refresh"} {
		t.Run(route, func(t *testing.T) {
			post(t, h, route, `{}`)

			rec := post(t, h, route, `{}`)
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("status = %d, want 429", rec.Code)
			}
			if code := httpxtest.ErrorCode(t, rec); code != "rate_limited" {
				t.Errorf("code = %q, want rate_limited", code)
			}
			if rec.Header().Get("Retry-After") == "" {
				t.Error("429 without a Retry-After header")
			}
		})
	}

	// Logout is deliberately unthrottled: it is idempotent and needs a token.
	t.Run("/logout", func(t *testing.T) {
		for range 3 {
			if rec := post(t, h, "/logout", `{"refresh_token":"x"}`); rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204", rec.Code)
			}
		}
	})
}
