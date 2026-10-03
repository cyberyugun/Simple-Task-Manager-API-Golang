# Phase 35 — Notification, Reminder & Communication Platform

Phase 35 adds reliable, preference-aware communication driven by task, workflow, billing, and operational activity.

## Core flow

```text
DOMAIN EVENT
  -> USER / ORGANIZATION PREFERENCE
  -> MUTED EVENT + SUPPRESSION CHECK
  -> TIMEZONE / QUIET HOURS / DIGEST SCHEDULING
  -> CHANNEL ROUTER
  -> DELIVERY
  -> RETRY / DEAD LETTER
```

## Channels

The platform has a common sender abstraction for:

- `in_app`
- `email`
- `push`
- `webhook`

The default runtime uses structured-log sender adapters for external channels so local and CI environments remain deterministic. Production providers can implement `NotificationChannelSender` without changing notification orchestration.

## Notification sources

Phase 35 is wired into the existing domain services:

- task assignment -> assigned user;
- comment mention using `@<user_id>` -> mentioned workspace member;
- workflow approval request -> organization owners/admins/delegated admins;
- operational incident creation -> organization owners/admins/delegated admins;
- billing webhook lifecycle events -> organization owners/admins/delegated admins;
- task due/overdue reminder worker -> creator, assignees, and watchers.

All emitters provide deterministic deduplication keys where a repeated event could otherwise create duplicate notifications.

## Preferences

Preferences may be global to a user or organization-scoped.

Supported controls:

- in-app enabled;
- email enabled;
- push enabled;
- webhook enabled;
- immediate / hourly / daily cadence;
- IANA timezone;
- locale;
- quiet-hours start/end;
- muted event types.

Organization-scoped preferences fall back to global user preferences and then to safe defaults.

## Quiet hours and digest cadence

Delivery timestamps are calculated in the user's timezone.

- `immediate`: deliver now unless quiet hours delay it.
- `hourly`: schedule at the next local hour, then apply quiet hours.
- `daily`: schedule for the next local 09:00, then apply quiet hours.

Quiet hours may cross midnight, such as 23:00–06:00.

## Due / overdue reminders

The reminder worker scans incomplete, non-archived, non-deleted tasks due within 24 hours.

Recipients are the union of:

- task creator;
- task assignees;
- task watchers.

A task before its due timestamp emits `task.due_soon`; after the timestamp it emits `task.overdue`. Reminder deduplication includes task, recipient, reminder kind, and due timestamp.

## Retry and dead-letter

Outbound deliveries persist:

- status;
- attempt count;
- maximum attempts;
- scheduled time;
- next retry time;
- last error;
- sent timestamp.

Failed deliveries use bounded exponential backoff. After the configured maximum attempts they become `dead_letter`.

## Templates and localization

`notification_templates` stores:

- template key;
- channel;
- locale;
- immutable version number;
- subject/body;
- active flag.

Migration 020 seeds English templates plus Indonesian examples. Rendering supports deterministic `{{variable}}` substitution and records the selected template version in notification history.

## Suppression

Organization suppression rules can suppress a specific event/channel or use `*` as a wildcard. Suppressed deliveries remain observable with status `suppressed`.

## Observability

Each notification records:

- recipient;
- organization/workspace scope;
- event type;
- title/body;
- structured data;
- dedup key;
- template key/version;
- read timestamp.

Delivery records expose channel-level status, retry state, and dead-letter state. Notification audit events capture creation, preference changes, queueing, suppression, delivery, retry scheduling, dead-lettering, and reads.

The stats endpoint reports totals, unread count, channel counts, delivery-status counts, dead-letter count, and suppression count.

## API

```text
GET  /api/notifications
POST /api/notifications/{notification_id}/read

GET  /api/notifications/preferences
PUT  /api/notifications/preferences
     ?organization_id={id}

GET  /api/notifications/stats
     ?organization_id={id}
```

## Worker configuration

```text
NOTIFICATION_POLL_INTERVAL=2s
NOTIFICATION_REMINDER_POLL_INTERVAL=1m
```

The delivery loop processes ready channel deliveries. The reminder loop creates due/overdue notification signals. Both are separate from the existing outbox/webhook worker and workflow-resume worker.

## Persistence

Migration `020_notification_platform.sql` adds:

- `notification_preferences`
- `notification_templates`
- `notifications`
- `notification_deliveries`
- `notification_suppression_rules`
- `notification_audit_events`

## Safety boundary

Notification templates do not execute arbitrary code. External delivery providers are explicit allowlisted sender implementations. Organization-scoped preferences require organization membership. Mention recipients must be workspace members.

## Definition of done

Phase 35 is complete when:

- in-app + email abstraction is active;
- push/webhook share the same provider abstraction;
- task due/overdue reminders run in the worker;
- assignment, mention, workflow approval, incident, and billing lifecycle notifications are wired;
- preferences, locale, timezone, quiet hours, cadence, and suppression are enforced;
- deduplication, retry and dead-letter persistence are covered;
- notification audit and stats are available;
- PostgreSQL/in-memory parity tests pass;
- migration idempotency, OpenAPI compatibility, Docker runtime smoke, CI, and Security gates are green.
