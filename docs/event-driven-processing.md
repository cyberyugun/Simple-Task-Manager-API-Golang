# Event-Driven Processing, Webhooks, and Idempotency

Phase 24 adds reliable asynchronous processing without requiring an external broker. PostgreSQL is the durable source of truth for domain changes, outbox events, delivery attempts, dead letters, and idempotency records.

## Architecture

```text
HTTP request
    |
    v
Task transaction
    |
    +-- task row
    |
    +-- outbox_events row
            |
            v
      committed together
            |
            v
      task-worker replicas
            |
      FOR UPDATE SKIP LOCKED
            |
            +-- no matching webhook
            |       |
            |       +--> processed
            |
            +-- matching webhooks
                    |
                    +--> signed HTTPS POST
                    |
                    +--> delivery record
                    |
                    +--> retry / dead-letter
```

A task write is not considered committed unless its corresponding outbox event is also committed.

## Event catalog

Current schema version: **1**.

Published event types:

- `task.created`
- `task.updated`
- `task.deleted`

Envelope:

```json
{
  "id": "evt_0123456789abcdef0123456789abcdef",
  "workspace_id": 42,
  "type": "task.created",
  "aggregate_type": "task",
  "aggregate_id": "123",
  "schema_version": 1,
  "data": {
    "id": 123,
    "workspace_id": 42,
    "title": "Review release",
    "description": "",
    "completed": false,
    "created_at": "2026-10-03T07:00:00Z",
    "updated_at": "2026-10-03T07:00:00Z"
  },
  "occurred_at": "2026-10-03T07:00:00Z"
}
```

Consumers should branch on both `type` and `schema_version`. Additive fields may be introduced within a schema version. Incompatible event payload changes require a new schema version and a compatibility window.

## Transactional outbox

`outbox_events` is written from the same PostgreSQL transaction as task create/update/delete.

This avoids the classic dual-write failure:

```text
task commit succeeds
event publish fails
```

The worker never deletes outbox rows. Processing state is retained through `processed_at`, `attempts`, `last_error`, and `dead_lettered_at`.

## Worker concurrency

The worker claims ready rows with:

```sql
FOR UPDATE SKIP LOCKED
```

This allows multiple worker replicas without two replicas processing the same event concurrently.

Production Kubernetes deploys two `task-worker` replicas using the same immutable application image as the API.

A stale lock is released after the configured worker lock timeout so an event can recover after a worker crash.

## Retry and dead-letter behavior

Default maximum attempts: **8**.

Retry delay is exponential:

```text
1s
2s
4s
8s
16s
32s
64s
128s
```

The SQL policy includes a 300-second upper bound for future larger attempt counts.

After the final failed attempt:

```text
dead_lettered_at != NULL
```

The event is no longer claimed automatically.

## Replay

Replay one event by its public event ID:

```bash
DATABASE_URL="$DATABASE_URL" ./event-replay evt_...
```

or in source form:

```bash
DATABASE_URL="$DATABASE_URL" EVENT_ID=evt_... go run ./cmd/event-replay
```

Replay:

- resets attempts and failure state
- clears the dead-letter marker
- clears old delivery rows
- makes the event immediately available

Clearing delivery rows is intentional: replay means **redeliver**, including subscriptions that previously returned success.

## Webhook subscriptions

Workspace owners and admins can manage subscriptions:

```text
GET    /api/workspaces/{id}/webhooks
POST   /api/workspaces/{id}/webhooks
DELETE /api/workspaces/{id}/webhooks/{subscription_id}
```

Create example:

```json
{
  "url": "https://events.example.com/task-manager",
  "event_types": ["task.created", "task.updated"]
}
```

Use `["*"]` to subscribe to all currently supported event types.

The signing secret is returned **only on creation**. List responses intentionally omit it.

Secrets are stored because the worker must calculate signatures. Production PostgreSQL should use encrypted storage/volumes and restricted database access.

## Webhook signature

Every request includes:

```text
X-Webhook-Id: evt_...
X-Webhook-Event: task.created
X-Webhook-Timestamp: 1791010800
X-Webhook-Signature: v1=<hex-hmac-sha256>
```

Signed message:

```text
<timestamp>.<raw-request-body>
```

Verification pseudocode:

```text
expected = HMAC_SHA256(signing_secret, timestamp + "." + raw_body)
constant_time_compare("v1=" + hex(expected), X-Webhook-Signature)
```

Consumers should also reject stale timestamps and persist `X-Webhook-Id` to make their own processing idempotent.

## Delivery semantics

Webhook delivery is **at least once**.

A successful subscription delivery is recorded in `webhook_deliveries`. If another subscription fails and the event retries, already successful subscriptions are skipped.

A 2xx response is success. Network failures, timeouts, and non-2xx HTTP responses trigger retry.

Response bodies retained for diagnostics are truncated to 2 KiB.

## Webhook destination security

Production defaults:

```text
WEBHOOK_ALLOW_INSECURE_HTTP=false
```

The worker:

- requires HTTPS
- rejects localhost
- rejects literal private/link-local addresses
- resolves DNS itself before connecting
- rejects DNS answers that resolve to private, loopback, link-local, multicast, unspecified, or otherwise non-global-unicast IPs
- validates redirect targets under the same policy

For local testing only:

```text
WEBHOOK_ALLOW_INSECURE_HTTP=true
```

This allows an HTTP local receiver.

## Request idempotency

`POST /api/tasks` accepts:

```text
X-Idempotency-Key: <client-generated-key>
```

The key is scoped to the resolved workspace and retained for `IDEMPOTENCY_TTL` (default 24 hours).

First request:

```text
key + method + path + request hash
        |
        v
pending reservation
        |
        v
handler executes
        |
        v
response persisted
```

A retry using the same key and the exact same request returns the stored response and adds:

```text
Idempotent-Replayed: true
```

Using the same key for a different request returns `409`.

A concurrent request while the first key is still pending also returns `409` with `Retry-After: 1`.

Server-side 5xx responses release the reservation so a later retry can execute again.

If the task transaction succeeds but persisting the idempotency response fails, the reservation deliberately remains pending rather than risking a duplicate task. This is an ambiguous-failure safety bias; the key expires according to `IDEMPOTENCY_TTL`.

## Tables

Phase 24 adds:

```text
outbox_events
webhook_subscriptions
webhook_deliveries
idempotency_records
```

Important indexes cover:

- ready outbox events
- workspace event history
- active workspace subscriptions
- event delivery status
- idempotency expiration

## Runtime configuration

API:

```text
IDEMPOTENCY_TTL=24h
WEBHOOK_ALLOW_INSECURE_HTTP=false
```

Worker:

```text
WORKER_POLL_INTERVAL=2s
WORKER_BATCH_SIZE=50
WEBHOOK_TIMEOUT=10s
WEBHOOK_ALLOW_INSECURE_HTTP=false
```

## Operational queries

Ready queue depth:

```sql
SELECT COUNT(*)
FROM outbox_events
WHERE processed_at IS NULL
  AND dead_lettered_at IS NULL
  AND available_at <= NOW();
```

Dead letters:

```sql
SELECT event_key, event_type, workspace_id, attempts, last_error, dead_lettered_at
FROM outbox_events
WHERE dead_lettered_at IS NOT NULL
ORDER BY dead_lettered_at DESC;
```

Recent failures:

```sql
SELECT e.event_key, d.subscription_id, d.http_status, d.error, d.attempted_at
FROM webhook_deliveries d
JOIN outbox_events e ON e.id = d.outbox_event_id
WHERE d.status = 'failed'
ORDER BY d.attempted_at DESC;
```

## Failure model

PostgreSQL unavailable:
- API readiness fails
- task mutations cannot commit
- therefore no task/event dual-write divergence occurs
- worker pauses naturally because it cannot claim events

Webhook destination unavailable:
- API task write remains successful
- worker retries independently
- repeated failure moves the event to dead letter
- operator can fix the destination and replay

Worker unavailable:
- API remains available
- outbox grows durably
- workers resume from persisted rows when restored

This separation is intentional: webhook availability does not sit on the synchronous task request path.
