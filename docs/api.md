# **api reference**

all bodies are json. ids are uuids. protected routes require `Authorization: Bearer <access token>`; the scheme is case-insensitive (rfc 6750).

## **endpoints**

| method | path | auth | success | errors |
|---|---|---|---|---|
| GET | `/health` | public | 200 | 503 database unreachable |
| GET | `/livez` | public | 200 | |
| POST | `/users` | public | 201 + `Location` | 400, 409 |
| POST | `/auth/login` | public | 200 | 400, 401, 429 |
| POST | `/auth/refresh` | public | 200 | 400, 401, 429 |
| POST | `/auth/logout` | public | 204 | 400 |
| GET | `/users` | member | 200 | 400 |
| GET | `/users/{id}` | member | 200 | 400, 404 |
| PUT | `/users/{id}` | self or admin | 200 | 400, 403, 404, 409 |
| DELETE | `/users/{id}` | self or admin | 204 | 400, 403, 404 |
| PATCH | `/users/{id}/role` | admin | 200 | 400, 403, 404 |
| GET | `/projects` | owner or admin | 200 | 400 |
| POST | `/projects` | member | 201 + `Location` | 400 |
| GET | `/projects/{id}` | owner or admin | 200 | 400, 404 |
| PUT | `/projects/{id}` | owner or admin | 200 | 400, 404 |
| DELETE | `/projects/{id}` | owner or admin | 204 | 400, 404 |
| GET | `/tasks` | project owner or admin | 200 | 400 |
| POST | `/tasks` | project owner or admin | 201 + `Location` | 400, 422 |
| GET | `/tasks/{id}` | project owner or admin | 200 | 400, 404 |
| PUT | `/tasks/{id}` | project owner or admin | 200 | 400, 404, 422 |
| DELETE | `/tasks/{id}` | project owner or admin | 204 | 400, 404 |

every protected route also returns `401` without a valid token.

## **request bodies and validation**

| resource | create body | rules |
|---|---|---|
| user | `{"name","email","password"}` | name 1 to 50 characters; plain e-mail address up to 200 characters, stored trimmed and lowercase; password 8 characters to 72 bytes (bcrypt limit). a `role` field is ignored |
| project | `{"name","description"}` | name 1 to 100 characters; description up to 2000 characters. the caller becomes the owner |
| task | `{"title","description","project_id","assignee_id"}` | title 1 to 200 characters; description up to 2000; `project_id` required and reachable by the caller; `assignee_id` optional, must be an existing user and not the all-zero uuid. status starts at `TODO` |
| role | `{"role"}` | `member` or `admin`. an admin cannot demote themselves |

## **authentication flow**

```bash
curl -s -X POST localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"alice@example.com","password":"password123"}'
```

```json
{
  "token": "<JWT, 15 minutes>",
  "refresh_token": "<opaque, 30 days>",
  "expires_in": 900,
  "user_id": "0199f0d8-...",
  "email": "alice@example.com",
  "role": "member"
}
```

-   `POST /auth/refresh` with `{"refresh_token": "..."}` returns a new pair and revokes the presented refresh token. replaying a revoked token revokes the whole family and returns `401`.
-   `POST /auth/logout` with `{"refresh_token": "..."}` revokes the token's family. an unknown token still returns `204`; an empty body returns `400`.
-   the role in the access token is read from the database on every login and refresh.

## **pagination and filters**

`GET /users`, `GET /projects` and `GET /tasks` accept `limit` (up to 200, default 50) and `offset` (0 or more). responses are plain json arrays ordered newest first. a `limit` above 200, a negative `offset` or a non-numeric value returns `400 validation_failed`; a `limit` of 0 or below falls back to the default.

`GET /tasks` also accepts these filters, combinable with each other and with pagination:

| parameter | value |
|---|---|
| `project_id` | uuid |
| `assignee_id` | uuid |
| `status` | `TODO`, `IN_PROGRESS` or `DONE` |

## **partial updates**

`PUT` updates only the fields present in the body. an empty body `{}` returns `400 no_fields_to_update`. fields that are not part of the resource are ignored, so `PUT /users/{id}` with `"role": "admin"` changes nothing.

`assignee_id` on `PUT /tasks/{id}` has three states:

| body | effect |
|---|---|
| field absent | assignee unchanged |
| `"assignee_id": null` | assignee cleared |
| `"assignee_id": "<uuid>"` | task reassigned |

`PUT /tasks/{id}` also accepts `project_id` to move a task. both the current and the destination project must be reachable by the caller; otherwise the answer is `404 task_not_found`.

cascades: deleting a project deletes its tasks; deleting a user deletes their projects (and those projects' tasks) and refresh tokens, and clears them as assignee on other tasks.

## **errors**

every error uses one envelope. `code` is stable and meant for programs; `message` is for humans and never contains sql or internal details.

```json
{ "error": { "code": "project_not_found", "message": "project not found" } }
```

| code | status | meaning |
|---|---|---|
| `invalid_request` | 400 | body is not exactly one valid json value (trailing data is rejected), or a required auth field is missing |
| `invalid_id` | 400 | path id is not a uuid |
| `validation_failed` | 400 | a field or query parameter breaks a rule; the message says which |
| `no_fields_to_update` | 400 | `PUT` with no recognised fields |
| `unauthorized` | 401 | missing, malformed, expired or invalid access token |
| `invalid_credentials` | 401 | wrong e-mail or password (indistinguishable on purpose) |
| `invalid_refresh_token` | 401 | unknown, expired, revoked or reused refresh token |
| `forbidden` | 403 | authenticated but not allowed (another account, member on an admin route, self-demotion) |
| `not_found` | 404 | no such route |
| `user_not_found` | 404 | no such user |
| `project_not_found` | 404 / 422 | 404 on a project route; 422 when a task references an unreachable project |
| `task_not_found` | 404 | no such task, or not reachable by the caller |
| `method_not_allowed` | 405 | route exists, method does not |
| `email_already_exists` | 409 | e-mail already registered |
| `assignee_not_found` | 422 | `assignee_id` references no user |
| `rate_limited` | 429 | too many attempts; honour the `Retry-After` header (seconds) |
| `internal_error` | 500 | unexpected failure; details are logged server-side with the request id |
