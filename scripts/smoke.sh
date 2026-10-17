#!/usr/bin/env bash
#
# mini-jira end-to-end smoke test.
#
# Exercises every public route, status code, header and boundary case of a
# running API. Every run uses a fresh random e-mail suffix, so it can be run
# repeatedly against the same database.
#
# Usage:
#
#   make smoke                       # or: ./scripts/smoke.sh
#
# Environment:
#
#   BASE_URL         API base URL                      (http://localhost:8080)
#   PROMETHEUS_URL   checks the scrape target is up    (http://localhost:9090)
#   ADMIN_EMAIL      credentials of an existing admin  (optional)
#   ADMIN_PASSWORD
#   AUTO_ADMIN       auto | 1 | 0                      (auto)
#                    Without ADMIN_EMAIL, register a temporary user and
#                    promote it through `docker exec $PG_CONTAINER psql`.
#                    auto: only when the container is reachable; 1: required.
#   PG_CONTAINER     Postgres container name           (mini-jira-postgres)
#   PG_DB            database name                     (mini_jira)
#
# Requires curl, plus jq or python3 for JSON parsing. Exits non-zero if any
# check fails.

set -uo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:9090}"
ADMIN_EMAIL="${ADMIN_EMAIL:-}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-}"
AUTO_ADMIN="${AUTO_ADMIN:-auto}"
PG_CONTAINER="${PG_CONTAINER:-mini-jira-postgres}"
PG_DB="${PG_DB:-mini_jira}"

RUN_ID="$(date +%s)$RANDOM"
PASSWORD="password123"
UNKNOWN_ID="00000000-0000-7000-8000-000000000999"
ZERO_ID="00000000-0000-0000-0000-000000000000"
MAX_429_RETRIES=4

PASSED=0
FAILED=0
SKIPPED=0

HEADERS_FILE="$(mktemp)"
trap 'rm -f "$HEADERS_FILE"' EXIT

# ------------------------------------------------------------- output -------

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
    C_PASS=$'\033[0;32m'; C_FAIL=$'\033[0;31m'; C_SKIP=$'\033[0;33m'; C_DIM=$'\033[0;36m'; C_OFF=$'\033[0m'
else
    C_PASS=""; C_FAIL=""; C_SKIP=""; C_DIM=""; C_OFF=""
fi

section() { printf '\n== %s\n' "$1"; }
info()    { printf '%s[INFO]%s %s\n' "$C_DIM" "$C_OFF" "$1"; }
pass()    { printf '%s[PASS]%s %s\n' "$C_PASS" "$C_OFF" "$1"; PASSED=$((PASSED + 1)); }
fail()    { printf '%s[FAIL]%s %s\n' "$C_FAIL" "$C_OFF" "$1"; FAILED=$((FAILED + 1)); }
skip()    { printf '%s[SKIP]%s %s\n' "$C_SKIP" "$C_OFF" "$1"; SKIPPED=$((SKIPPED + 1)); }

# --------------------------------------------------------------- json -------

if command -v jq >/dev/null 2>&1; then
    JSON_TOOL=jq
elif command -v python3 >/dev/null 2>&1; then
    JSON_TOOL=python3
else
    echo "smoke.sh needs jq or python3 to parse JSON" >&2
    exit 2
fi

# json <dotted.path> [document] -> value, empty for null or absent.
# Reads $BODY when no document is given.
json() {
    local path="$1" doc="${2-$BODY}"
    if [ "$JSON_TOOL" = jq ]; then
        printf '%s' "$doc" | jq -r ".$path // empty" 2>/dev/null
    else
        printf '%s' "$doc" | python3 -c '
import json, sys
try:
    v = json.load(sys.stdin)
    for k in sys.argv[1].split("."):
        v = v.get(k) if isinstance(v, dict) else None
    print("" if v is None else v)
except Exception:
    pass
' "$path"
    fi
}

# json_len [document] -> length of a JSON array.
json_len() {
    local doc="${1-$BODY}"
    if [ "$JSON_TOOL" = jq ]; then
        printf '%s' "$doc" | jq 'length' 2>/dev/null
    else
        printf '%s' "$doc" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)))' 2>/dev/null
    fi
}

# ----------------------------------------------------------- requests -------

# request <METHOD> <path> [body] [token] [extra curl args...]
# Sets CODE, BODY and HEADERS_FILE. A 429 is waited out (Retry-After) and
# retried, because the login bucket is shared by the whole run; only the
# rate-limit section turns this off with NO_RETRY=1.
request() {
    local method="$1" path="$2" body="${3-}" token="${4-}"
    shift 4 2>/dev/null || shift $#

    local args=(-s -X "$method" -D "$HEADERS_FILE" -w $'\n%{http_code}')
    [ -n "$body" ] && args+=(-H "Content-Type: application/json" --data-raw "$body")
    [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
    args+=("$@")

    local attempt=0 response wait
    while :; do
        response="$(curl "${args[@]}" "$BASE_URL$path")"
        CODE="${response##*$'\n'}"
        BODY="${response%$'\n'*}"

        if [ "$CODE" != "429" ] || [ "${NO_RETRY:-0}" = "1" ] || [ "$attempt" -ge "$MAX_429_RETRIES" ]; then
            return
        fi

        attempt=$((attempt + 1))
        wait="$(header Retry-After)"
        info "rate limited on $method $path, waiting ${wait:-6}s before retrying"
        sleep "${wait:-6}"
    done
}

# header <name> -> value of a response header from the last request.
header() {
    grep -i "^$1:" "$HEADERS_FILE" | head -n1 | cut -d: -f2- | tr -d '\r' | sed 's/^ *//'
}

# expect <label> <status> [error code]
# Checks the last response; with a third argument also the envelope code.
expect() {
    local label="$1" want="$2" want_code="${3-}"

    if [ "$CODE" != "$want" ]; then
        fail "$label: got $CODE, want $want"
        printf '       %s\n' "${BODY:0:300}"
        return 1
    fi

    if [ -n "$want_code" ]; then
        local got_code
        got_code="$(json error.code)"
        if [ "$got_code" != "$want_code" ]; then
            fail "$label: status $CODE but error code '$got_code', want '$want_code'"
            return 1
        fi
        pass "$label [$CODE $want_code]"
        return 0
    fi

    pass "$label [$CODE]"
}

# assert <label> <actual> <expected>
assert() {
    if [ "$2" = "$3" ]; then
        pass "$1"
    else
        fail "$1: got '$2', want '$3'"
    fi
}

# expect_location <label> <prefix> -> Location must be <prefix>/<id from body>
expect_location() {
    local location id
    location="$(header Location)"
    id="$(json id)"
    assert "$1 sets Location" "$location" "$2/$id"
}

email() { printf '%s-%s@example.com' "$1" "$RUN_ID"; }

user_body() { printf '{"name":"%s","email":"%s","password":"%s"}' "$1" "$2" "${3:-$PASSWORD}"; }

login_body() { printf '{"email":"%s","password":"%s"}' "$1" "${2:-$PASSWORD}"; }

# ----------------------------------------------------------------------------

echo "mini-jira smoke test"
echo "base url: $BASE_URL"
echo "run id:   $RUN_ID"
echo "json:     $JSON_TOOL"

if ! curl -s -o /dev/null "$BASE_URL/livez"; then
    echo "API not reachable at $BASE_URL. Start it with: make docker-up" >&2
    exit 2
fi

# ------------------------------------------------------------ 1. health ---
section "1. Health, liveness and routing"

request GET /health
expect "GET /health reports the database" 200
assert "GET /health database field" "$(json database)" "ok"

request GET /livez
expect "GET /livez" 200

request GET /metrics
expect "GET /metrics is not served on the public port" 404 not_found

request GET /does-not-exist
expect "unknown route uses the JSON envelope" 404 not_found

request DELETE /health
expect "wrong method uses the JSON envelope" 405 method_not_allowed

# api_target_health -> health of the minijira_api scrape target.
api_target_health() {
    local targets
    targets="$(curl -s --max-time 3 "$PROMETHEUS_URL/api/v1/targets?state=active")"
    if [ "$JSON_TOOL" = jq ]; then
        printf '%s' "$targets" | jq -r '.data.activeTargets[] | select(.labels.job=="minijira_api") | .health' 2>/dev/null | head -n1
    else
        printf '%s' "$targets" | python3 -c '
import json, sys
try:
    for t in json.load(sys.stdin)["data"]["activeTargets"]:
        if t["labels"].get("job") == "minijira_api":
            print(t["health"]); break
except Exception:
    pass
'
    fi
}

if curl -s -o /dev/null --max-time 3 "$PROMETHEUS_URL/-/ready"; then
    # A freshly started Prometheus reports "unknown" until its first scrape,
    # which can take one scrape interval (15s) after `make docker-reset`.
    API_HEALTH="$(api_target_health)"
    for _ in $(seq 1 30); do
        [ -n "$API_HEALTH" ] && [ "$API_HEALTH" != "unknown" ] && break
        sleep 1
        API_HEALTH="$(api_target_health)"
    done
    if [ "$API_HEALTH" = "unknown" ]; then
        skip "Prometheus has not scraped minijira_api after 30s"
    else
        assert "Prometheus scrapes the private metrics listener" "$API_HEALTH" "up"
    fi
else
    skip "Prometheus not reachable at $PROMETHEUS_URL"
fi

# ------------------------------------------------------- 2. registration ---
section "2. Registration (POST /users)"

ALICE_EMAIL="$(email alice)"
BOB_EMAIL="$(email bob)"
CAROL_EMAIL="$(email carol)"

request POST /users "$(user_body Alice "$ALICE_EMAIL")"
expect "register Alice" 201
expect_location "POST /users" /users
ALICE_ID="$(json id)"
assert "new accounts are members" "$(json role)" "member"
assert "password hash is never serialised" "$(json password)" ""

request POST /users "$(user_body "Alice Again" "$ALICE_EMAIL")"
expect "duplicate e-mail" 409 email_already_exists

request POST /users "$(user_body "Alice Upper" "$(echo "$ALICE_EMAIL" | tr '[:lower:]' '[:upper:]')")"
expect "duplicate e-mail differing only in case" 409 email_already_exists

request POST /users "$(user_body Bad not-an-email)"
expect "malformed e-mail" 400 validation_failed

request POST /users '{"name":"Bad","email":"Display Name <dn-'"$RUN_ID"'@example.com>","password":"password123"}'
expect "e-mail with a display name" 400 validation_failed

request POST /users "$(user_body Bad "$(email short)" abc)"
expect "password shorter than 8" 400 validation_failed

request POST /users "$(user_body Bad "$(email long)" "$(printf 'a%.0s' $(seq 1 73))")"
expect "password longer than 72 bytes" 400 validation_failed

request POST /users '{"name":"Bad",'
expect "malformed JSON" 400 invalid_request

request POST /users '{"name":"Sneaky","email":"'"$(email sneaky)"'","password":"password123","role":"admin"}'
expect "registration with an injected role" 201
assert "injected role is ignored" "$(json role)" "member"
SNEAKY_ID="$(json id)"

request POST /users "$(user_body Bob "$BOB_EMAIL")"
expect "register Bob" 201
BOB_ID="$(json id)"

request POST /users "$(user_body Carol "$CAROL_EMAIL")"
expect "register Carol" 201
CAROL_ID="$(json id)"

# ---------------------------------------------------------------- 3. auth ---
section "3. Authentication (/auth/login, /auth/refresh, /auth/logout)"

request POST /auth/login "$(login_body "$ALICE_EMAIL")"
expect "login with valid credentials" 200
ALICE_TOKEN="$(json token)"
ALICE_REFRESH="$(json refresh_token)"
assert "login returns the access token TTL" "$(json expires_in)" "900"
assert "login returns the role" "$(json role)" "member"

request POST /auth/login "$(login_body "  $(echo "$ALICE_EMAIL" | tr '[:lower:]' '[:upper:]')  ")"
expect "login is case-insensitive and trimmed" 200

request POST /auth/login "$(login_body "$ALICE_EMAIL" wrongpassword)"
expect "wrong password" 401 invalid_credentials

request POST /auth/login "$(login_body "nobody-$RUN_ID@example.com")"
expect "unknown e-mail (same answer as a wrong password)" 401 invalid_credentials

request POST /auth/login '{"email":'
expect "login with malformed JSON" 400 invalid_request

request POST /auth/login "$(login_body "$BOB_EMAIL")"
expect "login Bob" 200
BOB_TOKEN="$(json token)"
BOB_REFRESH="$(json refresh_token)"

request POST /auth/refresh '{"refresh_token":"'"$ALICE_REFRESH"'"}'
expect "refresh rotates the token" 200
ROTATED="$(json refresh_token)"
ALICE_TOKEN="$(json token)"
if [ -n "$ROTATED" ] && [ "$ROTATED" != "$ALICE_REFRESH" ]; then
    pass "rotated refresh token differs from the presented one"
else
    fail "refresh token was not rotated"
fi

request POST /auth/refresh '{"refresh_token":"'"$ALICE_REFRESH"'"}'
expect "replaying a rotated token" 401 invalid_refresh_token

request POST /auth/refresh '{"refresh_token":"'"$ROTATED"'"}'
expect "reuse detection revoked the whole family" 401 invalid_refresh_token

request POST /auth/refresh '{"refresh_token":"never-issued"}'
expect "unknown refresh token" 401 invalid_refresh_token

request POST /auth/login "$(login_body "$ALICE_EMAIL")"
expect "login Alice for the logout checks" 200
LOGOUT_REFRESH="$(json refresh_token)"

request POST /auth/logout '{"refresh_token":"'"$LOGOUT_REFRESH"'"}'
expect "logout" 204

request POST /auth/refresh '{"refresh_token":"'"$LOGOUT_REFRESH"'"}'
expect "refresh after logout" 401 invalid_refresh_token

request POST /auth/logout '{}'
expect "logout with an empty body" 400 invalid_request

request POST /auth/logout '{"refresh_token":"never-issued"}'
expect "logout with an unknown token is idempotent" 204

# ---------------------------------------------------------- 4. middleware ---
section "4. Auth middleware"

request GET /projects
expect "no Authorization header" 401 unauthorized

request GET /projects "" "" -H "Authorization: Basic dXNlcjpwYXNz"
expect "wrong scheme" 401 unauthorized

request GET /projects "" "" -H "Authorization: Bearer"
expect "empty token" 401 unauthorized

request GET /projects "" "not.a.token"
expect "malformed token" 401 unauthorized

request GET /projects "" "" -H "authorization: bearer $ALICE_TOKEN"
expect "lowercase bearer scheme (RFC 6750)" 200

# --------------------------------------------------------------- 5. users ---
section "5. Users"

request GET /users
expect "GET /users requires authentication" 401 unauthorized

request GET /users "" "$ALICE_TOKEN"
expect "GET /users" 200

request GET "/users?limit=2" "" "$ALICE_TOKEN"
expect "GET /users?limit=2" 200
assert "limit=2 caps the page" "$(json_len)" "2"

request GET "/users?limit=1&offset=1" "" "$ALICE_TOKEN"
expect "GET /users?limit=1&offset=1" 200

request GET "/users?limit=9999" "" "$ALICE_TOKEN"
expect "limit above the maximum" 400 validation_failed

request GET "/users?offset=-1" "" "$ALICE_TOKEN"
expect "negative offset" 400 validation_failed

request GET "/users?limit=abc" "" "$ALICE_TOKEN"
expect "non-numeric limit" 400 validation_failed

request GET "/users/$ALICE_ID" "" "$ALICE_TOKEN"
expect "GET /users/{id}" 200

request GET /users/not-a-uuid "" "$ALICE_TOKEN"
expect "malformed user ID" 400 invalid_id

request GET "/users/$UNKNOWN_ID" "" "$ALICE_TOKEN"
expect "unknown user" 404 user_not_found

request PUT "/users/$ALICE_ID" '{"name":"Alice Updated"}' "$ALICE_TOKEN"
expect "partial update of the own account" 200
assert "name was updated" "$(json name)" "Alice Updated"
assert "e-mail was preserved" "$(json email)" "$ALICE_EMAIL"

request PUT "/users/$ALICE_ID" '{}' "$ALICE_TOKEN"
expect "update with no fields" 400 no_fields_to_update

request PUT "/users/$ALICE_ID" '{"email":"garbage"}' "$ALICE_TOKEN"
expect "update with an invalid e-mail" 400 validation_failed

request PUT "/users/$ALICE_ID" '{"email":"'"$BOB_EMAIL"'"}' "$ALICE_TOKEN"
expect "update to an e-mail already in use" 409 email_already_exists

request PUT "/users/$ALICE_ID" '{"name":"Alice","role":"admin"}' "$ALICE_TOKEN"
expect "update with an injected role" 200
assert "self-promotion through PUT is ignored" "$(json role)" "member"

request PUT "/users/$BOB_ID" '{"name":"hijacked"}' "$ALICE_TOKEN"
expect "update another user's account" 403 forbidden

request PUT "/users/$BOB_ID" '{"password":"attackerpass123"}' "$ALICE_TOKEN"
expect "take over another account's password" 403 forbidden

request DELETE "/users/$BOB_ID" "" "$ALICE_TOKEN"
expect "delete another user's account" 403 forbidden

request PATCH "/users/$BOB_ID/role" '{"role":"admin"}' "$ALICE_TOKEN"
expect "member cannot change roles" 403 forbidden

# ------------------------------------------------- 6. password revocation ---
section "6. Password change revokes sessions"

request POST /auth/login "$(login_body "$CAROL_EMAIL")"
expect "login Carol" 200
CAROL_TOKEN="$(json token)"
CAROL_OLD_REFRESH="$(json refresh_token)"

request PUT "/users/$CAROL_ID" '{"password":"newpassword456"}' "$CAROL_TOKEN"
expect "change own password" 200

request POST /auth/refresh '{"refresh_token":"'"$CAROL_OLD_REFRESH"'"}'
expect "refresh token issued before the change is revoked" 401 invalid_refresh_token

request POST /auth/login "$(login_body "$CAROL_EMAIL")"
expect "old password no longer works" 401 invalid_credentials

request POST /auth/login "$(login_body "$CAROL_EMAIL" newpassword456)"
expect "new password works" 200
CAROL_TOKEN="$(json token)"

# ------------------------------------------------------------ 7. projects ---
section "7. Projects"

request POST /projects '{"name":"Alice Project","description":"private"}' "$ALICE_TOKEN"
expect "create project" 201
expect_location "POST /projects" /projects
ALICE_PROJECT="$(json id)"
assert "caller owns the project" "$(json owner_id)" "$ALICE_ID"

request POST /projects '{"name":"Alice Project 2"}' "$ALICE_TOKEN"
expect "create a second project" 201
ALICE_PROJECT_2="$(json id)"

request POST /projects '{"name":"Anon"}'
expect "create without authentication" 401 unauthorized

request POST /projects '{"name":"   "}' "$ALICE_TOKEN"
expect "blank name" 400 validation_failed

request POST /projects '{"name":"'"$(printf 'p%.0s' $(seq 1 101))"'"}' "$ALICE_TOKEN"
expect "name longer than 100 characters" 400 validation_failed

request GET "/projects/$ALICE_PROJECT" "" "$ALICE_TOKEN"
expect "owner reads the project" 200

request PUT "/projects/$ALICE_PROJECT" '{"description":"updated"}' "$ALICE_TOKEN"
expect "partial update" 200
assert "name was preserved" "$(json name)" "Alice Project"

request PUT "/projects/$ALICE_PROJECT" '{}' "$ALICE_TOKEN"
expect "update with no fields" 400 no_fields_to_update

request GET "/projects?limit=1" "" "$ALICE_TOKEN"
expect "GET /projects?limit=1" 200
assert "limit=1 caps the page" "$(json_len)" "1"

request GET "/projects?limit=1&offset=1" "" "$ALICE_TOKEN"
expect "GET /projects?limit=1&offset=1" 200
assert "offset=1 returns the other project" "$(json_len)" "1"

request GET "/projects?limit=9999" "" "$ALICE_TOKEN"
expect "limit above the maximum" 400 validation_failed

request GET "/projects?offset=-1" "" "$ALICE_TOKEN"
expect "negative offset" 400 validation_failed

request GET "/projects/$ALICE_PROJECT" "" "$BOB_TOKEN"
expect "another user cannot read it (404, not 403)" 404 project_not_found

request PUT "/projects/$ALICE_PROJECT" '{"name":"hijacked"}' "$BOB_TOKEN"
expect "another user cannot update it" 404 project_not_found

request DELETE "/projects/$ALICE_PROJECT" "" "$BOB_TOKEN"
expect "another user cannot delete it" 404 project_not_found

request GET /projects "" "$BOB_TOKEN"
expect "another user's list" 200
assert "Bob sees none of Alice's projects" "$(json_len)" "0"

# --------------------------------------------------------------- 8. tasks ---
section "8. Tasks"

request POST /tasks '{"title":"Unassigned","description":"d","project_id":"'"$ALICE_PROJECT"'"}' "$ALICE_TOKEN"
expect "create task without assignee" 201
expect_location "POST /tasks" /tasks
TASK_ID="$(json id)"
assert "new tasks start as TODO" "$(json status)" "TODO"
assert "assignee is null" "$(json assignee_id)" ""

request POST /tasks '{"title":"Assigned","project_id":"'"$ALICE_PROJECT"'","assignee_id":"'"$ALICE_ID"'"}' "$ALICE_TOKEN"
expect "create task with assignee" 201
ASSIGNED_TASK="$(json id)"
assert "assignee is stored" "$(json assignee_id)" "$ALICE_ID"

request POST /tasks '{"title":"Carol task","project_id":"'"$ALICE_PROJECT_2"'","assignee_id":"'"$CAROL_ID"'"}' "$ALICE_TOKEN"
expect "create task assigned to Carol" 201
CAROL_TASK="$(json id)"

request POST /tasks '{"title":"t","project_id":"'"$UNKNOWN_ID"'"}' "$ALICE_TOKEN"
expect "unknown project_id" 422 project_not_found

request POST /tasks '{"title":"sneak","project_id":"'"$ALICE_PROJECT"'"}' "$BOB_TOKEN"
expect "project owned by someone else" 422 project_not_found

request POST /tasks '{"title":"t","project_id":"'"$ALICE_PROJECT"'","assignee_id":"'"$UNKNOWN_ID"'"}' "$ALICE_TOKEN"
expect "unknown assignee_id" 422 assignee_not_found

request POST /tasks '{"title":"t","project_id":"'"$ALICE_PROJECT"'","assignee_id":"'"$ZERO_ID"'"}' "$ALICE_TOKEN"
expect "all-zero assignee_id" 400 validation_failed

request POST /tasks '{"title":"","project_id":"'"$ALICE_PROJECT"'"}' "$ALICE_TOKEN"
expect "empty title" 400 validation_failed

request POST /tasks '{"title":"t"}' "$ALICE_TOKEN"
expect "missing project_id" 400 validation_failed

for status in IN_PROGRESS DONE TODO; do
    request PUT "/tasks/$TASK_ID" '{"status":"'"$status"'"}' "$ALICE_TOKEN"
    expect "status transition to $status" 200
done

request PUT "/tasks/$TASK_ID" '{"status":"BLOCKED"}' "$ALICE_TOKEN"
expect "invalid status" 400 validation_failed

request PUT "/tasks/$TASK_ID" '{}' "$ALICE_TOKEN"
expect "update with no fields" 400 no_fields_to_update

request PUT "/tasks/$UNKNOWN_ID" '{"status":"DONE"}' "$ALICE_TOKEN"
expect "update an unknown task" 404 task_not_found

request GET "/tasks/$TASK_ID" "" "$BOB_TOKEN"
expect "another user cannot read the task" 404 task_not_found

request PUT "/tasks/$TASK_ID" '{"status":"DONE"}' "$BOB_TOKEN"
expect "another user cannot update the task" 404 task_not_found

request DELETE "/tasks/$TASK_ID" "" "$BOB_TOKEN"
expect "another user cannot delete the task" 404 task_not_found

section "9. assignee_id tri-state (absent / null / UUID)"

request PUT "/tasks/$ASSIGNED_TASK" '{"title":"Renamed"}' "$ALICE_TOKEN"
expect "absent assignee_id" 200
assert "absent assignee_id preserves the assignee" "$(json assignee_id)" "$ALICE_ID"

request PUT "/tasks/$ASSIGNED_TASK" '{"assignee_id":null}' "$ALICE_TOKEN"
expect "explicit null" 200
assert "null clears the assignee" "$(json assignee_id)" ""

request PUT "/tasks/$ASSIGNED_TASK" '{"assignee_id":"'"$BOB_ID"'"}' "$ALICE_TOKEN"
expect "UUID reassigns" 200
assert "assignee is Bob" "$(json assignee_id)" "$BOB_ID"

request PUT "/tasks/$ASSIGNED_TASK" '{"assignee_id":"'"$ZERO_ID"'"}' "$ALICE_TOKEN"
expect "all-zero assignee_id on update" 400 validation_failed

section "10. Moving tasks between projects"

request PUT "/tasks/$ASSIGNED_TASK" '{"project_id":"'"$ALICE_PROJECT_2"'"}' "$ALICE_TOKEN"
expect "move to another own project" 200
assert "task changed project" "$(json project_id)" "$ALICE_PROJECT_2"

request PUT "/tasks/$ASSIGNED_TASK" '{"project_id":"'"$ALICE_PROJECT"'"}' "$ALICE_TOKEN"
expect "move back" 200

request POST /projects '{"name":"Bob Project"}' "$BOB_TOKEN"
expect "Bob creates a project" 201
BOB_PROJECT="$(json id)"

request POST /tasks '{"title":"Bob task","project_id":"'"$BOB_PROJECT"'"}' "$BOB_TOKEN"
expect "Bob creates a task" 201
BOB_TASK="$(json id)"

request PUT "/tasks/$BOB_TASK" '{"project_id":"'"$ALICE_PROJECT"'"}' "$BOB_TOKEN"
expect "move into someone else's project" 404 task_not_found

request GET "/tasks/$BOB_TASK" "" "$BOB_TOKEN"
expect "Bob's task is still readable" 200
assert "task did not move" "$(json project_id)" "$BOB_PROJECT"

section "11. Task filters and pagination"

request GET /tasks "" "$ALICE_TOKEN"
expect "GET /tasks" 200
assert "Alice sees her three tasks" "$(json_len)" "3"

request GET "/tasks?project_id=$ALICE_PROJECT" "" "$ALICE_TOKEN"
expect "filter by project_id" 200
assert "two tasks in the first project" "$(json_len)" "2"

request GET "/tasks?project_id=$ALICE_PROJECT&status=TODO&assignee_id=$BOB_ID&limit=10&offset=0" "" "$ALICE_TOKEN"
expect "combined filters" 200
assert "one task matches project, status and assignee" "$(json_len)" "1"

request GET "/tasks?limit=1&offset=1" "" "$ALICE_TOKEN"
expect "limit and offset" 200
assert "limit=1 caps the page" "$(json_len)" "1"

request GET "/tasks?status=BOGUS" "" "$ALICE_TOKEN"
expect "invalid status filter" 400 validation_failed

request GET "/tasks?limit=9999" "" "$ALICE_TOKEN"
expect "limit above the maximum" 400 validation_failed

request GET "/tasks?offset=-1" "" "$ALICE_TOKEN"
expect "negative offset" 400 validation_failed

request GET "/tasks?project_id=nope" "" "$ALICE_TOKEN"
expect "malformed project_id filter" 400 validation_failed

request GET /tasks "" "$BOB_TOKEN"
expect "Bob's task list" 200
assert "Bob sees only his own task" "$(json_len)" "1"

# --------------------------------------------------------------- 12. admin ---
section "12. Admin"

ADMIN_TOKEN=""
ADMIN_ID=""
TEMP_ADMIN=0

pg_reachable() {
    command -v docker >/dev/null 2>&1 &&
        docker exec "$PG_CONTAINER" psql -U postgres -d "$PG_DB" -qtAc 'SELECT 1' >/dev/null 2>&1
}

if [ -n "$ADMIN_EMAIL" ] && [ -n "$ADMIN_PASSWORD" ]; then
    info "using the admin account $ADMIN_EMAIL"
    request POST /auth/login "$(login_body "$ADMIN_EMAIL" "$ADMIN_PASSWORD")"
    if [ "$CODE" = "200" ] && [ "$(json role)" = "admin" ]; then
        ADMIN_TOKEN="$(json token)"
        ADMIN_ID="$(json user_id)"
    else
        fail "admin login: got $CODE with role '$(json role)'"
    fi
elif [ "$AUTO_ADMIN" != "0" ] && pg_reachable; then
    info "promoting a temporary admin through the $PG_CONTAINER container"
    TEMP_ADMIN_EMAIL="$(email admin)"
    request POST /users "$(user_body "Temp Admin" "$TEMP_ADMIN_EMAIL")"
    expect "register the temporary admin" 201
    ADMIN_ID="$(json id)"
    TEMP_ADMIN=1

    # :'email' is quoted by psql; it is only interpolated in SQL read from stdin.
    echo "UPDATE users SET role = 'admin' WHERE email = :'email';" |
        docker exec -i "$PG_CONTAINER" psql -U postgres -d "$PG_DB" -qtA -v ON_ERROR_STOP=1 -v email="$TEMP_ADMIN_EMAIL" >/dev/null

    request POST /auth/login "$(login_body "$TEMP_ADMIN_EMAIL")"
    expect "login the temporary admin" 200
    assert "token carries the admin role" "$(json role)" "admin"
    ADMIN_TOKEN="$(json token)"
elif [ "$AUTO_ADMIN" = "1" ]; then
    fail "AUTO_ADMIN=1 but the $PG_CONTAINER container is not reachable"
fi

if [ -z "$ADMIN_TOKEN" ]; then
    skip "admin section: set ADMIN_EMAIL/ADMIN_PASSWORD or run with the Postgres container available"
else
    request GET "/projects/$ALICE_PROJECT" "" "$ADMIN_TOKEN"
    expect "admin reads another user's project" 200

    request GET "/tasks/$ASSIGNED_TASK" "" "$ADMIN_TOKEN"
    expect "admin reads another user's task" 200

    request PUT "/users/$ALICE_ID" '{"name":"Alice via admin"}' "$ADMIN_TOKEN"
    expect "admin updates another user's account" 200

    request PATCH "/users/$BOB_ID/role" '{"role":"admin"}' "$ADMIN_TOKEN"
    expect "admin promotes Bob" 200
    assert "Bob is admin" "$(json role)" "admin"

    request POST /auth/refresh '{"refresh_token":"'"$BOB_REFRESH"'"}'
    expect "Bob refreshes after the promotion" 200
    assert "refreshed token carries the new role" "$(json role)" "admin"

    request PATCH "/users/$BOB_ID/role" '{"role":"member"}' "$ADMIN_TOKEN"
    expect "admin demotes Bob" 200
    assert "Bob is member" "$(json role)" "member"

    request PATCH "/users/$BOB_ID/role" '{"role":"superuser"}' "$ADMIN_TOKEN"
    expect "invalid role" 400 validation_failed

    request PATCH "/users/$ADMIN_ID/role" '{"role":"member"}' "$ADMIN_TOKEN"
    expect "admin cannot demote themselves" 403 forbidden

    request DELETE "/users/$SNEAKY_ID" "" "$ADMIN_TOKEN"
    expect "admin deletes another user's account" 204
    SNEAKY_ID=""
fi

# ------------------------------------------------------------ 13. cascade ---
section "13. Deletion and cascades"

request DELETE "/users/$CAROL_ID" "" "$CAROL_TOKEN"
expect "Carol deletes her own account" 204

request GET "/tasks/$CAROL_TASK" "" "$ALICE_TOKEN"
expect "Carol's assigned task survives" 200
assert "deleting the assignee sets assignee_id to null" "$(json assignee_id)" ""

request DELETE "/tasks/$TASK_ID" "" "$ALICE_TOKEN"
expect "delete task" 204

request DELETE "/tasks/$TASK_ID" "" "$ALICE_TOKEN"
expect "delete an already deleted task" 404 task_not_found

request DELETE "/projects/$ALICE_PROJECT" "" "$ALICE_TOKEN"
expect "delete project" 204

request GET "/tasks/$ASSIGNED_TASK" "" "$ALICE_TOKEN"
expect "deleting a project deletes its tasks" 404 task_not_found

request DELETE "/users/$ALICE_ID" "" "$ALICE_TOKEN"
expect "Alice deletes her account (cascades to her projects)" 204

request DELETE "/users/$BOB_ID" "" "$BOB_TOKEN"
expect "Bob deletes his account" 204

if [ -n "$SNEAKY_ID" ]; then
    request POST /auth/login "$(login_body "$(email sneaky)")"
    request DELETE "/users/$SNEAKY_ID" "" "$(json token)"
    expect "remaining test user deletes its own account" 204
fi

if [ "$TEMP_ADMIN" = "1" ]; then
    request DELETE "/users/$ADMIN_ID" "" "$ADMIN_TOKEN"
    expect "temporary admin deletes its own account" 204
fi

# ------------------------------------------------------- 14. rate limiting ---
# Last, because it deliberately exhausts this client's login bucket.
section "14. Rate limiting (POST /auth/login)"

NO_RETRY=1
LIMITED_AFTER=""
for i in $(seq 1 25); do
    request POST /auth/login "$(login_body "nobody-$RUN_ID@example.com" wrongpassword)"
    if [ "$CODE" = "429" ]; then
        LIMITED_AFTER="$i"
        break
    fi
done
NO_RETRY=0

if [ -n "$LIMITED_AFTER" ]; then
    expect "repeated logins are throttled after $LIMITED_AFTER requests" 429 rate_limited
    RETRY_AFTER="$(header Retry-After)"
    if [[ "$RETRY_AFTER" =~ ^[0-9]+$ ]] && [ "$RETRY_AFTER" -gt 0 ]; then
        pass "429 carries Retry-After: $RETRY_AFTER"
    else
        fail "429 without a usable Retry-After header (got '$RETRY_AFTER')"
    fi
else
    fail "25 consecutive logins were never throttled"
fi

# ------------------------------------------------------------- summary ---
printf '\n== Summary\n'
printf 'passed:  %d\n' "$PASSED"
printf 'failed:  %d\n' "$FAILED"
printf 'skipped: %d\n' "$SKIPPED"

[ "$FAILED" -eq 0 ]
