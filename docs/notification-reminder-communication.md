# Phase 35 — Notification, Reminder & Communication Platform

Phase 35 adds a durable communication layer on top of the task, workflow, and event-driven foundations delivered in earlier phases.

## Capabilities

The platform supports:

- in-app notification history with unread counts and read/read-all actions;
- email delivery through an operator-configured HTTP provider;
- push delivery through an operator-configured HTTP provider;
- signed per-user webhook notification endpoints;
- task assignment and watcher notifications from domain events;
- comment mention notifications using `<@123>`, `@{123}`, or `@123` user-ID notation;
- due-soon and overdue reminders;
- workflow approval notifications for organization owners, admins, and delegated admins;
- daily and weekly unread-notification digests;
- timezone-aware quiet hours;
- per-channel and per-event preference enforcement;
- retry with exponential backoff and dead-letter state;
- manual dead-letter replay;
- per-user deduplication keys;
- organization-scoped template draft/version/publish lifecycle;
- locale fallback and built-in English/Indonesian templates.

## Event fanout

The existing outbox event worker now supports event consumers. Notification fanout runs before an outbox event is marked processed. When another delivery target causes the event to retry, notification dedupe keys prevent duplicate user notifications.

Current event mappings include:

```text
task.assigned        -> assigned user
task.watcher.added   -> added watcher
task.comment.created -> explicitly mentioned workspace members
task.comment.updated -> explicitly mentioned workspace members
```

The event worker continues to deliver the existing workspace webhooks after notification consumers succeed.

## Reminder and approval scans

The notification worker periodically evaluates durable state:

```text
open task + due_at <= reminder horizon
  -> assignees + creator
  -> due-soon or overdue notification

pending workflow_approval
  -> organization owner/admin/delegated_admin
  -> approval-required notification
```

Due-soon reminders are deduplicated by task, user, and due timestamp. Overdue reminders are deduplicated per task, user, and recipient-local calendar date.

## Preferences

Each user has one notification preference record. Defaults are:

```json
{
  "locale": "en",
  "timezone": "UTC",
  "quiet_hours_enabled": false,
  "quiet_start": "22:00",
  "quiet_end": "07:00",
  "digest_frequency": "off",
  "digest_hour": 8,
  "channels": {
    "in_app": true,
    "email": false,
    "push": false,
    "webhook": false
  },
  "events": {}
}
```

A missing event key means enabled. Setting an event key to `false` suppresses that notification class.

Quiet hours delay external email/push/webhook delivery until the recipient's configured local quiet-hours end. In-app records remain durable immediately.

## Digests

`daily` digests are generated around the configured local `digest_hour`. `weekly` digests are generated on Monday at that local hour. Digest counts exclude prior digest notifications and use unread notification state.

## Channel endpoints

Email can use the user's account email when no explicit email endpoint exists. Push and webhook channels require registered endpoints.

Webhook endpoints require HTTPS unless development insecure-webhook mode is enabled. A generated `signing_secret` is returned only on endpoint creation and is omitted from later list responses. They are signed with:

```text
X-Notification-Id
X-Notification-Event
X-Notification-Timestamp
X-Notification-Signature: v1=<HMAC-SHA256>
```

## Email and push providers

Email and push are provider-neutral HTTP adapters configured through:

```text
NOTIFICATION_EMAIL_PROVIDER_URL
NOTIFICATION_PUSH_PROVIDER_URL
NOTIFICATION_PROVIDER_TOKEN
NOTIFICATION_HTTP_TIMEOUT
```

The provider token is optional and, when present, is sent as a Bearer token. A channel is not enqueued when its provider URL is not configured.

## Retry and dead-letter

External deliveries are persisted independently from in-app notifications. Workers claim ready rows with `FOR UPDATE SKIP LOCKED`, apply exponential backoff, and move exhausted deliveries to `dead_letter`. Users can replay their own dead-lettered deliveries through the notification delivery retry API.

## Template versioning and localization

Organization notification administrators can create draft template versions and publish them. Publishing a new version archives the previous published version for the same:

```text
organization + template_key + locale + channel
```

Rendering first checks the organization override, then global stored templates, then built-in localized defaults. English is the final locale fallback.

Built-in template keys currently include:

- `task.assigned`
- `task.watcher_added`
- `task.mention`
- `task.due_soon`
- `task.overdue`
- `workflow.approval`
- `digest.summary`

Template variables use deterministic `{{variable}}` substitution. Arbitrary code execution is not supported.

## API

User communication endpoints:

```text
GET    /api/notifications
GET    /api/notifications/preferences
PUT    /api/notifications/preferences

GET    /api/notifications/endpoints
POST   /api/notifications/endpoints
DELETE /api/notifications/endpoints/{endpoint_id}

POST   /api/notifications/items/{notification_id}/read
POST   /api/notifications/read-all

GET    /api/notifications/deliveries
POST   /api/notifications/deliveries/{delivery_id}/retry
```

Organization template governance:

```text
GET  /api/organizations/{id}/notification-templates
POST /api/organizations/{id}/notification-templates
POST /api/organizations/{id}/notification-templates/{template_id}/publish
```

## Persistence

Migration `020_notification_reminder_communication.sql` adds:

- `notification_preferences`
- `notification_endpoints`
- `notification_templates`
- `notifications`
- `notification_deliveries`

## Worker configuration

```text
NOTIFICATION_POLL_INTERVAL=2s
NOTIFICATION_SCHEDULE_POLL_INTERVAL=1m
NOTIFICATION_BATCH_SIZE=50
NOTIFICATION_HTTP_TIMEOUT=10s
NOTIFICATION_REMINDER_HORIZON=24h
```

The worker starts both the event consumer and scheduled/delivery processor. This keeps notification generation durable while reusing the existing outbox retry semantics for domain-event fanout.
