#!/usr/bin/env bash
set -Eeuo pipefail

KUBECTL_BIN="${KUBECTL_BIN:-kubectl}"
KUBE_NAMESPACE="${KUBE_NAMESPACE:-task-manager-staging}"
ROLLBACK_DRILL_IMAGE="${ROLLBACK_DRILL_IMAGE:?ROLLBACK_DRILL_IMAGE is required}"
ROLLBACK_DRILL_EXPECTED_COMMIT="${ROLLBACK_DRILL_EXPECTED_COMMIT:-}"
ROLLBACK_DRILL_BASE_URL="${ROLLBACK_DRILL_BASE_URL:-}"
ALLOW_PRODUCTION_ROLLBACK_DRILL="${ALLOW_PRODUCTION_ROLLBACK_DRILL:-false}"

if [ "$KUBE_NAMESPACE" = "task-manager" ] && [ "$ALLOW_PRODUCTION_ROLLBACK_DRILL" != "true" ]; then
  echo "Refusing rollback drill in production namespace task-manager." >&2
  echo "Use a staging namespace, or explicitly set ALLOW_PRODUCTION_ROLLBACK_DRILL=true after change approval." >&2
  exit 1
fi

original_image="$("$KUBECTL_BIN" -n "$KUBE_NAMESPACE" get deployment task-api -o jsonpath='{.spec.template.spec.containers[?(@.name=="api")].image}')"
if [ -z "$original_image" ]; then
  echo "Could not determine current task-api image." >&2
  exit 1
fi
if [ "$original_image" = "$ROLLBACK_DRILL_IMAGE" ]; then
  echo "ROLLBACK_DRILL_IMAGE must differ from the currently deployed image." >&2
  exit 1
fi

restored=false
restore_original() {
  local exit_code=$?
  trap - EXIT ERR
  set +e

  if [ "$restored" != "true" ]; then
    echo "Restoring original image: $original_image"
    "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" set image deployment/task-api "api=$original_image"
    "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" rollout status deployment/task-api --timeout=5m
  fi

  exit "$exit_code"
}
trap restore_original EXIT ERR

verify_version() {
  local expected="$1"
  if [ -z "$ROLLBACK_DRILL_BASE_URL" ] || [ -z "$expected" ]; then
    return 0
  fi

  local output
  output="$(mktemp)"
  for attempt in $(seq 1 30); do
    if curl --fail --silent --show-error --connect-timeout 5 --max-time 10       "$ROLLBACK_DRILL_BASE_URL/version" >"$output"; then
      if python3 - "$output" "$expected" <<'PY'
import json
import sys
with open(sys.argv[1], "r", encoding="utf-8") as handle:
    actual = json.load(handle)["data"]["commit"]
raise SystemExit(0 if actual == sys.argv[2] else 1)
PY
      then
        rm -f "$output"
        return 0
      fi
    fi
    sleep 2
  done

  cat "$output" >&2 || true
  rm -f "$output"
  return 1
}

echo "Switching staging deployment to rollback-drill image."
"$KUBECTL_BIN" -n "$KUBE_NAMESPACE" set image deployment/task-api "api=$ROLLBACK_DRILL_IMAGE"
"$KUBECTL_BIN" -n "$KUBE_NAMESPACE" rollout status deployment/task-api --timeout=5m
verify_version "$ROLLBACK_DRILL_EXPECTED_COMMIT"

echo "Restoring original image."
"$KUBECTL_BIN" -n "$KUBE_NAMESPACE" set image deployment/task-api "api=$original_image"
"$KUBECTL_BIN" -n "$KUBE_NAMESPACE" rollout status deployment/task-api --timeout=5m
restored=true

echo "Rollback drill PASSED; original image restored."
