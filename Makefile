# mini-jira developer tasks. Run `make` or `make help` for the list.

SHELL := /bin/bash
.DEFAULT_GOAL := help

BIN          ?= bin
VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS      := -s -w -X main.version=$(VERSION)

PG_CONTAINER ?= mini-jira-postgres
PG_USER      ?= postgres
PG_DB        ?= mini_jira
PG_TEST_DB   ?= mini_jira_test
DB_URL       ?= postgres://postgres:postgres@localhost:5432/$(PG_DB)?sslmode=disable
TEST_DB_URL  ?= postgres://postgres:postgres@localhost:5432/$(PG_TEST_DB)?sslmode=disable

COMPOSE      ?= docker compose
PSQL         := docker exec -i $(PG_CONTAINER) psql -v ON_ERROR_STOP=1 -U $(PG_USER)

.PHONY: help build run dev migrate-up migrate-down migrate-status \
        test test-race test-integration test-all coverage fmt vet lint \
        docker-up docker-down docker-reset docker-logs db-up db-psql \
        promote-admin smoke clean

help: ## Show the available targets
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make <target> [VAR=value]\n\nTargets:\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# ------------------------------------------------------------ build / run ---

build: ## Compile bin/api and bin/migrate
	@mkdir -p $(BIN)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/api ./cmd/api
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN)/migrate ./cmd/migrate

run: ## Run the API locally (reads .env)
	go run ./cmd/api

dev: ## Run the API with hot reload (requires air)
	@command -v air >/dev/null || { echo "air not found: go install github.com/air-verse/air@latest"; exit 1; }
	air

# ------------------------------------------------------------- migrations ---

migrate-up: ## Apply pending migrations to DATABASE_URL (.env)
	go run ./cmd/migrate up

migrate-down: ## Roll back the most recent migration
	go run ./cmd/migrate down

migrate-status: ## Show applied and pending migrations
	go run ./cmd/migrate status

# ------------------------------------------------------------------ tests ---

test: ## Run unit tests (integration tests skip without TEST_DATABASE_URL)
	TEST_DATABASE_URL= go test ./...

test-race: ## Run unit tests with the race detector (needs cgo and a C compiler)
	@command -v $${CC:-gcc} >/dev/null || command -v cc >/dev/null || { echo "the race detector needs a C compiler (gcc or clang) for cgo"; exit 1; }
	CGO_ENABLED=1 TEST_DATABASE_URL= go test -race ./...

test-integration: db-up ## Run all tests against a dedicated, migrated Postgres database
	@$(PSQL) -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$(PG_TEST_DB)'" | grep -q 1 \
		|| $(PSQL) -d postgres -c "CREATE DATABASE $(PG_TEST_DB)"
	DATABASE_URL="$(TEST_DB_URL)" go run ./cmd/migrate up
	@# -p 1: packages share one database, and global queries such as the user
	@# list would otherwise see rows inserted concurrently by other packages.
	TEST_DATABASE_URL="$(TEST_DB_URL)" go test -p 1 -count=1 ./...

test-all: vet test-race test-integration ## Run vet, race-enabled unit tests and integration tests

coverage: db-up ## Write coverage.out and coverage.html (includes integration tests)
	TEST_DATABASE_URL="$(TEST_DB_URL)" go test -p 1 -count=1 -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@go tool cover -func=coverage.out | tail -1

fmt: ## Format all Go sources
	gofmt -s -w .

vet: ## Run go vet and fail on files gofmt -s would change
	go vet ./...
	@unformatted=$$(gofmt -s -l .); if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi

lint: vet ## Run golangci-lint when installed (vet always runs)
	@if command -v golangci-lint >/dev/null; then golangci-lint run ./...; else echo "golangci-lint not installed, ran go vet only"; fi

# ----------------------------------------------------------------- docker ---

docker-up: ## Build and start the full stack in the background
	$(COMPOSE) up -d --build

docker-down: ## Stop the stack (volumes are kept)
	$(COMPOSE) down

docker-reset: ## Destroy the stack and its volumes, then start fresh
	$(COMPOSE) down -v
	$(COMPOSE) up -d --build

docker-logs: ## Follow the API logs
	$(COMPOSE) logs -f api

db-up: ## Start only Postgres and wait until it is healthy
	$(COMPOSE) up -d --wait postgres

db-psql: ## Open a psql session in the Postgres container
	docker exec -it $(PG_CONTAINER) psql -U $(PG_USER) -d $(PG_DB)

promote-admin: ## Promote a registered user to admin: make promote-admin EMAIL=you@example.com
	@test -n "$(EMAIL)" || { echo "usage: make promote-admin EMAIL=you@example.com"; exit 1; }
	@# psql only interpolates :'email' (safely quoted) in SQL read from stdin, not in -c.
	@out=$$(echo "UPDATE users SET role = 'admin', updated_at = NOW() WHERE email = lower(trim(:'email')) RETURNING id, email;" \
		| $(PSQL) -d $(PG_DB) -qtA -v email="$(EMAIL)"); \
	if [ -z "$$out" ]; then echo "no user registered with email $(EMAIL)"; exit 1; fi; \
	echo "promoted to admin: $$out"; \
	echo "the user must log in again (or refresh) to receive a token with the admin role"

# ------------------------------------------------------------------ smoke ---

smoke: ## Run the end-to-end smoke test against BASE_URL (default http://localhost:8080)
	./scripts/smoke.sh

clean: ## Remove build output, coverage files and the air tmp dir
	rm -rf $(BIN) tmp coverage.out coverage.html
