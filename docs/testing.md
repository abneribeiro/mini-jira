# **testing**

| command | what it runs |
|---|---|
| `make test` | unit tests: services with fake repositories, handlers through `httptest`, pure sql builders, jwt, httpx, config, server routing. no docker needed |
| `make test-race` | the same with the race detector (needs cgo and a c compiler; without one, run `docker run --rm -v "$PWD":/src -w /src golang:1.27 make test-race`, since the alpine image has no gcc) |
| `make test-integration` | starts postgres, creates and migrates the `mini_jira_test` database, and runs every test with `TEST_DATABASE_URL` set, including the repository tests against real sql. packages run one at a time (`-p 1`) because they share the database |
| `make test-all` | `vet` + `test-race` + `test-integration` |
| `make coverage` | integration-enabled run producing `coverage.out` and `coverage.html` |
| `make lint` | `go vet`, `gofmt -s` check, and golangci-lint when installed |
| `make smoke` | `scripts/smoke.sh` against a running api |

integration tests skip, rather than fail, when `TEST_DATABASE_URL` is unset. they seed their own users and clean up via cascades, but they still write rows, so point them at a dedicated database.

## **smoke test**

`scripts/smoke.sh` is an end-to-end check of the running api. it prints `[PASS]`, `[FAIL]`, `[SKIP]` and `[INFO]` lines and exits non-zero on any failure. it covers:

-   health and liveness, `/metrics` isolation, and the 404/405 envelopes
-   registration, login, refresh rotation, reuse detection and logout
-   password-change revocation
-   the auth middleware
-   users, projects and tasks with all their boundary cases and cross-user isolation
-   the `assignee_id` tri-state, task moves, filters and cascades
-   the admin flow
-   rate limiting with `Retry-After`

```bash
make smoke
BASE_URL=http://staging:8080 ./scripts/smoke.sh
ADMIN_EMAIL=you@example.com ADMIN_PASSWORD=... ./scripts/smoke.sh
```

without `ADMIN_EMAIL`, the script registers a temporary user, promotes it through `docker exec mini-jira-postgres psql`, runs the admin checks, and deletes the user. `AUTO_ADMIN=0` disables this; `AUTO_ADMIN=1` makes it mandatory. it needs `curl` and either `jq` or `python3`. a login that gets throttled is retried after `Retry-After`, so the script can be run back to back.
