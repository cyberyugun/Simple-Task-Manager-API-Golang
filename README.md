# Simple Task Manager API - Golang

A simple REST API built with Go using a Handler -> Service -> Repository architecture.

## Features

- User registration and login
- Password hashing with bcrypt
- HS256 JWT access tokens
- JWT authentication middleware
- Per-user task ownership
- Create, list, read, update, complete, and delete tasks
- In-memory storage for quick local development
- PostgreSQL storage
- SQL migrations
- Docker Compose PostgreSQL setup
- Unit and HTTP handler tests

## Requirements

- Go 1.23+
- Docker and Docker Compose, optional for PostgreSQL

## Environment

Copy `.env.example` or export the variables manually.

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `PORT` | No | `8080` | HTTP server port |
| `DATABASE_URL` | No | empty | PostgreSQL URL. Empty uses in-memory storage. |
| `JWT_SECRET` | Yes | none | JWT signing secret, minimum 32 characters. |

## Install dependencies

```bash
go mod tidy
```

## Run with in-memory storage

Linux/macOS:

```bash
export JWT_SECRET="replace-this-with-a-random-secret-at-least-32-characters"
go run ./cmd/api
```

PowerShell:

```powershell
$env:JWT_SECRET="replace-this-with-a-random-secret-at-least-32-characters"
go run ./cmd/api
```

## Run with PostgreSQL

Start PostgreSQL:

```bash
docker compose up -d
```

For a new Docker volume, files in `migrations/` are executed automatically in filename order.

If the database volume already existed before Phase 7, apply the new migration manually:

```bash
psql "$DATABASE_URL" -f migrations/002_add_users_and_task_ownership.sql
```

Then run the API:

```bash
export DATABASE_URL="postgres://task_user:task_password@localhost:5432/task_manager?sslmode=disable"
export JWT_SECRET="replace-this-with-a-random-secret-at-least-32-characters"
go run ./cmd/api
```

## Authentication flow

```text
Register / Login
      |
      v
Auth Handler
      |
      v
Auth Service
      |
      +---- bcrypt password hash/verify
      |
      +---- User Repository
      |
      v
JWT access token
      |
      v
Authorization: Bearer <token>
      |
      v
Auth Middleware
      |
      v
Authenticated Task API
      |
      v
Task queries scoped by user_id
```

## Public endpoints

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/health` | Health check |
| `POST` | `/api/auth/register` | Create user and return access token |
| `POST` | `/api/auth/login` | Login and return access token |

## Protected task endpoints

All task endpoints require:

```text
Authorization: Bearer <access_token>
```

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/api/tasks` | List current user's tasks |
| `POST` | `/api/tasks` | Create task for current user |
| `GET` | `/api/tasks/{id}` | Get current user's task |
| `PUT` | `/api/tasks/{id}` | Update current user's task |
| `PATCH` | `/api/tasks/{id}/complete` | Mark current user's task completed |
| `DELETE` | `/api/tasks/{id}` | Delete current user's task |

Requests for another user's task return `404` rather than exposing whether that task exists.

## Register

```bash
curl -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"name":"Yudi","email":"yudi@example.com","password":"password123"}'
```

Example response:

```json
{
  "success": true,
  "data": {
    "user": {
      "id": 1,
      "name": "Yudi",
      "email": "yudi@example.com"
    },
    "access_token": "<jwt>",
    "token_type": "Bearer"
  }
}
```

## Login

```bash
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"yudi@example.com","password":"password123"}'
```

## Create an authenticated task

```bash
curl -X POST http://localhost:8080/api/tasks \
  -H "Authorization: Bearer <access_token>" \
  -H "Content-Type: application/json" \
  -d '{"title":"Learn Golang","description":"Build authenticated REST API"}'
```

## Get current user's tasks

```bash
curl http://localhost:8080/api/tasks \
  -H "Authorization: Bearer <access_token>"
```

## Run tests

```bash
go test ./...
```

## Project structure

```text
go-simple-task-api/
├── cmd/api/main.go
├── internal/
│   ├── auth/token.go
│   ├── database/postgres.go
│   ├── handler/
│   │   ├── auth_handler.go
│   │   └── task_handler.go
│   ├── middleware/auth.go
│   ├── model/
│   │   ├── task.go
│   │   └── user.go
│   ├── repository/
│   │   ├── task_repository.go
│   │   ├── user_repository.go
│   │   ├── postgres_task_repository.go
│   │   └── postgres_user_repository.go
│   └── service/
│       ├── auth_service.go
│       └── task_service.go
├── migrations/
│   ├── 001_create_tasks.sql
│   └── 002_add_users_and_task_ownership.sql
├── pkg/response/response.go
├── .env.example
├── docker-compose.yml
├── go.mod
└── README.md
```
