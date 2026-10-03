#!/usr/bin/env bash
set -euo pipefail

DB_CONNECTION_BUDGET="${DB_CONNECTION_BUDGET:-80}"

config_file="deploy/k8s/base/configmap.yaml"
hpa_file="deploy/k8s/base/hpa.yaml"
rules_file="deploy/k8s/observability/prometheusrule.yaml"
migration_file="migrations/006_performance_indexes.sql"

db_max_open="$(awk '/DB_MAX_OPEN_CONNS:/ {gsub(/"/, "", $2); print $2}' "$config_file")"
db_max_idle="$(awk '/DB_MAX_IDLE_CONNS:/ {gsub(/"/, "", $2); print $2}' "$config_file")"
hpa_min="$(awk '/minReplicas:/ {print $2; exit}' "$hpa_file")"
hpa_max="$(awk '/maxReplicas:/ {print $2; exit}' "$hpa_file")"

for value in "$DB_CONNECTION_BUDGET" "$db_max_open" "$db_max_idle" "$hpa_min" "$hpa_max"; do
  printf '%s' "$value" | grep -Eq '^[0-9]+$' || {
    echo "performance policy value is not numeric: $value" >&2
    exit 1
  }
done

if [ "$db_max_idle" -gt "$db_max_open" ]; then
  echo "DB_MAX_IDLE_CONNS exceeds DB_MAX_OPEN_CONNS" >&2
  exit 1
fi

worst_case_connections=$((db_max_open * hpa_max))
if [ "$worst_case_connections" -gt "$DB_CONNECTION_BUDGET" ]; then
  echo "worst-case DB pool budget exceeded: $db_max_open x $hpa_max = $worst_case_connections > $DB_CONNECTION_BUDGET" >&2
  exit 1
fi

if [ "$hpa_min" -lt 3 ]; then
  echo "HPA minimum replicas must remain at least 3" >&2
  exit 1
fi

for alert in TaskAPIHighP95Latency TaskAPIDatabasePoolSaturation TaskAPIDatabasePoolWaits TaskAPIRedisPoolTimeouts; do
  grep -q "alert: $alert" "$rules_file" || {
    echo "missing performance alert: $alert" >&2
    exit 1
  }
done

for index in idx_tasks_user_title_trgm idx_tasks_user_description_trgm idx_tasks_user_completed_created_at idx_refresh_tokens_user_active_last_used; do
  grep -q "$index" "$migration_file" || {
    echo "missing performance index: $index" >&2
    exit 1
  }
done

echo "Performance capacity policy PASSED: replicas=$hpa_min-$hpa_max db_pool_per_pod=$db_max_open worst_case_db_connections=$worst_case_connections budget=$DB_CONNECTION_BUDGET"
