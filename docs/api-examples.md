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
