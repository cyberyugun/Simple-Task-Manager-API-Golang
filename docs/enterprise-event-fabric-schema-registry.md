# Phase 39 — Enterprise Event Fabric & Schema Registry

Phase 39 unifies the existing transactional outbox, notification consumers, workflow/integration event flows and future broker adapters behind a governed event contract.

## Runtime flow

```text
DOMAIN EVENT
  -> TRANSACTIONAL POSTGRES OUTBOX
  -> SCHEMA REGISTRY VALIDATION
  -> ROUTING RULES
  -> DURABLE SUBSCRIPTION DELIVERY
  -> CONSUMER OFFSET
  -> RETENTION

FAILED DELIVERY
  -> RETRY/BACKOFF
  -> DEAD LETTER
  -> REDRIVE / RANGE REPLAY
```

The existing PostgreSQL outbox remains the default event source. Phase 39 adds a second durable delivery layer so an outbox event can be routed independently to multiple governed consumers without coupling task writes to downstream processing.

## Schema registry

Schemas are versioned per workspace and event type. The lightweight schema contract supports object properties with JSON types and required-field validation. Supported compatibility modes are:

- `none`
- `backward`
- `forward`
- `full`

Publishing a later version performs compatibility checks against the previous version. Existing versions remain available for old outbox events until explicitly deprecated. The event worker validates an event against the exact active schema version when that event type is registered. Event types that have not yet been registered continue to flow, preserving compatibility with legacy platform events.

Schema ownership metadata and explicit deprecation provide the event ownership/deprecation lifecycle recommended by the roadmap.

## Correlation and causation

`outbox_events` now persists:

- `correlation_id`
- `causation_id`

Legacy/domain writes default correlation to the event key and causation to empty. Both fields are carried into durable event-fabric deliveries and worker logs. Future domain commands can propagate an upstream correlation and causation identifier without changing the fabric contract.

## Routing and durable subscriptions

A subscription defines a stable `consumer_key`, event patterns, adapter, retry ceiling and retention window. Patterns support exact event names, domain wildcards such as `task.*`, and `*`.

Routes map an event pattern to a subscription. A delivery is uniquely keyed by `(subscription_id, outbox_event_id)`, making routing idempotent when an outbox event is retried.

Successful delivery advances a durable consumer offset with the source outbox event ID/key and occurrence timestamp.

## Retry, DLQ and replay

Delivery processing uses `FOR UPDATE SKIP LOCKED` in PostgreSQL, stale-lock recovery and bounded exponential retry. Exhausted deliveries enter `dead_letter` and are isolated from other subscriptions.

Operators can:

- redrive one delivery;
- replay an event-ID range for a durable subscription;
- inspect pending/retry/delivered/dead-letter deliveries;
- inspect consumer offsets.

Range replay can materialize historical outbox events that match the subscription even when the subscription did not exist when the original event was emitted.

## Adapters

The provider-neutral adapter catalog contains:

- `postgres_outbox` — configured by default;
- `kafka`;
- `rabbitmq`;
- `nats`;
- `cloud_event_bus`.

The PostgreSQL durable adapter is implemented in this phase. The other adapters are extension boundaries and report `configured=false` until an adapter is registered by the deployment. Sending to an unconfigured optional adapter retries and ultimately isolates that delivery in the DLQ rather than dropping the event.

## Worker configuration

```text
EVENT_FABRIC_POLL_INTERVAL=2s
EVENT_FABRIC_RETENTION_POLL_INTERVAL=1h
EVENT_FABRIC_BATCH_SIZE=50
```

The normal outbox worker validates/routes events through `EventFabricService.Consume`. A separate event-fabric worker drains durable deliveries and retention.

## API

Workspace administrators can use:

- `GET /api/workspaces/{id}/event-fabric/adapters`
- `GET|POST /api/workspaces/{id}/event-fabric/schemas`
- `POST /api/workspaces/{id}/event-fabric/schemas/{schema_id}/deprecate`
- `GET|POST /api/workspaces/{id}/event-fabric/subscriptions`
- `PATCH /api/workspaces/{id}/event-fabric/subscriptions/{subscription_id}`
- `GET /api/workspaces/{id}/event-fabric/subscriptions/{subscription_id}/offset`
- `POST /api/workspaces/{id}/event-fabric/subscriptions/{subscription_id}/replay`
- `GET|POST /api/workspaces/{id}/event-fabric/routes`
- `GET /api/workspaces/{id}/event-fabric/deliveries`
- `POST /api/workspaces/{id}/event-fabric/deliveries/{delivery_id}/redrive`

## Delivery guarantees

The platform provides at-least-once durable routing. Consumers must remain idempotent. The fabric prevents duplicate delivery rows for a subscription/outbox pair, but a worker crash after external publish and before acknowledgement can still cause a retry; this is expected for an at-least-once design.
