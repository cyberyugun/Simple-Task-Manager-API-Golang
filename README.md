# Simple Task Manager API - Golang

A REST API built with Go using a Handler -> Service -> Repository architecture.

## Features

- Registration/login with bcrypt password hashing
- HS256 JWT authentication
- Per-user task ownership
- Task CRUD and complete action
- Pagination, search, completion filter, sorting, and ordering
- In-memory and PostgreSQL repositories
- SQL migrations and Docker Compose
- OpenAPI 3.1 specification
- Interactive Swagger UI
- GitHub Actions CI for format, vet, tests, and build
- Unit and HTTP handler tests

## Environment

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `PORT` | No | `8080` | HTTP server port |
| `DATABASE_URL` | No | empty | PostgreSQL URL; empty uses in-memory storage |
| `JWT_SECRET` | Yes | none | JWT signing secret, minimum 32 characters |

Install dependencies:

```bash
go mod tidy
```

Run in-memory:

```bash
export JWT_SECRET="replace-this-with-a-random-secret-at-least-32-characters"
go run ./cmd/api
```

PowerShell:

```powershell
$env:JWT_SECRET="replace-this-with-a-random-secret-at-least-32-characters"
go run ./cmd/api
```

## API documentation

After the API starts:

```text
Swagger UI: http://localhost:8080/docs
OpenAPI:    http://localhost:8080/openapi.yaml
```

Swagger UI supports the Bearer JWT security scheme, so after register/login you can use the **Authorize** button and test protected endpoints directly from the browser.

The UI assets are loaded from the pinned `swagger-ui-dist@5.33.0` CDN release. The OpenAPI YAML itself is embedded in the Go binary.

## PostgreSQL

Start PostgreSQL:

```bash
docker compose up -d
```

For an existing database, apply migrations not yet executed:

```bash
psql "$DATABASE_URL" -f migrations/002_add_users_and_task_ownership.sql
psql "$DATABASE_URL" -f migrations/003_task_list_indexes.sql
```

Then:

```bash
export DATABASE_URL="postgres://task_user:task_password@localhost:5432/task_manager?sslmode=disable"
export JWT_SECRET="replace-this-with-a-random-secret-at-least-32-characters"
go run ./cmd/api
```

## Public endpoints

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/health` | Health check |
| `GET` | `/docs` | Swagger UI |
| `GET` | `/openapi.yaml` | OpenAPI specification |
| `POST` | `/api/auth/register` | Register and return access token |
| `POST` | `/api/auth/login` | Login and return access token |

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

Another user's task returns `404`.

## Task list query parameters

`GET /api/tasks` supports:

| Parameter | Default | Allowed values |
| --- | --- | --- |
| `page` | `1` | Positive integer |
| `limit` | `10` | `1` to `100` |
| `search` | empty | Searches title and description |
| `completed` | empty | `true`, `false` |
| `sort` | `created_at` | `id`, `title`, `created_at`, `updated_at`, `completed` |
| `order` | `desc` | `asc`, `desc` |

Example:

```bash
curl "http://localhost:8080/api/tasks?search=golang&completed=false&sort=title&order=asc"   -H "Authorization: Bearer <access_token>"
```

## Register

```bash
curl -X POST http://localhost:8080/api/auth/register   -H "Content-Type: application/json"   -d '{"name":"Yudi","email":"yudi@example.com","password":"password123"}'
```

## Login

```bash
curl -X POST http://localhost:8080/api/auth/login   -H "Content-Type: application/json"   -d '{"email":"yudi@example.com","password":"password123"}'
```

## Continuous integration

`.github/workflows/ci.yml` runs on pushes to `main` and pull requests. It performs:

```text
gofmt check
go vet ./...
go test -race -coverprofile=coverage.out ./...
go build ./cmd/api
```

The workflow uses the Go version declared in `go.mod`.

## Run tests

```bash
go test ./...
```

## Migrations

```text
001_create_tasks.sql
002_add_users_and_task_ownership.sql
003_task_list_indexes.sql
```
