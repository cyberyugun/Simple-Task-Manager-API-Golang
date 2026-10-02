# Simple Task Manager API - Golang

A REST API built with Go using a Handler -> Service -> Repository architecture.

## Features

- Registration/login with bcrypt password hashing
- Short-lived HS256 JWT access tokens
- 256-bit opaque refresh tokens stored only as SHA-256 hashes
- Refresh-token rotation and replay rejection
- Logout/revocation for refresh sessions
- Configurable access/refresh token TTLs
- Change password with refresh-session revocation
- Forgot/reset password with one-time hashed action tokens
- Email verification with one-time hashed action tokens
- Active session/device listing and per-session revocation
- Logout all devices
- Per-IP authentication rate limiting with optional Redis-backed distributed storage
- Structured JSON request/application logs
- Correlation/request IDs via `X-Request-ID`
- Liveness, dependency readiness, and Prometheus metrics endpoints
- Versioned deployment migration runner with PostgreSQL advisory locking
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
| `REDIS_URL` | No | empty | Redis URL; when set, auth rate limits are shared across API instances |
| `JWT_SECRET` | Yes | none | JWT signing secret, minimum 32 characters |
| `LOG_LEVEL` | No | `info` | `debug`, `info`, `warn`, or `error` |
| `READINESS_TIMEOUT` | No | `2s` | Timeout for dependency readiness probes |
| `ACCESS_TOKEN_TTL` | No | `15m` | Access JWT lifetime |
| `REFRESH_TOKEN_TTL` | No | `720h` | Refresh token lifetime; must exceed access TTL |
| `PASSWORD_RESET_TTL` | No | `30m` | Password reset token lifetime |
| `EMAIL_VERIFICATION_TTL` | No | `24h` | Email verification token lifetime |
| `AUTH_RATE_LIMIT_REQUESTS` | No | `20` | Auth requests allowed per client IP/window |
| `AUTH_RATE_LIMIT_WINDOW` | No | `1m` | Authentication rate-limit window |
| `RATE_LIMIT_FAIL_OPEN` | No | `true` | Allow auth requests if Redis fails after startup |
| `EXPOSE_AUTH_TOKENS` | No | `false` | Show reset/verification tokens for local/testing only |
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
Liveness:    http://localhost:8080/health
Readiness:   http://localhost:8080/ready
Metrics:     http://localhost:8080/metrics
Swagger UI:  http://localhost:8080/docs
OpenAPI:     http://localhost:8080/openapi.yaml
PostgreSQL:  localhost:5432
Redis:       localhost:6379
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

The Dockerfile uses a multi-stage build, produces stripped API and migration binaries, includes SQL migrations, and runs the final container as a non-root user. Docker Compose starts PostgreSQL and Redis, runs the migration job to completion, then starts the API.

## PostgreSQL migrations

For Docker Compose, the dedicated `migrate` service applies pending migrations before the API starts.

Run migrations manually:

```bash
DATABASE_URL="$DATABASE_URL" MIGRATIONS_DIR=migrations go run ./cmd/migrate
```

The migration runner:

- creates `schema_migrations`
- serializes concurrent migration runs with a PostgreSQL advisory lock
- applies migration files in lexical order
- records each successfully committed migration
- safely skips versions that were already applied

## API documentation

Swagger UI supports the Bearer JWT security scheme. Register/login, copy the returned access token, select **Authorize**, and call protected task endpoints from the browser.

## Public endpoints

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/health` | Liveness health check |
| `GET` | `/ready` | PostgreSQL/Redis readiness check |
| `GET` | `/metrics` | Prometheus text metrics |
| `GET` | `/docs` | Swagger UI |
| `GET` | `/openapi.yaml` | OpenAPI specification |
| `POST` | `/api/auth/register` | Register |
| `POST` | `/api/auth/login` | Login |
| `POST` | `/api/auth/refresh` | Rotate refresh token and issue a new token pair |
| `POST` | `/api/auth/logout` | Revoke a refresh token |
| `POST` | `/api/auth/forgot-password` | Request password-reset instructions |
| `POST` | `/api/auth/reset-password` | Reset password with one-time token |
| `POST` | `/api/auth/email-verification/confirm` | Confirm email with one-time token |

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

## Account security endpoints

These endpoints require a valid access JWT:

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/api/auth/change-password` | Change password and revoke all refresh sessions |
| `POST` | `/api/auth/logout-all` | Revoke all refresh sessions |
| `GET` | `/api/auth/sessions` | List active sessions/devices |
| `DELETE` | `/api/auth/sessions/{id}` | Revoke one session |
| `POST` | `/api/auth/email-verification/request` | Create email verification instructions |

Session responses expose the session ID, user agent, IP address, created time, last-used time, and expiry to the authenticated owner only. Refresh-token hashes are never returned.

### Password reset

`POST /api/auth/forgot-password` deliberately returns the same generic message whether or not an account exists, reducing email enumeration risk. Reset tokens are random opaque values; only SHA-256 hashes are stored, and tokens are single-use.

For local/CI testing only:

```bash
export EXPOSE_AUTH_TOKENS=true
```

This adds `development_token` to password-reset and email-verification request responses. Keep this disabled in production. A production deployment should deliver those tokens through an email provider.

### Email verification

Email verification is implemented as an optional account state. Current task endpoints do not require a verified email. The flow is:

```text
Authenticated verification request
        |
        v
One-time verification token
        |
        v
POST /api/auth/email-verification/confirm
        |
        v
email_verified_at is set
```

### Authentication rate limiting

Sensitive authentication routes use a fixed-window limiter keyed by the direct client IP. Without `REDIS_URL`, the limiter is process-local. With `REDIS_URL`, counters are stored atomically in Redis so limits are shared across API replicas.

The server intentionally does not trust `X-Forwarded-For` by default. If deployed behind a trusted reverse proxy, proxy-aware client IP handling should be added explicitly rather than trusting forwarded headers globally.

`RATE_LIMIT_FAIL_OPEN=true` keeps auth endpoints available if Redis fails after the API has started. The readiness probe still fails while Redis is unavailable, allowing an orchestrator to stop routing new traffic to the instance.

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

## Observability and operations

Every request receives an `X-Request-ID`. A valid incoming ID is preserved; otherwise the API generates a random 128-bit ID. Structured JSON request logs include the request ID, method, path, status, response bytes, duration, and direct remote IP.

```text
GET /health   -> process liveness only
GET /ready    -> PostgreSQL + Redis readiness when configured
GET /metrics  -> Prometheus text exposition
```

The metrics endpoint currently exports aggregate request count/duration, in-flight requests, recovered panics, rate-limit rejections, readiness checks, and readiness failures. It deliberately avoids raw URL-path labels to prevent high-cardinality metrics from task IDs.

Panic recovery is centralized in HTTP middleware. Recovered panics return HTTP 500, increment the panic metric, and emit a structured error log with the request ID and stack trace.

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

It validates formatting, `go vet`, race-enabled unit tests, compilation, migration-runner idempotency, real PostgreSQL repository behavior, refresh-token rotation/replay protection, one-time reset/verification tokens, session revocation, Redis-backed rate limiting, readiness/metrics endpoints, structured request logs, Docker image construction, OpenAPI, and Swagger UI.

## Graceful shutdown

The API handles `SIGINT` and `SIGTERM`. On shutdown it stops accepting new requests and gives active requests up to `SHUTDOWN_TIMEOUT` to finish before forcing the server closed.


## Existing databases

Use the migration runner for existing databases instead of applying individual files manually:

```bash
DATABASE_URL="$DATABASE_URL" MIGRATIONS_DIR=migrations go run ./cmd/migrate
```

Docker Compose uses the same runner automatically before starting the API.
