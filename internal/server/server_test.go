package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abneribeiro/internal/auth"
	"github.com/abneribeiro/internal/jwt"
	authmiddleware "github.com/abneribeiro/internal/middleware"
	"github.com/abneribeiro/internal/testdb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testSecret = "test-secret-at-least-32-characters-long"

// unreachablePool never connects: pgxpool dials lazily, so construction
// succeeds and every query fails fast against a closed port.
func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/none?connect_timeout=1")
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

func get(t *testing.T, h http.Handler, target string, header http.Header) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range header {
		req.Header[k] = v
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
		t.Fatalf("body is not a JSON envelope (%v): %s", err, rec.Body.String())
	}

	return body.Error.Code
}

func TestLivez(t *testing.T) {
	pool := unreachablePool(t)

	for name, h := range map[string]http.Handler{
		"public":  New(Deps{DB: pool, JWTSecret: testSecret}),
		"metrics": NewMetrics(pool),
	} {
		t.Run(name, func(t *testing.T) {
			// Liveness does not depend on the database.
			if rec := get(t, h, "/livez", nil); rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", rec.Code)
			}
		})
	}
}

func TestHealthReportsUnreachableDatabase(t *testing.T) {
	h := New(Deps{DB: unreachablePool(t), JWTSecret: testSecret})

	rec := get(t, h, "/health", nil)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"database":"unreachable"`) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestHealthWithDatabase(t *testing.T) {
	h := New(Deps{DB: testdb.Pool(t), JWTSecret: testSecret})

	if rec := get(t, h, "/health", nil); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

// /metrics exposes the route inventory and runtime internals; it must only
// answer on the private listener.
func TestMetricsIsolation(t *testing.T) {
	pool := unreachablePool(t)

	t.Run("absent from the public API", func(t *testing.T) {
		rec := get(t, New(Deps{DB: pool, JWTSecret: testSecret}), "/metrics", nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		if code := errorCode(t, rec); code != "not_found" {
			t.Errorf("code = %q, want not_found", code)
		}
	})

	t.Run("served on the private listener", func(t *testing.T) {
		rec := get(t, NewMetrics(pool), "/metrics", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "go_goroutines") {
			t.Error("exposition is missing the Go runtime collector")
		}
	})
}

func TestUnmatchedRoutesUseTheEnvelope(t *testing.T) {
	h := New(Deps{DB: unreachablePool(t), JWTSecret: testSecret})

	token, err := jwt.NewManager(testSecret).GenerateToken(uuid.New(), "a@b.co", "member")
	if err != nil {
		t.Fatal(err)
	}
	authed := http.Header{"Authorization": {"Bearer " + token}}

	tests := []struct {
		name   string
		method string
		target string
		header http.Header
		status int
		code   string
	}{
		{"unknown top-level route", http.MethodGet, "/nope", nil, http.StatusNotFound, "not_found"},
		{"unknown route under a public sub-router", http.MethodGet, "/auth/nope", nil, http.StatusNotFound, "not_found"},
		{"unknown route under a protected sub-router", http.MethodGet, "/projects/x/y", authed, http.StatusNotFound, "not_found"},
		{"wrong method", http.MethodDelete, "/health", nil, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"wrong method under a sub-router", http.MethodGet, "/auth/login", nil, http.StatusMethodNotAllowed, "method_not_allowed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.target, nil)
			for k, v := range tt.header {
				req.Header[k] = v
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body.String())
			}
			if code := errorCode(t, rec); code != tt.code {
				t.Errorf("code = %q, want %q", code, tt.code)
			}
		})
	}
}

// Without a trusted proxy, X-Forwarded-For must not select the rate-limit
// bucket, or a client could rotate it to bypass the login limit.
func TestProxyHeadersOnlyTrustedWhenEnabled(t *testing.T) {
	tight := authmiddleware.RateLimitConfig{Burst: 1, Refill: time.Hour, TTL: time.Hour}

	login := func(h http.Handler, forwardedFor string) int {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{`))
		req.Header.Set("X-Forwarded-For", forwardedFor)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	build := func(trust bool) http.Handler {
		return New(Deps{
			DB:                unreachablePool(t),
			JWTSecret:         testSecret,
			Limiters:          auth.Limiters{Login: authmiddleware.NewRateLimiter(tight)},
			TrustProxyHeaders: trust,
		})
	}

	t.Run("untrusted", func(t *testing.T) {
		h := build(false)
		login(h, "10.0.0.1")
		if code := login(h, "10.0.0.2"); code != http.StatusTooManyRequests {
			t.Errorf("spoofed address got %d, want 429", code)
		}
	})

	t.Run("trusted", func(t *testing.T) {
		h := build(true)
		login(h, "10.0.0.1")
		if code := login(h, "10.0.0.2"); code == http.StatusTooManyRequests {
			t.Error("distinct forwarded clients shared one bucket")
		}
	})
}
