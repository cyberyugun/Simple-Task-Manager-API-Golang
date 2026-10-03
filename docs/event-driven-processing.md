# Event-Driven Processing and Webhooks

Phase 24 adds durable asynchronous processing using the PostgreSQL transactional outbox pattern.

## Architecture

```text
Task mutation
    |
    | same PostgreSQL transaction
    v
tasks + outbox_events
    |
    v
task-worker
    |
    +--> webhook_deliveries
              |
              +--> HTTPS subscriber
              +--> retry/backoff
              +--> dead-letter
```

The API does not publish directly to an external broker. PostgreSQL is the durability source, which avoids a dual-write window between task persistence and event publication.

## Domain events

Current schema version: `1`.

Event types:

- `task.created`
- `task.updated`
- `task.completed`
- `task.deleted`

Webhook body:

```json
{
  "event_id": "evt_...",
  "workspace_id": 42,
  "aggregate_type": "task",
  "aggregate_id": "123",
  "event_type": "task.created",
  "schema_version": 1,
  "payload": {
    "id": 123,
    "workspace_id": 42,
    "title": "Ship Phase 24",
    "description": "Durable async processing",
    "completed": false,
    "created_at": "2026-10-03T06:00:00Z",
    "updated_at": "2026-10-03T06:00:00Z"
  },
  "occurred_at": "2026-10-03T06:00:00Z"
}
```

Consumers must branch on `schema_version`. Additive payload fields remain within the same schema version; incompatible event shape changes require a new schema version.

## Transactional outbox guarantee

PostgreSQL task create/update/complete/delete operations insert their corresponding outbox row before committing the same transaction.

Therefore:

- task commit + event missing is not allowed
- event commit + task rollback is not allowed
- worker downtime does not lose committed events
- multiple workers can claim work concurrently with `FOR UPDATE SKIP LOCKED`

A task delete captures the task payload before deletion and writes `task.deleted` in the same transaction.

## Worker recovery

Outbox and webhook delivery rows move through:

```text
pending -> processing -> processed/delivered
                    \-> dead
```

A worker crash can leave a row in `processing`. Rows locked for more than five minutes are eligible to be reclaimed automatically.

Default worker configuration:

```text
WORKER_BATCH_SIZE=25
WORKER_POLL_INTERVAL=1s
WEBHOOK_REQUEST_TIMEOUT=10s
WEBHOOK_MAX_ATTEMPTS=8
WEBHOOK_BASE_BACKOFF=1s
```

Retry uses exponential backoff capped by the attempt-shift limit in the worker. Exhausted webhook deliveries enter the dead-letter state.

## Idempotency

Each event has a unique immutable `event_id`.

Webhook fan-out has a database uniqueness constraint on:

```text
(subscription_id, event_id)
```

so replaying/fan-out retry cannot create duplicate logical delivery rows.

Every webhook request includes:

```text
X-Webhook-Event: task.created
X-Webhook-Event-ID: evt_...
X-Webhook-Schema-Version: 1
X-Webhook-Timestamp: <unix-seconds>
X-Webhook-Signature: sha256=<hex-hmac>
Idempotency-Key: evt_...
```

Subscribers should persist processed event IDs and treat `Idempotency-Key` as the deduplication key because HTTP retries can still cause the same event to be received more than once.

## Signature verification

The signature is:

```text
HMAC-SHA256(secret, timestamp + "." + raw_request_body)
```

Verify the raw body before JSON parsing and reject timestamps outside an acceptable clock-skew window.

Webhook signing secrets must be at least 32 characters and are never returned by the API after creation.

## Destination security

Webhook URLs must use HTTPS.

The worker resolves the destination before dialing and rejects:

- private addresses
- loopback addresses
- link-local addresses
- unspecified addresses
- multicast addresses

This reduces SSRF exposure even when a hostname resolves to an internal address.

## Administration API

All endpoints require JWT authentication plus workspace resolution. Only workspace owners/admins can use them.

```text
GET    /api/webhooks
POST   /api/webhooks
DELETE /api/webhooks/{id}

GET    /api/events/stats
POST   /api/events/replay
```

Use `X-Workspace-ID` for shared workspaces. Omitting it selects the personal workspace.

Supported subscription filters:

- `task.*`
- `task.created`
- `task.updated`
- `task.completed`
- `task.deleted`

## Dead-letter replay

API:

```bash
curl -X POST https://api.example.com/api/events/replay \
  -H "Authorization: Bearer <token>" \
  -H "X-Workspace-ID: 42"
```

Operator CLI:

```bash
DATABASE_URL="$DATABASE_URL" go run ./cmd/eventctl \
  -action stats \
  -workspace-id 42

DATABASE_URL="$DATABASE_URL" go run ./cmd/eventctl \
  -action replay \
  -workspace-id 42
```

The container image also includes `/app/eventctl`.

Replay resets matching dead-letter rows to pending and clears attempt/error state.

## Observability

`GET /metrics` exposes durable queue gauges:

```text
task_api_outbox_events{status="pending"}
task_api_outbox_events{status="dead"}
task_api_webhook_deliveries{status="pending"}
task_api_webhook_deliveries{status="dead"}
```

The worker emits structured logs for startup, iteration failures, and shutdown. Queue statistics are also available per workspace from `GET /api/events/stats`.

Operational alerts should trigger on sustained dead-letter counts and continuously growing pending queues.

## Deployment

The same immutable image contains:

- `/app/task-api`
- `/app/migrate`
- `/app/worker`
- `/app/eventctl`

Docker Compose runs the worker as a separate process.

Kubernetes deploys `task-worker` separately from `task-api`. Production uses two worker replicas and PostgreSQL row locking to distribute work safely. Staging uses one worker replica.

The progressive production release promotes the API and worker to the same signed image digest. Rollback restores both workloads to their previous digest.

## Failure semantics

Webhook delivery is **at least once**, not exactly once.

A subscriber can receive an event more than once when:

- the receiver processes a request but the response is lost
- a network timeout occurs after remote processing
- an operator replays dead-letter deliveries

Consumers must therefore be idempotent by `event_id`.
