# Phase 33 — Task Management 2.0 & Collaboration

Phase 33 closes the product-domain gap identified after Phase 32. The enterprise control plane remains unchanged while the task domain gains the collaboration primitives needed by future workflows and notifications.

## Task lifecycle

Supported status values:

- `BACKLOG`
- `TODO`
- `IN_PROGRESS`
- `BLOCKED`
- `IN_REVIEW`
- `DONE`
- `ARCHIVED`

Priority values are `LOW`, `MEDIUM`, `HIGH`, and `URGENT`.

The legacy boolean remains available for API v1 compatibility. `DONE` is written with `completed=true`; all other active states use `completed=false`.

## Planning and hierarchy

Tasks can be grouped into workspace-scoped projects and ordered lists. A task may reference a parent task to represent a subtask hierarchy. Project, list, and parent references are validated inside the same workspace.

## Collaboration

Phase 33 adds:

- Multiple assignees.
- Watchers.
- Workspace labels.
- Comments with owner-only editing and soft deletion.
- Activity timeline.
- Task dependencies with graph-cycle prevention.
- Recurrence configuration.
- Workspace custom-field definitions and typed task values.

## Concurrency and lifecycle

Every task carries a monotonically increasing `version`. Updates use optimistic locking and return a conflict when a stale version is submitted.

User-facing DELETE is a soft delete. Restore clears trash/archive state. The internal repository hard-delete operation remains available to Phase 27 governance retention jobs so privacy/retention semantics are not weakened.

## Filtering

Task listing can filter by:

- `completed`
- `status`
- `priority`
- `project_id`
- `list_id`
- `assignee_id`
- `label_id`
- `archived`
- `deleted`

Sorting adds `status`, `priority`, `due_at`, and `position`.

## Domain events

The transactional event system now recognizes richer task events such as assignment/unassignment, status/priority/due-date changes, comment lifecycle, dependency lifecycle, watcher addition, recurrence update, archive, and restore.

Core task create/update/hard-delete continue to use the existing transactional outbox.

## Migration

`018_task_management_collaboration.sql` adds task v2 columns and collaboration tables while backfilling existing rows from `completed` to `status`.

The migration is additive and idempotent for compatibility with existing deployments.
