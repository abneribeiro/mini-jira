# **mini-jira: design document**

this document records why mini-jira is built the way it is. for running, using and testing the service, see [README.md](README.md).

## **goal**

a small system to manage projects and tasks, used to apply a disciplined structure to a go rest api: clear layering, explicit sql, authorization enforced by the database query, and production concerns (sessions, rate limiting, metrics, graceful shutdown) handled properly rather than deferred.

## **stack choices**

-   **chi on top of `net/http`.** chi stays close to the standard library: handlers are `http.Handler`, middleware is `func(http.Handler) http.Handler`, and request data travels in `context`. nothing hides the fundamentals.
-   **pgx/v5 native api, no orm.** the project targets postgresql only, so the native pgx api is preferred over `database/sql`. sql is written by hand, parameterised, and lives only in repositories.
-   **goose for migrations.** versioned sql files, embedded in the binary with `go:embed`, applied by a separate `cmd/migrate` command (`up`, `down`, `status`, `version`).
-   **golang-jwt/v5 and bcrypt** for authentication.
-   **log/slog** for structured logging, **prometheus client** for metrics.

## **layering**

```text
HTTP -> Handler -> Service -> Repository -> PostgreSQL
```

| layer | owns | must not know about |
|---|---|---|
| handler | decoding the request, path and query parsing, mapping domain errors to status codes and the error envelope | sql |
| service | business rules, validation, normalisation, coordinating repositories | `net/http` |
| repository | all sql, including the authorization predicate | http |
| postgresql | structural integrity: pk, fk, unique, check, not null, indexes | |

repositories return domain errors (`ErrTaskNotFound`, `ErrEmailAlreadyExists`, ...) wrapped with `%w`, never strings that a higher layer has to parse. postgresql error codes are interpreted in exactly one place, `internal/database` (`UniqueViolation` for `23505`, `ForeignKeyViolation` for `23503`, both matched on the constraint name).

each domain package keeps its model in a file named after the domain (`user.go`, `project.go`, `task.go`) next to `repository.go`, `service.go` and `handler.go`. `Routes(...)` builds the production stack; an unexported `routes(...)` takes the handler so tests can mount the same routing over a stub service.

handler plumbing that is identical everywhere is shared: `httpx.URLParamUUID` parses path ids, `httpx.ParsePagination` reads `limit` and `offset`, and `middleware.RequireScope` returns the authenticated caller. paging policy is deliberately not shared: each domain keeps its own `DefaultListLimit`, `MaxListLimit` and range checks in its service, so one domain can change its limits without touching the others.

### **dependencies between domains**

`auth` depends on `user` (it reads users to log them in). the reverse dependency is needed once: a password change must revoke the user's refresh tokens. instead of importing `auth`, the `user` package declares the interface it needs:

```go
type SessionRevoker interface {
    RevokeAllForUser(ctx context.Context, userID uuid.UUID) error
}
```

`internal/server` wires `auth.NewRefreshRepository(db)` into `user.Routes`. the graph stays acyclic and each package states only what it consumes.

## **data model**

### **user**

```text
id          UUID PRIMARY KEY
name        TEXT NOT NULL
email       TEXT NOT NULL UNIQUE      stored trimmed and lowercase
password    TEXT NOT NULL             bcrypt hash, never serialised
role        TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('member', 'admin'))
created_at  TIMESTAMPTZ
updated_at  TIMESTAMPTZ
```

### **project**

```text
id          UUID PRIMARY KEY
owner_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE
name        TEXT NOT NULL
description TEXT NOT NULL DEFAULT ''
created_at, updated_at
```

### **task**

```text
id          UUID PRIMARY KEY
title       TEXT NOT NULL
description TEXT NOT NULL DEFAULT ''
status      TEXT NOT NULL DEFAULT 'TODO' CHECK (status IN ('TODO', 'IN_PROGRESS', 'DONE'))
project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE
assignee_id UUID NULL     REFERENCES users(id)    ON DELETE SET NULL
created_at, updated_at
```

### **refresh token**

```text
id          UUID PRIMARY KEY
user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE
family_id   UUID NOT NULL           one rotation chain
token_hash  BYTEA NOT NULL UNIQUE   SHA-256 of the opaque token
expires_at  TIMESTAMPTZ NOT NULL
revoked_at  TIMESTAMPTZ NULL
created_at  TIMESTAMPTZ
```

### **relationships**

-   a project belongs to one user (`owner_id`). this column is what all authorization keys on.
-   a task belongs to one project and has no owner of its own: it inherits the project's owner.
-   a task may have one assignee. assignment does not grant access.

### **identifiers**

the first iteration used `BIGSERIAL`. the project moved to uuid v7 generated in the service layer: ids are not enumerable, the id is known before the insert, and v7's time ordering keeps b-tree inserts mostly append-only.

### **optional and tri-state fields**

`Task.AssigneeID` is `*uuid.UUID` because the column is nullable. in `UpdateInput` it is a `NullableUUID{Set, Value}`, whose `UnmarshalJSON` marks the field as present even for `null`. a plain pointer collapses "absent" and `null` into `nil`, which would make it impossible to clear an assignee with a partial update.

## **migrations**

```text
internal/migrations/
  00001_create_users.sql
  00002_create_projects.sql
  00003_create_tasks.sql                 FKs with CASCADE / SET NULL, status CHECK
  00004_fix_nullable_description.sql     description NOT NULL DEFAULT '', index on tasks.status
  00005_add_project_owner.sql            projects.owner_id, the basis of authorization
  00006_add_user_roles.sql               users.role with CHECK
  00007_create_refresh_tokens.sql        revocable sessions
  00008_list_indexes.sql                 composite indexes for paginated lists
```

principles:

-   every schema change is a small, coherent, versioned migration with a working `Down`.
-   migrations run separately from the api (`cmd/migrate`, a one-shot compose service the api waits on). the api never changes the schema at startup.
-   indexes follow the queries. list endpoints filter by owner or project and page with `ORDER BY created_at DESC LIMIT ... OFFSET ...`, so migration 00008 replaces the single-column `owner_id` and `project_id` indexes with `(owner_id, created_at DESC)` and `(project_id, created_at DESC)`, and adds `users(created_at DESC)`.

## **authentication**

### **tokens**

login returns two credentials:

| token | format | lifetime | revocable |
|---|---|---|---|
| access token | hs256 jwt with `user_id`, `email`, `role`, `sub`, `exp`, `iat` | 15 minutes | no, it is stateless |
| refresh token | 32 random bytes, base64url | 30 days | yes, it is stored server-side as a sha-256 hash |

the access token is short-lived precisely because it cannot be revoked. the refresh token is the revocable credential:

-   **rotation.** `POST /auth/refresh` revokes the presented token and issues a new one in the same `family_id`. the user is re-read from the database, so a role change takes effect on the next refresh.
-   **reuse detection.** a revoked token presented again means two parties hold it: the legitimate client already moved on to the next one. the whole family is revoked, which also kills the attacker's or the victim's current token and forces a fresh login.
-   **logout** revokes the family, so the session cannot resume from an older link.
-   **password change** revokes every refresh token of the user (all families).
-   **storage.** only the sha-256 hash is persisted. a database leak yields no usable refresh tokens. sha-256 rather than bcrypt is correct here: the input is 256 bits of entropy, so there is nothing to brute-force, and the lookup must be an indexed equality.
-   **pruning.** a background worker deletes expired rows hourly.

### **login hardening**

-   e-mail is normalised (trim, lowercase) before lookup, matching how it was stored.
-   an unknown e-mail still pays for one bcrypt comparison against a fixed dummy hash, so timing does not reveal which addresses are registered. both failures return the same `401 invalid_credentials`.
-   only `user.ErrUserNotFound` counts as a credential failure. any other repository error is a `500`, logged in full, so a database outage is not silently reported as wrong passwords.
-   the jwt validator pins the algorithm to hs256, which rejects `none` and algorithm-confusion tokens, and keeps the failure reason in the server log only.
-   `JWT_SECRET` must be at least 32 characters; the process refuses to start otherwise.

### **rate limiting**

per-client token buckets in memory, with separate budgets because the protected credentials differ in guessability:

| route | burst | refill | reason |
|---|---|---|---|
| `/auth/login` | 10 | 1 every 6 s | passwords are guessable and each attempt costs a bcrypt hash |
| `/auth/refresh` | 60 | 1 per second | tokens are unguessable; legitimate clients refresh often |
| `/auth/logout` | none | | idempotent and requires a token |

the bucket key is the client ip. chi's `RealIP` middleware, which rewrites the remote address from `X-Forwarded-For` / `X-Real-IP`, is mounted only when `TRUST_PROXY_HEADERS=true`. without a proxy that overwrites those headers, trusting them would let any client choose a fresh bucket per request. idle buckets are swept periodically so the map cannot grow without bound.

## **authorization**

authentication (who you are) is separate from authorization (what you can reach).

`middleware.AuthMiddleware` validates the token and stores the user id and role in the request context. `middleware.ScopeFromContext` turns them into an `authz.Scope{UserID, IsAdmin}`, which handlers pass through services into repositories. repositories put the scope into sql:

```sql
SELECT ... FROM projects WHERE id = $1 AND (owner_id = $2 OR $3)
```

writes do the same in one statement, for example `INSERT ... SELECT ... WHERE EXISTS (SELECT 1 FROM projects WHERE id = $4 AND (owner_id = $6 OR $7))` when creating a task. there is no window between checking ownership and writing. moving a task adds an `EXISTS` on the destination project to the same `UPDATE`.

status codes are chosen deliberately:

-   a project or task owned by someone else returns **404**, identical to a missing one. a 403 would confirm that the id exists.
-   `PUT` / `DELETE /users/{id}` on another account returns **403**. user ids are already visible through `GET /users`, so there is nothing to hide.
-   a task referencing an unreachable project or a missing assignee returns **422**: the task route itself is valid, but a reference in the body is not.

### **roles**

there are two roles: `member` (the default) and `admin`. an admin bypasses ownership everywhere, through the `OR $isAdmin` predicate.

neither registration nor `PUT /users/{id}` accepts a role; `UpdateInput` has no `Role` field at all. roles change only through `PATCH /users/{id}/role`, behind `RequireAdmin`. that leaves the first admin to be created outside the api (`make promote-admin EMAIL=...`). an admin cannot demote themselves, so the system cannot lose its last admin through the api.

## **error model**

all errors go through `httpx.WriteError`, which marshals:

```json
{ "error": { "code": "task_not_found", "message": "task not found" } }
```

-   `code` is stable and machine-readable; `message` is for people.
-   each handler package has one `respondError` that maps domain errors with `errors.Is`. anything unrecognised becomes `500 internal_error` and is logged with the request path; sql, constraint names and layer names never reach the client.
-   unknown routes and wrong methods use the same envelope (`not_found`, `method_not_allowed`), registered on the root router and inherited by mounted sub-routers.
-   `httpx.WriteJSON` marshals before writing the status, so an encoding failure turns into a clean 500 instead of a 200 with a truncated body. request bodies are capped at 1 mib and must contain exactly one json value; a second value or trailing garbage is a `400 invalid_request`.

## **runtime behaviour**

-   **two listeners.** the public api on `PORT` and an operator listener on `METRICS_PORT` serving `/metrics`, `/health` and `/livez`. compose publishes only the public one.
-   **probes.** `/livez` answers whenever the process can serve. `/health` pings the database with a 2-second timeout and returns 503 when it fails. the container healthcheck runs `api healthcheck`, which gets `/livez` on loopback, because the distroless image has no shell or curl.
-   **timeouts.** read-header 5 s, read 15 s, write 30 s, idle 60 s, plus a 30-second per-request context timeout.
-   **graceful shutdown.** on sigint or sigterm both servers drain for up to 10 seconds. the background workers (metrics collector, two rate-limiter sweepers, token pruner) stop on the same context and are joined before the pool is closed.
-   **metrics.** http metrics are labelled with the chi route pattern (`/tasks/{id}`), never the raw path, so each uuid does not create a new time series.

## **principles**

```text
Handlers do not know SQL.
Services do not know HTTP.
Repositories do not know HTTP; all SQL lives there.
Authorization is a predicate in the query, not a check after the read.
The schema evolves only through migrations.
Business rules live in services; integrity rules live in the database.
```

keep responsibilities separate without adding abstractions the code does not need yet.
