#!/usr/bin/env bash
set -euo pipefail

failures=0
pass() { printf 'PASS: %s\n' "$*"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; failures=$((failures + 1)); }

require_text() {
  local file="$1"
  local pattern="$2"
  local description="$3"
  if grep -Eq "$pattern" "$file"; then
    pass "$description"
  else
    fail "$description"
  fi
}

require_text infra/terraform/aws/main.tf 'count = var.availability_zone_count' 'AWS uses per-AZ NAT/subnet resources'
require_text infra/terraform/aws/main.tf 'multi_az[[:space:]]*=[[:space:]]*var.postgres_multi_az' 'AWS RDS Multi-AZ is controlled explicitly'
require_text infra/terraform/aws/main.tf 'automatic_failover_enabled[[:space:]]*=[[:space:]]*true' 'AWS Redis automatic failover is enabled'
require_text infra/terraform/azure/main.tf 'mode[[:space:]]*=[[:space:]]*"ZoneRedundant"' 'Azure PostgreSQL zone-redundant HA is enabled'
require_text infra/terraform/azure/main.tf 'geo_redundant_backup_enabled[[:space:]]*=[[:space:]]*true' 'Azure PostgreSQL geo-redundant backups are enabled'
require_text infra/terraform/azure/main.tf 'zones[[:space:]]*=[[:space:]]*var.aks_node_zones' 'AKS nodes are distributed across zones'
require_text infra/terraform/gcp/main.tf 'availability_type[[:space:]]*=[[:space:]]*"REGIONAL"' 'Cloud SQL regional HA is enabled'
require_text infra/terraform/gcp/main.tf 'point_in_time_recovery_enabled[[:space:]]*=[[:space:]]*true' 'Cloud SQL PITR is enabled'
require_text infra/terraform/gcp/main.tf 'tier[[:space:]]*=[[:space:]]*"STANDARD_HA"' 'Memorystore Standard HA is enabled'
require_text deploy/k8s/base/deployment.yaml 'topologyKey: topology.kubernetes.io/zone' 'API pods have zone topology spreading'
require_text deploy/k8s/base/deployment.yaml 'whenUnsatisfiable: DoNotSchedule' 'Zone anti-concentration is enforced'
require_text deploy/k8s/base/hpa.yaml 'minReplicas: 3' 'HPA keeps at least three API replicas'
require_text deploy/k8s/base/pdb.yaml 'minAvailable: 1' 'PodDisruptionBudget protects application availability'

if [ "$failures" -gt 0 ]; then
  echo "DR readiness policy FAILED with $failures issue(s)." >&2
  exit 1
fi

echo "DR readiness policy PASSED."
