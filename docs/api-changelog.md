# API Changelog

## 1.5.0 — Phase 24

Compatibility line: **v1**.

Additive asynchronous-processing release:

- adds optional `X-Idempotency-Key` on `POST /api/tasks`
- adds workspace webhook subscription list/create/delete endpoints
- adds signing-secret-on-create behavior
- adds task domain events `task.created`, `task.updated`, and `task.deleted`
- adds transactional PostgreSQL outbox and background delivery workers
- adds retry, dead-letter, and replay semantics
- preserves existing task behavior when no idempotency key is supplied
- preserves all existing v1 paths, operation IDs, required request properties, and response fields


## 1.4.0 — Phase 23

Compatibility line: **v1**.

Additive authorization and tenancy release:

- adds personal and shared workspaces
- adds owner/admin/member RBAC
- adds optional `X-Workspace-ID` selection for existing task endpoints
- preserves no-header task behavior by resolving the user's personal workspace
- adds workspace membership-management APIs
- adds owner/admin workspace audit API
- adds optional `workspace_id` to task responses
- adds cross-tenant live contract validation
- preserves existing v1 endpoint paths, operation IDs, required request fields, and existing response fields

The migration retains legacy task ownership fields during the rolling-deployment compatibility window so old and new pods cannot accidentally cross tenant boundaries.


## 1.3.0 — Phase 22

Compatibility line: **v1**.

Non-breaking governance release:

- declares the existing public API as compatibility line v1
- adds `X-API-Version: v1` and `API-Supported-Versions: v1` runtime headers
- documents existing `429 Too Many Requests` behavior for register, login, refresh, and logout
- adds backward-compatibility diff checks
- adds consumer-driven contract expectations
- adds live provider/schema contract validation
- adds generated TypeScript SDK artifacts
- adds formal versioning and deprecation policy

No existing endpoint, request property, response property, operation ID, or authentication requirement was removed or incompatibly changed.

## 1.2.0

Pre-Phase-22 OpenAPI contract for system, authentication, session/account security, and task-management APIs.
