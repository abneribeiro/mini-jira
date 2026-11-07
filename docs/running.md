# **running and configuration**

## **quickstart**

Run `make help` for every available target.

### **option a: everything in docker**

```bash
make docker-up            # docker compose up -d --build
make smoke                # optional: run the end-to-end checks
```

Compose starts Postgres, runs the migrations in a one-shot container, starts the API once the schema is ready, then Prometheus and Grafana. The API is at `http://localhost:8080`.

```bash
make docker-logs          # follow the API logs
make docker-down          # stop, keep data
make docker-reset         # stop, delete volumes, start fresh
```

### **option b: api on the host, postgres in docker**

```bash
cp .env.example .env      # set JWT_SECRET to at least 32 characters
make db-up                # start only Postgres
make migrate-up
make run                  # or `make dev` for hot reload with air
```

`make build` produces `bin/api` and `bin/migrate`. Use `make migrate-status` and `make migrate-down` to inspect or roll back the schema.

### **ports**

| Port | Service | Published to the host |
|---|---|---|
| 8080 | Public API | yes |
| 9091 | API metrics listener (`/metrics`, `/health`, `/livez`) | no, compose network only |
| 5432 | PostgreSQL | yes |
| 9090 | Prometheus | yes |
| 3000 | Grafana (admin / admin) | yes |

To read the raw exposition by hand:

```bash
docker compose exec prometheus wget -qO- http://api:9091/metrics
```

## **configuration**

All configuration is read from the environment. A `.env` file in the working directory is loaded if present; see [.env.example](../.env.example).

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Public API port |
| `METRICS_PORT` | `9091` | Private metrics listener port |
| `APP_ENV` | `local` | `local` logs as text; any other value logs JSON |
| `DATABASE_URL` | required | PostgreSQL connection string |
| `JWT_SECRET` | required | HS256 key, at least 32 characters (`openssl rand -hex 32`) |
| `TRUST_PROXY_HEADERS` | `false` | Take the client IP from `X-Forwarded-For` / `X-Real-IP`. Enable only behind a proxy that overwrites them |
| `TEST_DATABASE_URL` | unset | Enables repository integration tests. Use a dedicated database |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD` | unset | Optional admin credentials for `scripts/smoke.sh` |

## **permissions and the first admin**

There are two roles, `member` (the default) and `admin`.

| Action | Member | Admin |
|---|---|---|
| Read the user list and any user profile | yes | yes |
| Update or delete an account | own only (`403` otherwise) | any |
| Change a role | no (`403`) | any user except demoting themselves |
| Projects and their tasks | own only (`404` otherwise) | all |

**Why 404 for projects and tasks but 403 for users?** A project or task ID that belongs to someone else answers exactly like one that does not exist, so ownership cannot be probed. User IDs are already listed by `GET /users`, so hiding them would gain nothing; `403` is the honest answer there.

Registration and profile updates never accept a role, so the first admin has to be created outside the API:

```bash
make promote-admin EMAIL=you@example.com
```

This runs a parameterised `UPDATE users SET role = 'admin'` inside the Postgres container. The user has to log in again (or refresh) to receive a token carrying the new role. From then on, that admin can promote others with `PATCH /users/{id}/role`.
