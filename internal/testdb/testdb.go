// Package testdb wires repository tests to a real Postgres.
//
// Tests skip unless TEST_DATABASE_URL is set, so `go test ./...` still runs
// without Docker. To run them:
//
//	docker compose up -d postgres
//	go run ./cmd/migrate
//	TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/mini_jira?sslmode=disable" go test ./...
package testdb

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EnvVar names the connection string that enables these tests.
const EnvVar = "TEST_DATABASE_URL"

// Pool returns a pool against the test database, or skips the test when the
// environment variable is absent.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv(EnvVar)
	if dsn == "" {
		t.Skipf("set %s to run repository integration tests", EnvVar)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to %s: %v", EnvVar, err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping test database: %v", err)
	}

	t.Cleanup(pool.Close)

	return pool
}

// Context returns a context bounded to the test.
func Context(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	return ctx
}
