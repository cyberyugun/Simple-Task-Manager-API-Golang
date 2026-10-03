# Observability and SRE

Phase 17 adds a production observability contract across application metrics, distributed traces, centralized logs, synthetic probing, SLO alerting, and recovery verification.

## Signals

### Metrics

The API exposes `GET /metrics` using Prometheus text exposition.

HTTP metrics use bounded labels only:

- method
- normalized route template
- status class

Task IDs and other dynamic URL values are never used as metric labels.

Key metrics include:

```text
task_api_http_requests_total
task_api_http_request_duration_seconds_bucket
task_api_http_in_flight_requests
task_api_panics_total
task_api_rate_limited_total
task_api_readiness_failures_total
task_api_dependency_ready
task_api_trace_export_errors_total

task_api_db_open_connections
task_api_db_in_use_connections
task_api_db_idle_connections
task_api_db_wait_count_total
task_api_db_wait_duration_seconds_total

task_api_redis_pool_hits_total
task_api_redis_pool_misses_total
task_api_redis_pool_timeouts_total
task_api_redis_pool_total_connections
task_api_redis_pool_idle_connections
task_api_redis_pool_stale_connections_total
```

The production overlay includes a `ServiceMonitor`, `PrometheusRule`, and Grafana dashboard ConfigMap.

### Tracing

The API supports W3C `traceparent` propagation and generates server spans.

When `OTEL_EXPORTER_OTLP_ENDPOINT` is configured, spans are batched and sent with OTLP/HTTP JSON to:

```text
<OTEL_EXPORTER_OTLP_ENDPOINT>/v1/traces
```

Production defaults point to the in-cluster OpenTelemetry Collector.

Request logs include:

```text
request_id
trace_id
span_id
method
path
status
duration_ms
```

Trace export failures do not take the API down. They increment `task_api_trace_export_errors_total` and emit a warning log.

### Logs

The application writes structured JSON to stdout.

The platform Terraform stack can install Grafana Alloy and Loki. Production Loki and Alloy values are deliberately supplied externally because storage, retention, tenancy, credentials, and capacity must be chosen for the actual environment instead of using an ephemeral repository default.

Recommended minimum log retention:

```text
application logs: 14 days
security/audit-relevant logs: 30 days or organizational requirement
incident logs: retain/export according to incident policy
```

Do not log passwords, access tokens, refresh tokens, reset tokens, database URLs, Redis URLs, or cloud credentials.

## Platform stack

Set:

```text
enable_observability=true
```

and pin approved chart versions for:

- kube-prometheus-stack
- Tempo
- Loki
- Grafana Alloy
- OpenTelemetry Collector

The platform stack creates the `monitoring` namespace.

Loki and Tempo values must use durable production storage. Do not use filesystem-only storage for a production installation.

## SLOs

Initial service objectives:

| SLI | Objective | Window |
| --- | --- | --- |
| Availability | 99.9% successful non-health requests | rolling 30 days |
| Server error ratio | < 1% 5xx | 5-minute signal, 10-minute alert hold |
| Latency | p95 < 500 ms | 5-minute signal, 10-minute alert hold |
| Dependency readiness | PostgreSQL and Redis ready | continuous |
| Trace export | no sustained export failures | 15-minute signal |
| Synthetic probes | health/ready/version HTTP 200 and latency below configured threshold | every 15 minutes |

A 99.9% availability target permits approximately 43 minutes 50 seconds of unavailable time in a 30-day period.

Health, readiness, and metrics scraping endpoints are excluded from the user-facing availability error-rate query.

These are initial operational targets. Revisit them after real production traffic establishes a stable baseline.

## Alerts

`deploy/k8s/observability/prometheusrule.yaml` defines:

- `TaskAPIHighErrorRate`
- `TaskAPIHighP95Latency`
- `TaskAPIDependencyNotReady`
- `TaskAPIPanicsDetected`
- `TaskAPITraceExportFailures`
- `TaskAPIReadinessFailures`

Page-severity alerts should route to the active incident responder. Ticket-severity alerts should create a trackable follow-up if they persist.

## Synthetic production checks

`.github/workflows/synthetic.yml` runs every 15 minutes after this repository variable is configured:

```text
SYNTHETIC_BASE_URL=https://api.example.com
```

Optional:

```text
SYNTHETIC_MAX_LATENCY_MS=2000
```

Create a GitHub Environment named `synthetic-production` if you want environment-level approval/policy controls.

The workflow validates:

- HTTPS-only target
- `GET /health`
- `GET /ready`
- `GET /version`
- HTTP 200
- JSON success payloads
- version metadata presence
- maximum endpoint latency

## Backup and restore drill

`.github/workflows/backup-restore.yml` runs weekly after:

```text
BACKUP_RESTORE_ENABLED=true
```

is configured.

Create a GitHub Environment named `backup-restore` containing:

```text
BACKUP_DATABASE_URL
RESTORE_DATABASE_URL
```

`RESTORE_DATABASE_URL` must point to an isolated disposable database. The verification script intentionally resets the target `public` schema before restoring.

The drill:

1. creates a logical custom-format PostgreSQL dump
2. resets only the isolated restore target
3. restores the dump with `pg_restore --exit-on-error`
4. compares source and restored public table counts
5. compares `schema_migrations` counts
6. verifies the restored database accepts a query

This logical restore drill complements, but does not replace, provider snapshot/PITR tests. RDS, Azure PostgreSQL, and Cloud SQL recovery procedures should also be exercised periodically.

## Production deploy gate

Production preflight requires:

- Metrics API
- cert-manager APIs
- Prometheus Operator `ServiceMonitor` and `PrometheusRule` APIs
- `monitoring` namespace
- RBAC to create SLO monitoring resources

After rollout, deployment verification confirms:

- `/health`
- `/ready`
- `/version`
- SLO histogram metrics
- trace-export failure metric
- `ServiceMonitor/task-api`
- `PrometheusRule/task-api-slo`
- Grafana dashboard ConfigMap

A production rollout therefore fails before completion when the required observability control plane is absent.

## Dashboard

The bundled Grafana dashboard covers:

- request throughput
- 5xx ratio
- p95 latency
- route/status traffic
- PostgreSQL connection pools
- Redis pool health
- dependency readiness

Treat dashboard panels as investigation aids rather than alert sources. Alert logic remains version-controlled in `PrometheusRule`.
