# Simple Task Manager API - Golang

A REST API built with Go using a Handler -> Service -> Repository architecture.

## Features

- Registration/login with bcrypt password hashing
- Short-lived HS256 JWT access tokens
- 256-bit opaque refresh tokens stored only as SHA-256 hashes
- Refresh-token rotation and replay rejection
- Logout/revocation for refresh sessions
- Configurable access/refresh token TTLs
- Per-user task ownership
- Task CRUD and complete action
- Pagination, search, filtering, sorting, and ordering
- In-memory and PostgreSQL repositories
- SQL migrations
- OpenAPI 3.1 + Swagger UI
- Multi-stage production Docker image
- Docker Compose API + PostgreSQL stack
- Graceful shutdown and HTTP server timeouts
- Environment/config validation
- Unit, handler, PostgreSQL integration, and container smoke tests
- GitHub Actions CI

## Environment

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `PORT` | No | `8080` | HTTP server port, 1-65535 |
| `DATABASE_URL` | No | empty | PostgreSQL URL; empty uses in-memory storage |
| `JWT_SECRET` | Yes | none | JWT signing secret, minimum 32 characters |
| `ACCESS_TOKEN_TTL` | No | `15m` | Access JWT lifetime |
| `REFRESH_TOKEN_TTL` | No | `720h` | Refresh token lifetime; must exceed access TTL |
| `SHUTDOWN_TIMEOUT` | No | `10s` | Graceful shutdown timeout |

## Run locally

Install dependencies:

```bash
go mod tidy
```

Run with in-memory storage:

```bash
export JWT_SECRET="replace-this-with-a-random-secret-at-least-32-characters"
go run ./cmd/api
```

PowerShell:

```powershell
$env:JWT_SECRET="replace-this-with-a-random-secret-at-least-32-characters"
go run ./cmd/api
```

## Run the complete Docker stack

Copy the example environment file if you want to customize values:

```bash
cp .env.example .env
```

Set a strong local secret and start API + PostgreSQL:

```bash
export JWT_SECRET="replace-this-with-a-random-secret-at-least-32-characters"
docker compose up -d --build
```

Then:

```text
API:         http://localhost:8080
Health:      http://localhost:8080/health
Swagger UI:  http://localhost:8080/docs
OpenAPI:     http://localhost:8080/openapi.yaml
PostgreSQL:  localhost:5432
```

Check containers:

```bash
docker compose ps
```

View logs:

```bash
docker compose logs -f api
```

Stop the stack:

```bash
docker compose down
```

Delete the local database volume too:

```bash
docker compose down -v
```

The Compose file contains a development-only fallback JWT secret. Set `JWT_SECRET` explicitly outside local development.

## Docker image

Build directly:

```bash
docker build -t simple-task-manager-api .
```

The Dockerfile uses a multi-stage build, produces a stripped Linux binary, and runs the final container as a non-root user.

## PostgreSQL migrations

For a fresh Compose database volume, migrations are applied automatically.

For an existing database:

```bash
psql "$DATABASE_URL" -f migrations/002_add_users_and_task_ownership.sql
psql "$DATABASE_URL" -f migrations/003_task_list_indexes.sql
```

## API documentation

Swagger UI supports the Bearer JWT security scheme. Register/login, copy the returned access token, select **Authorize**, and call protected task endpoints from the browser.

## Public endpoints

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/health` | Liveness health check |
| `GET` | `/docs` | Swagger UI |
| `GET` | `/openapi.yaml` | OpenAPI specification |
| `POST` | `/api/auth/register` | Register |
| `POST` | `/api/auth/login` | Login |
| `POST` | `/api/auth/refresh` | Rotate refresh token and issue a new token pair |
| `POST` | `/api/auth/logout` | Revoke a refresh token |

## Refresh token flow

Register and login return both an access token and a refresh token. Access tokens default to 15 minutes; refresh tokens default to 30 days.

```json
{
  "access_token": "<jwt>",
  "refresh_token": "<opaque-token>",
  "token_type": "Bearer",
  "access_token_expires_in": 900,
  "refresh_token_expires_in": 2592000
}
```

Rotate a refresh token:

```bash
curl -X POST http://localhost:8080/api/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"<refresh_token>"}'
```

Every successful refresh invalidates the previous refresh token. Reusing an old token returns `401`.

Logout:

```bash
curl -X POST http://localhost:8080/api/auth/logout \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"<refresh_token>"}'
```

Logout revokes the refresh session. An access JWT already issued remains valid only until its short access TTL expires.

## Protected task endpoints

All task endpoints require:

```text
Authorization: Bearer <access_token>
```

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/api/tasks` | List current user's tasks |
| `POST` | `/api/tasks` | Create task |
| `GET` | `/api/tasks/{id}` | Get task |
| `PUT` | `/api/tasks/{id}` | Update task |
| `PATCH` | `/api/tasks/{id}/complete` | Mark task completed |
| `DELETE` | `/api/tasks/{id}` | Delete task |

## Task list query parameters

| Parameter | Default | Allowed values |
| --- | --- | --- |
| `page` | `1` | Positive integer |
| `limit` | `10` | `1` to `100` |
| `search` | empty | Title/description search |
| `completed` | empty | `true`, `false` |
| `sort` | `created_at` | `id`, `title`, `created_at`, `updated_at`, `completed` |
| `order` | `desc` | `asc`, `desc` |

## Tests

Unit tests:

```bash
go test ./...
```

PostgreSQL integration tests:

```bash
TEST_DATABASE_URL="postgres://task_user:task_password@localhost:5432/task_manager_test?sslmode=disable"   go test -tags=integration ./internal/repository -run Integration -v
```

## CI pipeline

The GitHub Actions workflow now has three stages:

```text
Unit Test and Build
        |
        v
PostgreSQL Integration Test
        |
        v
Docker Compose Smoke Test
```

It validates formatting, `go vet`, race-enabled unit tests, compilation, real PostgreSQL repository behavior, refresh-token rotation/replay protection, Docker image construction, container health, OpenAPI, and Swagger UI.

## Graceful shutdown

The API handles `SIGINT` and `SIGTERM`. On shutdown it stops accepting new requests and gives active requests up to `SHUTDOWN_TIMEOUT` to finish before forcing the server closed.


## Auth migration

Existing PostgreSQL databases must also apply:

```bash
psql "$DATABASE_URL" -f migrations/004_refresh_tokens.sql
```

Fresh Docker Compose volumes apply migration 004 automatically.
