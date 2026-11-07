# **running and configuration**

## **quickstart**

run `make help` for every available target.

### **option a: everything in docker**

```bash
make docker-up            # docker compose up -d --build
make smoke                # optional: run the end-to-end checks
```

compose starts postgres, runs the migrations in a one-shot container, starts the api once the schema is ready, then prometheus and grafana. the api is at `http://localhost:8080`.

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

`make build` produces `bin/api` and `bin/migrate`. use `make migrate-status` and `make migrate-down` to inspect or roll back the schema.

### **ports**

| port | service | published to the host |
|---|---|---|
| 8080 | public api | yes |
| 9091 | api metrics listener (`/metrics`, `/health`, `/livez`) | no, compose network only |
| 5432 | postgresql | yes |
| 9090 | prometheus | yes |
| 3000 | grafana (admin / admin) | yes |

to read the raw exposition by hand:

```bash
docker compose exec prometheus wget -qO- http://api:9091/metrics
```

## **configuration**

all configuration is read from the environment. a `.env` file in the working directory is loaded if present; see [.env.example](../.env.example).

| variable | default | description |
|---|---|---|
| `PORT` | `8080` | public api port |
| `METRICS_PORT` | `9091` | private metrics listener port |
| `APP_ENV` | `local` | `local` logs as text; any other value logs json |
| `DATABASE_URL` | required | postgresql connection string |
| `JWT_SECRET` | required | hs256 key, at least 32 characters (`openssl rand -hex 32`) |
| `TRUST_PROXY_HEADERS` | `false` | take the client ip from `X-Forwarded-For` / `X-Real-IP`. enable only behind a proxy that overwrites them |
| `TEST_DATABASE_URL` | unset | enables repository integration tests. use a dedicated database |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD` | unset | optional admin credentials for `scripts/smoke.sh` |

## **permissions and the first admin**

there are two roles, `member` (the default) and `admin`.

| action | member | admin |
|---|---|---|
| read the user list and any user profile | yes | yes |
| update or delete an account | own only (`403` otherwise) | any |
| change a role | no (`403`) | any user except demoting themselves |
| projects and their tasks | own only (`404` otherwise) | all |

**why 404 for projects and tasks but 403 for users?** a project or task id that belongs to someone else answers exactly like one that does not exist, so ownership cannot be probed. user ids are already listed by `GET /users`, so hiding them would gain nothing; `403` is the honest answer there.

registration and profile updates never accept a role, so the first admin has to be created outside the api:

```bash
make promote-admin EMAIL=you@example.com
```

this runs a parameterised `UPDATE users SET role = 'admin'` inside the postgres container. the user has to log in again (or refresh) to receive a token carrying the new role. from then on, that admin can promote others with `PATCH /users/{id}/role`.
