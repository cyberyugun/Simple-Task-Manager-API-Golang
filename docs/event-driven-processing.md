# Event-Driven Processing, Outbox, and Webhooks

Phase 24 adds reliable asynchronous processing without introducing an external message broker.

## Architecture

Task mutations and event creation share the same PostgreSQL transaction:

```text
HTTP mutation
   |
   v
Task repository transaction
   |
   +-- INSERT / UPDATE / DELETE task
   |
   +-- INSERT outbox_events
   |
   +-- optional idempotency_keys update
   |
   v
COMMIT
   |
   v
Worker fanout
   |
   +-- webhook_subscriptions filter
   |
   +-- webhook_deliveries
   |
   v
Signed HTTP delivery
   |
   +-- 2xx -> delivered
   |
   +-- failure -> exponential retry
   |
   +-- max attempts -> dead letter
```

The outbox prevents the dual-write failure mode where business data commits but a separately published event is lost.

## Event envelope v1

Current task events:

```text
task.created
task.updated
task.completed
task.deleted
```

Every webhook payload uses:

```json
{
  "event_id": "<uuid>",
  "event_type": "task.created",
  "schema_version": 1,
  "workspace_id": 42,
  "aggregate_type": "task",
  "aggregate_id": "123",
  "occurred_at": "2026-10-03T05:00:00Z",
  "data": {}
}
```

The event schema version is independent from HTTP API document versioning. Consumers should route primarily by `event_type` and tolerate additive fields within the same event schema version.

## Task idempotency

`POST /api/tasks` accepts:

```text
Idempotency-Key: <8-128 characters>
```

Keys are scoped to the resolved workspace.

For the retention window:

- first request creates the task and outbox event atomically
- identical retries return the same task with HTTP 201
- replayed responses include `Idempotency-Replayed: true`
- reusing the key with a different normalized task payload returns HTTP 409
- only one `task.created` event is produced

The default idempotency retention is 24 hours.

## Webhook API

Owner/admin workspace roles can manage webhooks through:

```text
GET    /api/webhooks
POST   /api/webhooks
DELETE /api/webhooks/{id}

GET    /api/webhooks/{id}/deliveries
POST   /api/webhooks/{id}/deliveries/{delivery_id}/replay
```

These routes use the same optional `X-Workspace-ID` selector as task routes.

An empty `event_types` list subscribes to all supported current event types.

## Signing

Webhook signing uses a separate `WEBHOOK_SIGNING_KEY`, minimum 32 characters in deployed environments.

The subscription-specific secret is derived using HMAC-SHA256 from the master key and subscription ID. It is returned on subscription creation and is not persisted as plaintext.

Each delivery includes:

```text
X-Webhook-Delivery-ID: whd_<delivery-id>
X-Webhook-Event-ID: <event-id>
X-Webhook-Event: task.created
X-Webhook-Timestamp: <unix-seconds>
X-Webhook-Signature: v1=<hex-hmac>
Idempotency-Key: <event-id>
```

Signature input:

```text
<timestamp>.<raw-request-body>
```

Receivers should:

1. reject timestamps outside a small replay window
2. compute HMAC-SHA256 using the subscription signing secret
3. compare signatures in constant time
4. deduplicate using `event_id` or the `Idempotency-Key` header
5. return a 2xx only after the event is durably accepted

## Retry and dead-letter behavior

The worker increments the attempt counter when it claims a delivery.

Retry delay follows exponential backoff:

```text
attempt 1 -> 1s
attempt 2 -> 2s
attempt 3 -> 4s
...
capped at 1h
```

Default max delivery attempts: **8**.

After the final failed attempt, `dead_lettered_at` is set and normal polling stops for that delivery.

Owners/admins can inspect delivery history and manually replay a delivery. Replay clears prior terminal state and places it back on the delivery queue.

## Worker concurrency

Multiple worker replicas are safe.

PostgreSQL claims pending deliveries with:

```text
FOR UPDATE SKIP LOCKED
```

A lock TTL permits another worker to recover a delivery when a process dies after claiming it.

Outbox fanout is also transactional: delivery rows and `published_at` advance together.

## SSRF protection

Public webhook URLs must use HTTPS.

Loopback HTTP is accepted by the API for local development only. The worker separately rejects loopback, private, link-local, and unspecified addresses unless:

```text
WEBHOOK_ALLOW_PRIVATE_NETWORKS=true
```

Production Kubernetes explicitly sets this to `false`.

Private-network webhook delivery should only be enabled in a deliberately isolated environment with a reviewed network policy.

## Runtime settings

```text
WEBHOOK_SIGNING_KEY
WORKER_POLL_INTERVAL=1s
WORKER_BATCH_SIZE=50
WORKER_LOCK_TTL=2m
WEBHOOK_HTTP_TIMEOUT=10s
WEBHOOK_ALLOW_PRIVATE_NETWORKS=false
WORKER_METRICS_ADDR=:9091
```

The Docker image contains:

```text
/app/task-api
/app/migrate
/app/worker
```

## Worker observability

Worker endpoints:

```text
GET :9091/health
GET :9091/metrics
```

Metrics include:

```text
task_worker_fanout_total
task_worker_delivered_total
task_worker_retried_total
task_worker_dead_letter_total
task_worker_processing_errors
```

Production Prometheus alerts cover processing errors and any new dead-letter event.

## Operations

Inspect recent delivery state through the authenticated API rather than reading database tables directly.

For incident investigation, correlate:

- event ID
- delivery ID
- subscription ID
- workspace ID
- event type
- attempt count
- last HTTP status/error

A dead-letter generally means the receiver remained unavailable or rejected the payload through every retry. Fix the destination first, then replay.

## Deployment safety

The worker uses the same immutable image digest as the API.

Staging and production deployments wait for both:

```text
deployment/task-api
deployment/task-worker
```

Production progressive delivery promotes or rolls back the worker together with the stable API release.

Database changes remain expand-compatible so the old API, canary API, and worker can coexist during release transitions.
