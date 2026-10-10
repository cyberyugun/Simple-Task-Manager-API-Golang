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
grep -q 'app.kubernetes.io/part-of: simple-task-manager' deploy/k8s/base/networkpolicy.yaml

build_push_block="$(awk '/- name: Build and push/{flag=1} /- name: Scan published image/{flag=0} flag' .github/workflows/deploy.yml)"
if printf '%s\n' "$build_push_block" | grep -q 'IMAGE_REPOSITORY.*latest'; then
  echo "build step must not publish :latest before promotion succeeds" >&2
  exit 1
fi
grep -q 'name: Enforce deployment freeze' .github/workflows/deploy.yml
grep -q 'name: Progressive canary release' .github/workflows/deploy.yml
grep -q 'name: Move latest tag only after successful promotion' .github/workflows/deploy.yml
grep -q 'name: Deploy and Validate Staging' .github/workflows/deploy.yml
grep -q 'needs: \[build, staging\]' .github/workflows/deploy.yml
grep -q 'name: Verify staging promotion gate' .github/workflows/deploy.yml
grep -q 'environment: staging' .github/workflows/deploy.yml
grep -q 'staging-promotion-evidence' .github/workflows/deploy.yml
grep -q 'name: Enforce production provider validation policy' .github/workflows/deploy.yml
grep -q 'PROVIDER_VALIDATION_GATE_MODE' .github/workflows/deploy.yml
grep -q 'PROVIDER_VALIDATION_REQUIRED_TARGETS' .github/workflows/deploy.yml
grep -q 'PROVIDER_OPERATIONAL_EVIDENCE_REQUIRED' .github/workflows/deploy.yml
grep -q 'PROVIDER_OPERATIONAL_EVIDENCE_REQUIRE_APPROVAL' .github/workflows/deploy.yml
grep -q 'provider-operational-approved-' .github/workflows/deploy.yml
grep -q 'provider-operational-revocation-' .github/workflows/deploy.yml
grep -q 'name: Provider Operational Evidence Governance' .github/workflows/provider-operational-evidence-governance.yml
grep -q 'name: Provider Operational Evidence Expiry' .github/workflows/provider-operational-evidence-expiry.yml
grep -q 'provider-operational-evidence' .github/workflows/deploy.yml
grep -q 'provider-validation-release-policy.json' .github/workflows/deploy.yml
grep -q 'actions: read' .github/workflows/deploy.yml

python3 scripts/check-migration-compatibility.py migrations

echo "Progressive release policy PASSED: canary_weight=$CANARY_WEIGHT observation_seconds=$CANARY_OBSERVATION_SECONDS"
