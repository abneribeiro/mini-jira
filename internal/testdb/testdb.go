// Package testdb wires repository tests to a real Postgres.
//
// Tests skip unless TEST_DATABASE_URL is set, so `go test ./...` still runs
// without Docker. `make test-integration` starts Postgres, creates and
// migrates the dedicated mini_jira_test database, and runs them against it.
package testdb

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
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

// SeedUser inserts a throwaway user and deletes it when the test ends, which
// cascades to everything the user owns.
func SeedUser(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := db.Exec(Context(t),
		`INSERT INTO users (id, name, email, password) VALUES ($1, 'Test', $2, 'x')`,
		id, id.String()+"@test.invalid")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(Context(t), `DELETE FROM users WHERE id = $1`, id)
	})

	return id
}
