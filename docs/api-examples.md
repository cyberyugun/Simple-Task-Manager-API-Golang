# API Request and Response Examples

All examples are on compatibility line `v1` and successful JSON responses include `X-API-Version: v1`.

## Register

```http
POST /api/auth/register
Content-Type: application/json

{
  "name": "Yudi",
  "email": "yudi@example.com",
  "password": "password123"
}
```

Response shape:

```json
{
  "success": true,
  "data": {
    "user": {
      "id": 1,
      "name": "Yudi",
      "email": "yudi@example.com",
      "created_at": "2026-10-03T04:00:00Z",
      "updated_at": "2026-10-03T04:00:00Z"
    },
    "access_token": "<jwt>",
    "refresh_token": "<opaque-token>",
    "token_type": "Bearer",
    "access_token_expires_in": 900,
    "refresh_token_expires_in": 2592000
  }
}
```

## Create task

```http
POST /api/tasks
Authorization: Bearer <access-token>
Content-Type: application/json

{
  "title": "Review API contract",
  "description": "Run compatibility gates"
}
```

## List tasks

```http
GET /api/tasks?page=1&limit=20&sort=created_at&order=desc
Authorization: Bearer <access-token>
```

The authoritative request/response shapes remain the OpenAPI schemas served at `/openapi.yaml`.

## Shared workspace task

Select a shared workspace without changing the existing task URL:

```http
POST /api/tasks
Authorization: Bearer <access-token>
X-Workspace-ID: 42
Content-Type: application/json

{
  "title": "Shared deployment review",
  "description": "Visible to members of workspace 42"
}
```

Omitting `X-Workspace-ID` continues to use the authenticated user's personal workspace.

## Idempotent task creation

```http
POST /api/tasks
Authorization: Bearer <access-token>
X-Idempotency-Key: task-create-20261003-001
Content-Type: application/json

{
  "title": "Create exactly once from the client perspective"
}
```

A retry with the same key and request returns the stored response with:

```text
Idempotent-Replayed: true
```

Reusing the key with different request content returns `409`.

## Create webhook subscription

```http
POST /api/workspaces/42/webhooks
Authorization: Bearer <access-token>
Content-Type: application/json

{
  "url": "https://events.example.com/task-manager",
  "event_types": ["task.created", "task.updated", "task.deleted"]
}
```

The creation response includes a `signing_secret` once. Store it securely; subsequent list responses omit it.
