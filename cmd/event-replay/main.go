package main

import (
	"log/slog"
	"os"
	"strings"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/observability"
	"go-simple-task-api/internal/repository"
)

func main() {
	logger := observability.NewJSONLogger(os.Getenv("LOG_LEVEL"))
	slog.SetDefault(logger)

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	eventKey := strings.TrimSpace(os.Getenv("EVENT_ID"))
	if len(os.Args) > 1 {
		eventKey = strings.TrimSpace(os.Args[1])
	}
	if databaseURL == "" || eventKey == "" {
		logger.Error("event_replay_configuration_failed", "error", "DATABASE_URL and EVENT_ID (or first argument) are required")
		os.Exit(1)
	}

	db, err := appdb.OpenPostgres(databaseURL)
	if err != nil {
		logger.Error("event_replay_database_failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := repository.NewPostgresEventRepository(db).Replay(eventKey, time.Now().UTC()); err != nil {
		logger.Error("event_replay_failed", "event_id", eventKey, "error", err)
		os.Exit(1)
	}
	logger.Info("event_replay_queued", "event_id", eventKey)
}
