#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
COMPOSE_BIN="${COMPOSE_BIN:-docker compose}"

status_code() {
  curl --silent --output /dev/null --write-out '%{http_code}' --max-time 5 "$BASE_URL$1" || true
}

wait_status() {
  local path="$1"
  local expected="$2"
  local attempts="${3:-30}"

  for _ in $(seq 1 "$attempts"); do
    actual="$(status_code "$path")"
    if [ "$actual" = "$expected" ]; then
      return 0
    fi
    sleep 2
  done

  echo "Expected $path to return $expected, got $(status_code "$path")" >&2
  return 1
}

assert_liveness() {
  test "$(status_code /health)" = "200" || {
    echo "API liveness failed during dependency outage." >&2
    return 1
  }
}

exercise_dependency() {
  local service="$1"

  echo "Stopping dependency: $service"
  $COMPOSE_BIN stop "$service"

  wait_status /ready 503 20
  assert_liveness

  echo "Recovering dependency: $service"
  $COMPOSE_BIN start "$service"

  wait_status /ready 200 30
  assert_liveness
}

wait_status /ready 200 30
assert_liveness

exercise_dependency redis
exercise_dependency postgres

$COMPOSE_BIN exec -T postgres psql   -U task_user   -d task_manager   -v ON_ERROR_STOP=1   -c 'SELECT 1;' >/dev/null

echo "Dependency resilience exercise PASSED."
