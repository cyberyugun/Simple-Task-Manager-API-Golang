package main

import (
	"log/slog"
	"os"
	"strings"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/migrations"
	"go-simple-task-api/internal/observability"
)

func main() {
	logger := observability.NewJSONLogger(os.Getenv("LOG_LEVEL"))
	slog.SetDefault(logger)

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		logger.Error("migration_failed", "error", "DATABASE_URL is required")
		os.Exit(1)
	}

	dir := strings.TrimSpace(os.Getenv("MIGRATIONS_DIR"))
	if dir == "" {
		dir = "migrations"
	}

	db, err := appdb.OpenPostgres(databaseURL)
	if err != nil {
		logger.Error("migration_failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := migrations.Run(db, dir); err != nil {
		logger.Error("migration_failed", "error", err)
		os.Exit(1)
	}

	logger.Info("migrations_complete", "directory", dir)
}
