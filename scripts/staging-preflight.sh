#!/usr/bin/env bash
set -euo pipefail

KUBECTL_BIN="${KUBECTL_BIN:-kubectl}"
KUBE_NAMESPACE="${KUBE_NAMESPACE:-task-manager-staging}"
PRODUCTION_NAMESPACE="${PRODUCTION_NAMESPACE:-task-manager}"
REQUIRE_DATA_ISOLATION="${REQUIRE_DATA_ISOLATION:-true}"

failures=0
pass() { printf 'PASS: %s\n' "$*"; }
warn() { printf 'WARN: %s\n' "$*" >&2; }
fail() { printf 'FAIL: %s\n' "$*" >&2; failures=$((failures + 1)); }

secret_value() {
  local namespace="$1"
  local key="$2"
  "$KUBECTL_BIN" -n "$namespace" get secret task-api-secrets -o "go-template={{index .data \"$key\"}}" 2>/dev/null || true
}

"$KUBECTL_BIN" cluster-info >/dev/null 2>&1 || fail "Kubernetes API is not reachable"
"$KUBECTL_BIN" get namespace "$KUBE_NAMESPACE" >/dev/null 2>&1 || fail "staging namespace is missing: $KUBE_NAMESPACE"
"$KUBECTL_BIN" get ingressclass nginx >/dev/null 2>&1 || fail "IngressClass nginx is missing"
"$KUBECTL_BIN" get --raw /apis/cert-manager.io/v1 >/dev/null 2>&1 || fail "cert-manager v1 API is unavailable"

if "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" get secret task-api-secrets >/dev/null 2>&1; then
  pass "staging task-api-secrets exists"
else
  fail "staging task-api-secrets is missing"
fi

for key in DATABASE_URL REDIS_URL JWT_SECRET WEBHOOK_SIGNING_KEY; do
  value="$(secret_value "$KUBE_NAMESPACE" "$key")"
  if [ -n "$value" ] && [ "$value" != "<no value>" ]; then
    pass "staging secret contains $key"
  else
    fail "staging secret is missing $key"
  fi
done

if "$KUBECTL_BIN" get namespace "$PRODUCTION_NAMESPACE" >/dev/null 2>&1; then
  for key in DATABASE_URL REDIS_URL; do
    staging_value="$(secret_value "$KUBE_NAMESPACE" "$key")"
    production_value="$(secret_value "$PRODUCTION_NAMESPACE" "$key")"
    if [ -n "$production_value" ] && [ "$staging_value" = "$production_value" ]; then
      if [ "$REQUIRE_DATA_ISOLATION" = "true" ]; then
        fail "staging $key matches production; staging data service must be isolated"
      else
        warn "staging $key matches production"
      fi
    else
      pass "staging $key is isolated from production value"
    fi
  done
else
  pass "production namespace is not present on this cluster; staging uses a separate cluster or isolated namespace context"
fi

for permission in   "create deployments.apps"   "patch deployments.apps"   "create services"   "create ingresses.networking.k8s.io"   "create jobs.batch"   "delete jobs.batch"; do
  verb="${permission%% *}"
  resource="${permission#* }"
  if "$KUBECTL_BIN" auth can-i "$verb" "$resource" -n "$KUBE_NAMESPACE" | grep -qx yes; then
    pass "RBAC allows $verb $resource"
  else
    fail "RBAC does not allow $verb $resource"
  fi
done

if [ "$failures" -gt 0 ]; then
  echo "Staging readiness FAILED with $failures issue(s)." >&2
  exit 1
fi

echo "Staging readiness PASSED."
