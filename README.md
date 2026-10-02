# Go Simple Task API

A minimal REST API built with Go standard library only.

## Run

```bash
go run ./cmd/api
```

Server runs at:

```text
http://localhost:8080
```

## Endpoints

- `GET /health`
- `GET /api/tasks`
- `POST /api/tasks`
- `GET /api/tasks/{id}`
- `PUT /api/tasks/{id}`
- `PATCH /api/tasks/{id}/complete`
- `DELETE /api/tasks/{id}`

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

## Complete task

```bash
curl -X PATCH http://localhost:8080/api/tasks/1/complete
```
