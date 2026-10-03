package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
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
	signingKey := os.Getenv("WEBHOOK_SIGNING_KEY")
	if len(signingKey) < 32 {
		logger.Error("worker_configuration_error", "error", "WEBHOOK_SIGNING_KEY must be at least 32 characters")
		os.Exit(1)
	}

	pollInterval := envDuration("WORKER_POLL_INTERVAL", time.Second)
	httpTimeout := envDuration("WEBHOOK_HTTP_TIMEOUT", 10*time.Second)
	lockTTL := envDuration("WORKER_LOCK_TTL", 2*time.Minute)
	batchSize := envInt("WORKER_BATCH_SIZE", 50)
	allowPrivate := envBool("WEBHOOK_ALLOW_PRIVATE_NETWORKS", false)
	metricsAddr := strings.TrimSpace(os.Getenv("WORKER_METRICS_ADDR"))
	if metricsAddr == "" {
		metricsAddr = ":9091"
	}

	db, err := appdb.OpenPostgres(databaseURL)
	if err != nil {
		logger.Error("worker_database_error", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	hostname, _ := os.Hostname()
	workerID := strings.TrimSpace(os.Getenv("WORKER_ID"))
	if workerID == "" {
		workerID = hostname + "-" + strconv.Itoa(os.Getpid())
	}

	processor := worker.NewProcessor(
		repository.NewPostgresEventRepository(db),
		worker.ProcessorOptions{
			HTTPClient:           &http.Client{Timeout: httpTimeout},
			SigningKey:           signingKey,
			WorkerID:             workerID,
			BatchSize:            batchSize,
			LockTTL:              lockTTL,
			AllowPrivateNetworks: allowPrivate,
			Logger:               logger,
		},
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/metrics", processor.MetricsHandler())
	metricsServer := &http.Server{
		Addr:              metricsAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("worker_metrics_started", "address", metricsAddr)
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("worker_metrics_failed", "error", err)
			stop()
		}
	}()

	run := func() {
		stats, err := processor.RunOnce(ctx)
		if err != nil {
			logger.Error("worker_iteration_failed", "error", err)
			return
		}
		if stats.FannedOut > 0 || stats.Claimed > 0 {
			logger.Info(
				"worker_iteration",
				"fanned_out", stats.FannedOut,
				"claimed", stats.Claimed,
				"delivered", stats.Delivered,
				"retried", stats.Retried,
				"dead_lettered", stats.DeadLetter,
			)
		}
	}

	run()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = metricsServer.Shutdown(shutdownCtx)
			cancel()
			logger.Info("worker_stopped")
			return
		case <-ticker.C:
			run()
		}
	}
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		slog.Error("worker_configuration_error", "name", name, "value", value)
		os.Exit(1)
	}
	return parsed
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		slog.Error("worker_configuration_error", "name", name, "value", value)
		os.Exit(1)
	}
	return parsed
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		slog.Error("worker_configuration_error", "name", name, "value", value)
		os.Exit(1)
	}
	return parsed
}
