#!/usr/bin/env bash
set -euo pipefail

KUBECTL_BIN="${KUBECTL_BIN:-kubectl}"
KUBE_NAMESPACE="${KUBE_NAMESPACE:-task-manager}"
REQUIRE_GHCR_PULL_SECRET="${REQUIRE_GHCR_PULL_SECRET:-false}"
REQUIRE_METRICS_SERVER="${REQUIRE_METRICS_SERVER:-true}"

failures=0

pass() { printf 'PASS: %s\n' "$*"; }
warn() { printf 'WARN: %s\n' "$*" >&2; }
fail() { printf 'FAIL: %s\n' "$*" >&2; failures=$((failures + 1)); }

check_command() {
  if command -v "$1" >/dev/null 2>&1; then
    pass "command available: $1"
  else
    fail "required command is missing: $1"
  fi
}

check_can_i() {
  local verb="$1"
  local resource="$2"
  local scope_args=()
  if [ "${3:-namespaced}" = "namespaced" ]; then
    scope_args=(-n "$KUBE_NAMESPACE")
  fi

  if "$KUBECTL_BIN" auth can-i "$verb" "$resource" "${scope_args[@]}" | grep -qx "yes"; then
    pass "RBAC allows $verb $resource"
  else
    fail "RBAC does not allow $verb $resource"
  fi
}

check_secret_key() {
  local secret="$1"
  local key="$2"
  local value

  value="$("$KUBECTL_BIN" -n "$KUBE_NAMESPACE" get secret "$secret" -o "go-template={{index .data \"$key\"}}" 2>/dev/null || true)"
  if [ -n "$value" ] && [ "$value" != "<no value>" ]; then
    pass "$secret contains $key"
  else
    fail "$secret is missing non-empty key $key"
  fi
}

printf 'Production readiness preflight\n'
printf 'Namespace: %s\n' "$KUBE_NAMESPACE"

check_command "$KUBECTL_BIN"

if ! command -v "$KUBECTL_BIN" >/dev/null 2>&1; then
  exit 1
fi

if "$KUBECTL_BIN" cluster-info >/dev/null 2>&1; then
  pass "Kubernetes API is reachable"
else
  fail "Kubernetes API is not reachable"
fi

if "$KUBECTL_BIN" get namespace "$KUBE_NAMESPACE" >/dev/null 2>&1; then
  pass "namespace exists: $KUBE_NAMESPACE"
else
  fail "namespace does not exist: $KUBE_NAMESPACE"
fi

cert_manager_resources="$("$KUBECTL_BIN" api-resources --api-group=cert-manager.io -o name 2>/dev/null || true)"
for resource in clusterissuers.cert-manager.io certificates.cert-manager.io certificaterequests.cert-manager.io; do
  if printf '%s\n' "$cert_manager_resources" | grep -qx "$resource"; then
    pass "cert-manager API resource is available: $resource"
  else
    fail "cert-manager API resource is missing: $resource"
  fi
done

if "$KUBECTL_BIN" get --raw /apis/cert-manager.io/v1 >/dev/null 2>&1; then
  pass "cert-manager v1 API is served"
else
  fail "cert-manager v1 API is unavailable"
fi

if "$KUBECTL_BIN" auth can-i get deployments.apps -n cert-manager | grep -qx "yes"; then
  if "$KUBECTL_BIN" -n cert-manager get deployment cert-manager >/dev/null 2>&1; then
    pass "cert-manager controller deployment is visible"
  else
    fail "cert-manager controller deployment not found in namespace cert-manager"
  fi
else
  warn "RBAC cannot inspect the cert-manager namespace; API discovery passed but controller deployment health was not directly verified"
fi

if "$KUBECTL_BIN" get ingressclass nginx >/dev/null 2>&1; then
  pass "nginx IngressClass is available"
else
  fail "IngressClass nginx is missing"
fi

if "$KUBECTL_BIN" get --raw /apis/metrics.k8s.io/v1beta1 >/dev/null 2>&1; then
  pass "Kubernetes resource metrics API is available"
elif [ "$REQUIRE_METRICS_SERVER" = "true" ]; then
  fail "metrics.k8s.io API is unavailable; HPA CPU/memory metrics will not work"
else
  warn "metrics.k8s.io API is unavailable, but REQUIRE_METRICS_SERVER is not true"
fi

if "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" get secret task-api-secrets >/dev/null 2>&1; then
  pass "task-api-secrets exists"
  check_secret_key task-api-secrets DATABASE_URL
  check_secret_key task-api-secrets REDIS_URL
  check_secret_key task-api-secrets JWT_SECRET
else
  fail "required secret task-api-secrets is missing"
fi

if "$KUBECTL_BIN" -n "$KUBE_NAMESPACE" get secret ghcr-pull-secret >/dev/null 2>&1; then
  pass "ghcr-pull-secret exists"
elif [ "$REQUIRE_GHCR_PULL_SECRET" = "true" ]; then
  fail "ghcr-pull-secret is required but missing"
else
  warn "ghcr-pull-secret is missing; this is valid only when the GHCR image is publicly pullable"
fi

check_can_i get secrets
check_can_i create configmaps
check_can_i get jobs.batch
check_can_i create jobs.batch
check_can_i delete jobs.batch
check_can_i get deployments.apps
check_can_i create deployments.apps
check_can_i patch deployments.apps
check_can_i create services
check_can_i create ingresses.networking.k8s.io
check_can_i create horizontalpodautoscalers.autoscaling
check_can_i create poddisruptionbudgets.policy
check_can_i create networkpolicies.networking.k8s.io
check_can_i create clusterissuers.cert-manager.io cluster

if [ "$failures" -gt 0 ]; then
  printf '\nProduction readiness FAILED with %d issue(s).\n' "$failures" >&2
  exit 1
fi

printf '\nProduction readiness PASSED.\n'
