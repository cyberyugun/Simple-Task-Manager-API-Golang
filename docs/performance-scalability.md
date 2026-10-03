# Performance, Scalability, and Load Engineering

Phase 19 adds repeatable performance regression testing, capacity guardrails, query/index validation, and tunable PostgreSQL/Redis client pools.

## Performance objectives

The current engineering targets are:

| Signal | Target |
| --- | ---: |
| HTTP request failure ratio | < 1% |
| Overall p95 latency | < 750 ms in CI/local load harness |
| Overall p99 latency | < 1500 ms in CI/local load harness |
| Task list p95 | < 500 ms |
| Task create p95 | < 750 ms |
| Production SLO p95 | < 500 ms |
| PostgreSQL pool utilization | sustained < 80% |
| Redis pool timeouts | effectively zero during steady load |

The CI/load-harness thresholds are intentionally slightly looser than the production SLO because GitHub-hosted runners and local Docker Compose are noisy shared environments.

## k6 profiles

The version-controlled workload is:

```text
tests/performance/task-api.js
```

It performs an authenticated business flow:

```text
register performance user
       |
       v
GET task list
       |
       v
POST task
       |
       v
PATCH complete
       |
       v
GET task
       |
       v
DELETE task
```

Profiles:

### smoke

```text
2 VUs
10 seconds
```

Runs in normal CI and blocks merge on threshold failure.

### load

```text
0 -> 10 VUs / 30s
10 -> 25 VUs / 2m
25 -> 0 VUs / 30s
```

Runs weekly by default in the dedicated Performance workflow.

### spike

```text
10 -> 75 VUs rapidly
hold 75 VUs
recover to 0
```

Use to validate burst behavior, connection-pool saturation, and recovery.

### soak

```text
25 VUs
15 minutes
```

Use to find connection leaks, monotonically increasing memory, Redis pool churn, and latency degradation over time.

## Running locally

Start the stack:

```bash
JWT_SECRET='replace-with-a-long-local-secret' \
AUTH_RATE_LIMIT_REQUESTS=10000 \
docker compose up -d --build
```

Run the smoke profile:

```bash
docker run --rm --network host \
  -v "$PWD:/work" \
  -w /work \
  grafana/k6:2.3.0 run \
  -e BASE_URL=http://127.0.0.1:8080 \
  -e PROFILE=smoke \
  tests/performance/task-api.js
```

Use `PROFILE=load`, `spike`, or `soak` for deeper tests.

The script writes `performance-summary.json` through k6's `handleSummary` hook.

## Scheduled workflow

`.github/workflows/performance.yml` runs the load profile weekly.

It can also be started manually with:

```text
load
spike
soak
```

Artifacts retained for each run:

- `performance-summary.json`
- Prometheus application metrics snapshot
- Docker Compose service state

This gives a reproducible record for comparing regressions over time.

## PostgreSQL pool tuning

Supported environment variables:

```text
DB_MAX_OPEN_CONNS
DB_MAX_IDLE_CONNS
DB_CONN_MAX_IDLE_TIME
DB_CONN_MAX_LIFETIME
```

Application defaults:

```text
max open: 10
max idle: 5
max idle time: 5m
max lifetime: 30m
```

The production Kubernetes ConfigMap is deliberately more conservative:

```text
DB_MAX_OPEN_CONNS=8
DB_MAX_IDLE_CONNS=4
```

The HPA maximum is 10 replicas, therefore the version-controlled worst-case application pool budget is:

```text
8 connections/pod x 10 pods = 80 possible PostgreSQL connections
```

`scripts/performance-policy-check.sh` blocks CI when that value exceeds the current repository budget of 80.

The real database connection budget must reserve headroom for:

- migration jobs
- administrators
- monitoring
- failover/recovery processes
- provider internal sessions
- future services

When moving to a different PostgreSQL tier, update the documented budget only after checking that provider's effective `max_connections` and preserving safe headroom.

## Redis pool tuning

Supported environment variables:

```text
REDIS_POOL_SIZE
REDIS_MIN_IDLE_CONNS
REDIS_POOL_TIMEOUT
REDIS_DIAL_TIMEOUT
REDIS_READ_TIMEOUT
REDIS_WRITE_TIMEOUT
```

Defaults:

```text
pool size: 20
minimum idle: 5
pool timeout: 4s
dial timeout: 5s
read timeout: 3s
write timeout: 3s
```

Watch:

```text
task_api_redis_pool_timeouts_total
task_api_redis_pool_total_connections
task_api_redis_pool_idle_connections
```

Do not solve Redis latency by increasing client connections indefinitely. Validate server CPU, memory, network latency, command mix, and connection limits first.

## PostgreSQL indexes and query plans

Migration `006_performance_indexes.sql` adds:

- `pg_trgm`
- GIN trigram index for task title search
- GIN trigram index for task description search
- user/completed/created-at composite index
- active refresh-session last-used index

The existing `ILIKE '%search%'` query cannot efficiently use a normal B-tree title index. The trigram indexes match that access pattern.

PostgreSQL integration tests verify:

- the extension exists
- all performance indexes exist
- the completed-task listing can use its composite index
- the substring title search can use its trigram index

The query-plan assertions disable sequential scans **only inside the EXPLAIN transaction**. This proves index compatibility; it is not a claim that PostgreSQL will always choose that index for every table size or data distribution.

## Saturation metrics and alerts

Prometheus exposes:

```text
task_api_db_max_open_connections
task_api_db_open_connections
task_api_db_in_use_connections
task_api_db_idle_connections
task_api_db_wait_count_total
task_api_db_wait_duration_seconds_total

task_api_redis_pool_timeouts_total
task_api_redis_pool_total_connections
task_api_redis_pool_idle_connections
```

Phase 19 adds alerts for:

- database pool > 80% utilized for 10 minutes
- repeated database pool waits
- Redis pool timeouts

Treat pool waits as saturation evidence, not as a reason to blindly raise pool limits.

## HPA load validation

The production HPA scales from 3 to 10 replicas using:

```text
CPU target: 70%
memory target: 80%
```

A real cluster validation should run the k6 load/spike profile against the public or controlled test endpoint while watching:

```bash
kubectl -n task-manager get hpa task-api -w
kubectl -n task-manager get pods -w
kubectl -n task-manager top pods
```

Acceptance criteria:

1. replicas scale above the baseline when sustained CPU/memory demand requires it
2. no pods remain Pending because of resource or zone constraints
3. readiness remains stable
4. error rate stays below the test threshold
5. p95 latency returns toward baseline after scale-out
6. DB pool utilization stays below sustained saturation
7. scale-down follows the configured stabilization window without oscillation

A local Docker test cannot prove HPA behavior; HPA validation must be performed on a Kubernetes environment with Metrics API enabled.

## Capacity model

Use this simple model before raising traffic expectations:

```text
effective request capacity
  = min(
      API CPU capacity,
      PostgreSQL throughput,
      Redis throughput,
      ingress/network capacity
    )
```

For connection-backed dependencies:

```text
worst-case DB client connections
  = HPA max replicas x DB_MAX_OPEN_CONNS
```

Do not size solely from requests/second. Track concurrency and service time:

```text
concurrency ~= throughput x average response time
```

A workload at 200 req/s with 250 ms average service time represents roughly 50 concurrent in-flight requests.

## Regression process

When a performance run regresses:

1. compare throughput, p95, p99, and failure rate with the last known-good run
2. inspect per-route latency
3. inspect PostgreSQL pool utilization/waits
4. inspect Redis pool timeout/miss behavior
5. check CPU/memory throttling
6. review query plans for changed queries
7. correlate traces for slow requests
8. compare application commit and infrastructure changes
9. fix the bottleneck before raising thresholds

Thresholds should only be relaxed when a new documented service objective or capacity model justifies the change.
