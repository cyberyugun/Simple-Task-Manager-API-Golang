FROM golang:1.26.9-alpine AS builder

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w -X go-simple-task-api/internal/buildinfo.Version=${VERSION} -X go-simple-task-api/internal/buildinfo.Commit=${COMMIT} -X go-simple-task-api/internal/buildinfo.BuildTime=${BUILD_TIME}" \
      -o /out/task-api \
      ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/migrate \
      ./cmd/migrate && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/event-replay ./cmd/event-replay

FROM alpine:3.21

RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates && \
    addgroup -S app && \
    adduser -S -G app app

WORKDIR /app

COPY --from=builder /out/task-api /app/task-api
COPY --from=builder /out/migrate /app/migrate
COPY --from=builder /out/worker /app/worker
COPY --from=builder /out/event-replay /app/event-replay
COPY migrations /app/migrations

USER app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/ready >/dev/null || exit 1

ENTRYPOINT ["/app/task-api"]
