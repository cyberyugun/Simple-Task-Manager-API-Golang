# Go Simple Task API

A simple Task Manager REST API written in Go with a Handler -> Service -> Repository architecture.

The app supports two storage modes:

- In-memory storage by default.
- PostgreSQL when `DATABASE_URL` is configured.

## Features

- Create task
- List tasks
- Get task by ID
- Update task
- Mark task as completed
- Delete task
- Health endpoint
- In-memory repository
- PostgreSQL repository
- Unit and HTTP handler tests
- Docker Compose PostgreSQL setup

## Requirements

- Go 1.23+
- Docker and Docker Compose, optional for PostgreSQL

## Install dependencies

```bash
go mod tidy
```

## Run with in-memory storage

No database configuration is required:

```bash
go run ./cmd/api
```

The API runs at:

```text
http://localhost:8080
```

## Run with PostgreSQL

Start PostgreSQL:

```bash
docker compose up -d
```

The compose setup automatically executes the SQL files in `migrations/` when the database volume is first created.

Set the database connection string.

Linux/macOS:

```bash
export DATABASE_URL="postgres://task_user:task_password@localhost:5432/task_manager?sslmode=disable"
go run ./cmd/api
```

PowerShell:

```powershell
$env:DATABASE_URL="postgres://task_user:task_password@localhost:5432/task_manager?sslmode=disable"
go run ./cmd/api
```

For an existing PostgreSQL server, run the migration manually:

```bash
psql "$DATABASE_URL" -f migrations/001_create_tasks.sql
```

## Environment variables

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `PORT` | No | `8080` | HTTP server port |
| `DATABASE_URL` | No | empty | PostgreSQL connection URL; empty uses in-memory storage |

Example values are available in `.env.example`.

## Endpoints

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/health` | Health check |
| `GET` | `/api/tasks` | List tasks |
| `POST` | `/api/tasks` | Create task |
| `GET` | `/api/tasks/{id}` | Get task |
| `PUT` | `/api/tasks/{id}` | Update task |
| `PATCH` | `/api/tasks/{id}/complete` | Mark task completed |
| `DELETE` | `/api/tasks/{id}` | Delete task |

## Create task

```bash
curl -X POST http://localhost:8080/api/tasks \
  -H "Content-Type: application/json" \
  -d '{"title":"Learn Golang","description":"Build REST API"}'
```

## Get tasks

```bash
curl http://localhost:8080/api/tasks
```

## Update task

```bash
curl -X PUT http://localhost:8080/api/tasks/1 \
  -H "Content-Type: application/json" \
  -d '{"title":"Learn PostgreSQL","description":"Persist tasks in PostgreSQL"}'
```

## Complete task

```bash
curl -X PATCH http://localhost:8080/api/tasks/1/complete
```

## Delete task

```bash
curl -X DELETE http://localhost:8080/api/tasks/1
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
│   ├── database/postgres.go
│   ├── handler/
│   ├── model/
│   ├── repository/
│   │   ├── task_repository.go
│   │   └── postgres_task_repository.go
│   └── service/
├── migrations/
│   └── 001_create_tasks.sql
├── pkg/response/
├── .env.example
├── docker-compose.yml
├── go.mod
└── README.md
```
