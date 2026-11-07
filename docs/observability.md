# **observability**

Logs are structured `slog` records (text locally, JSON otherwise). Each request line carries the method, route pattern, status, duration and request ID.

Prometheus scrapes `api:9091` every 15 seconds. Exported metrics:

| Metric | Type | Labels |
|---|---|---|
| `http_requests_total` | counter | `method`, `endpoint` (route pattern), `status` |
| `http_request_duration_seconds` | histogram | `method`, `endpoint` |
| `db_query_duration_seconds` | histogram | `operation`, `table` |
| `db_connections_active` | gauge | |
| `users_total`, `projects_total`, `tasks_total` | gauge | |
| `tasks_by_status` | gauge | `status` |
| `login_attempts_total` | counter | `result`: `success`, `bad_password`, `unknown_user`, `error`, `refresh`, `bad_refresh`, `refresh_reuse`, `expired_refresh` |

Plus the standard Go runtime and process collectors. Endpoints are labelled with the chi route pattern (`/tasks/{id}`), never the raw path, so cardinality stays bounded. Gauges are refreshed by a background collector every 30 seconds.

Grafana (`http://localhost:3000`, admin / admin) ships with the provisioned dashboard **Mini Jira - API**, which has these panels:

- request rate by route
- 5xx error rate
- p95 latency by route
- p95 database query time by table and operation
- entity counts
- tasks by status
- active database connections
- login attempts by result
- rate-limited (429) requests
