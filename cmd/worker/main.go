package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/observability"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/worker"
)

func main() {
	logger := observability.NewJSONLogger(os.Getenv("LOG_LEVEL"))
	slog.SetDefault(logger)

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		logger.Error("worker_configuration_error", "error", "DATABASE_URL is required")
		os.Exit(1)
	}
	db, err := appdb.OpenPostgres(databaseURL)
	if err != nil {
		logger.Error("worker_database_error", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	cfg := worker.Config{
		BatchSize:      envInt("WORKER_BATCH_SIZE", 25),
		PollInterval:   envDuration("WORKER_POLL_INTERVAL", time.Second),
		RequestTimeout: envDuration("WEBHOOK_REQUEST_TIMEOUT", 10*time.Second),
		MaxAttempts:    envInt("WEBHOOK_MAX_ATTEMPTS", 8),
		BaseBackoff:    envDuration("WEBHOOK_BASE_BACKOFF", time.Second),
	}
	processor := worker.New(repository.NewPostgresEventRepository(db), cfg, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.Info("worker_started", "batch_size", cfg.BatchSize, "poll_interval", cfg.PollInterval)

	if err := processor.Run(ctx); err != nil && err != context.Canceled {
		logger.Error("worker_stopped_with_error", "error", err)
		os.Exit(1)
	}
	logger.Info("worker_stopped")
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envDuration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
