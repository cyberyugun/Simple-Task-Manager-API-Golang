#!/usr/bin/env bash
set -euo pipefail

CANARY_WEIGHT="${CANARY_WEIGHT:-10}"
CANARY_OBSERVATION_SECONDS="${CANARY_OBSERVATION_SECONDS:-60}"

is_uint() {
  printf '%s' "$1" | grep -Eq '^[0-9]+$'
}

is_uint "$CANARY_WEIGHT" || {
  echo "CANARY_WEIGHT must be an integer" >&2
  exit 1
}
is_uint "$CANARY_OBSERVATION_SECONDS" || {
  echo "CANARY_OBSERVATION_SECONDS must be an integer" >&2
  exit 1
}

if [ "$CANARY_WEIGHT" -lt 1 ] || [ "$CANARY_WEIGHT" -gt 50 ]; then
  echo "CANARY_WEIGHT must be between 1 and 50" >&2
  exit 1
fi

if [ "$CANARY_OBSERVATION_SECONDS" -lt 15 ] || [ "$CANARY_OBSERVATION_SECONDS" -gt 900 ]; then
  echo "CANARY_OBSERVATION_SECONDS must be between 15 and 900" >&2
  exit 1
fi

grep -q 'nginx.ingress.kubernetes.io/canary: "true"' deploy/k8s/progressive/canary-ingress.yaml
grep -q 'nginx.ingress.kubernetes.io/canary-by-header: "X-Canary"' deploy/k8s/progressive/canary-ingress.yaml
grep -q 'name: task-api-canary' deploy/k8s/progressive/canary-deployment.yaml
grep -q 'name: task-api-canary' deploy/k8s/progressive/canary-service.yaml
grep -q 'maxUnavailable: 0' deploy/k8s/base/deployment.yaml

python3 scripts/check-migration-compatibility.py migrations

echo "Progressive release policy PASSED: canary_weight=$CANARY_WEIGHT observation_seconds=$CANARY_OBSERVATION_SECONDS"
