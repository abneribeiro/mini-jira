<div align="center">
  <br />
  <h3><b>mini-jira</b></h3>
  <p>a small project and task tracker exposed as a rest api in go, built the way a production service should be.</p>
</div>

<br />

## **overview**

-   users, projects and tasks behind jwt auth, with rotating refresh tokens and an admin role
-   strict layering: handler (`net/http` + chi) → service (business rules) → repository (all sql) → postgres constraints as the last line of defence
-   structured `slog` logs, prometheus metrics on a private listener, and a provisioned grafana dashboard
-   a test suite that runs against a real postgres, plus an end-to-end smoke script
-   stack: `go 1.27`, `chi v5`, `pgx v5`, `goose v3`, `postgres 18`, `prometheus`, `grafana`, `docker compose`

## **design & trade-offs**

-   **authorization inside the query:** repositories receive an `authz.Scope` and filter with `WHERE owner_id = $n OR $isAdmin`. ownership check and write happen in one statement, so there is no read-then-decide window.
-   **explicit sql, no orm:** every query is visible, parameterised and reviewed with its indexes. partial updates build their `SET` clause in a pure, unit-tested function.
-   **rotating refresh tokens with reuse detection:** each refresh revokes the presented token and issues the next one in the same family. presenting an already rotated token means it leaked, so the whole family is revoked. only the sha-256 of a refresh token is stored.
-   **login that leaks nothing:** unknown e-mails are still compared against a dummy bcrypt hash, so response time does not reveal registered accounts. a database outage is a `500`, never a misleading `401`.
-   **known cost:** stolen access tokens stay valid for up to 15 minutes, because immediate revocation would need a per-request lookup and give up the stateless jwt. the full list is in [known limitations](docs/architecture.md#known-limitations-and-trade-offs).

## **quickstart**

everything in docker (postgres, migrations, api, prometheus, grafana):

```bash
make docker-up      # api on localhost:8080, grafana on localhost:3000
make smoke          # optional: end-to-end checks against the running api
make docker-down    # stop, keep data
```

or the api on the host, postgres in docker:

```bash
cp .env.example .env      # set JWT_SECRET to at least 32 characters
make db-up
make migrate-up
make run                  # or `make dev` for hot reload with air
```

`make help` lists every target.

## **testing**

```bash
make test               # unit tests, no docker needed
make test-race          # same, with the race detector
make test-integration   # repositories against a real, migrated postgres
make test-all           # vet + test-race + test-integration
```

-   integration tests skip, rather than fail, when `TEST_DATABASE_URL` is unset, and write rows, so point them at a dedicated database
-   `scripts/smoke.sh` covers auth, refresh reuse, cross-user isolation, the admin flow and rate limiting end to end

## **docs**

-   [architecture](docs/architecture.md): layering, package layout, engineering decisions, known limitations
-   [running and configuration](docs/running.md): docker and host setups, ports, environment variables, the first admin
-   [api reference](docs/api.md): endpoints, validation, auth flow, pagination, partial updates, errors
-   [testing](docs/testing.md): every test target and the smoke script
-   [observability](docs/observability.md): logs, exported metrics, grafana panels
-   [design document](minijira.md): data model, migrations, authentication and authorization design
