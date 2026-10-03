package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"go-simple-task-api/internal/apidocs"
	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/buildinfo"
	"go-simple-task-api/internal/cache"
	"go-simple-task-api/internal/config"
	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/handler"
	"go-simple-task-api/internal/middleware"
	"go-simple-task-api/internal/observability"
	"go-simple-task-api/internal/readiness"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

func main() {
	bootstrapLogger := observability.NewJSONLogger(os.Getenv("LOG_LEVEL"))
	slog.SetDefault(bootstrapLogger)

	cfg, err := config.Load()
	if err != nil {
		bootstrapLogger.Error("configuration_error", "error", err)
		os.Exit(1)
	}

	logger := observability.NewJSONLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	metrics := observability.NewMetrics()
	tracer, err := observability.NewTracer(
		cfg.OTELExporterEndpoint,
		cfg.OTELServiceName,
		cfg.AppEnv,
		logger,
		metrics,
	)
	if err != nil {
		logger.Error("tracing_configuration_failed", "error", err)
		os.Exit(1)
	}
	if tracer.Enabled() {
		logger.Info("tracing_configured", "protocol", "otlp_http_json", "endpoint", cfg.OTELExporterEndpoint)
	}

	var db *sql.DB
	var redisClient *redis.Client
	var taskRepo repository.TaskRepository
	var userRepo repository.UserRepository
	var refreshRepo repository.RefreshTokenRepository
	var actionRepo repository.AuthActionTokenRepository
	var workspaceRepo repository.WorkspaceRepository
	var eventRepo repository.EventRepository

	if cfg.DatabaseURL != "" {
		db, err = appdb.OpenPostgres(cfg.DatabaseURL, appdb.Options{
			MaxOpenConns:    cfg.DBMaxOpenConns,
			MaxIdleConns:    cfg.DBMaxIdleConns,
			ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
			ConnMaxLifetime: cfg.DBConnMaxLifetime,
		})
		if err != nil {
			logger.Error("postgres_connection_failed", "error", err)
			os.Exit(1)
		}
		defer db.Close()

		taskRepo = repository.NewPostgresTaskRepository(db)
		userRepo = repository.NewPostgresUserRepository(db)
		refreshRepo = repository.NewPostgresRefreshTokenRepository(db)
		actionRepo = repository.NewPostgresAuthActionTokenRepository(db)
		workspaceRepo = repository.NewPostgresWorkspaceRepository(db)
		eventRepo = repository.NewPostgresEventRepository(db)
		logger.Info(
			"storage_configured",
			"backend", "postgresql",
			"max_open_connections", cfg.DBMaxOpenConns,
			"max_idle_connections", cfg.DBMaxIdleConns,
		)
	} else {
		taskRepo = repository.NewInMemoryTaskRepository()
		userRepo = repository.NewInMemoryUserRepository()
		refreshRepo = repository.NewInMemoryRefreshTokenRepository()
		actionRepo = repository.NewInMemoryAuthActionTokenRepository()
		workspaceRepo = repository.NewInMemoryWorkspaceRepository()
		eventRepo = repository.NewInMemoryEventRepository()
		logger.Warn("storage_configured", "backend", "in-memory")
	}

	if cfg.RedisURL != "" {
		redisClient, err = cache.OpenRedis(cfg.RedisURL, cache.Options{
			PoolSize:     cfg.RedisPoolSize,
			MinIdleConns: cfg.RedisMinIdleConns,
			PoolTimeout:  cfg.RedisPoolTimeout,
			DialTimeout:  cfg.RedisDialTimeout,
			ReadTimeout:  cfg.RedisReadTimeout,
			WriteTimeout: cfg.RedisWriteTimeout,
		})
		if err != nil {
			logger.Error("redis_connection_failed", "error", err)
			os.Exit(1)
		}
		defer redisClient.Close()
		logger.Info(
			"redis_configured",
			"pool_size", cfg.RedisPoolSize,
			"min_idle_connections", cfg.RedisMinIdleConns,
		)
	}

	tokenManager := auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTokenTTL)
	authService := service.NewAuthService(
		userRepo,
		refreshRepo,
		actionRepo,
		tokenManager,
		cfg.AccessTokenTTL,
		cfg.RefreshTokenTTL,
		cfg.PasswordResetTTL,
		cfg.EmailVerificationTTL,
	)
	taskService := service.NewTaskService(taskRepo)
	workspaceService := service.NewWorkspaceService(workspaceRepo, userRepo)
	webhookService := service.NewWebhookService(workspaceRepo, eventRepo, cfg.WebhookAllowInsecure)
	authHandler := handler.NewAuthHandler(authService, cfg.ExposeAuthTokens)
	taskHandler := handler.NewTaskHandler(taskService)
	workspaceHandler := handler.NewWorkspaceHandler(workspaceService)
	webhookHandler := handler.NewWebhookHandler(webhookService)
	authMiddleware := middleware.Auth(tokenManager)
	workspaceMiddleware := middleware.WorkspaceScope(workspaceRepo)
	idempotencyMiddleware := middleware.Idempotency(eventRepo, cfg.IdempotencyTTL)

	var authRateLimiter middleware.AuthRateLimiter
	if redisClient != nil {
		authRateLimiter = middleware.NewRedisRateLimiter(
			redisClient,
			cfg.AuthRateLimitRequests,
			cfg.AuthRateLimitWindow,
			cfg.RateLimitFailOpen,
			logger,
			metrics,
		)
		logger.Info("rate_limiter_configured", "backend", "redis")
	} else {
		authRateLimiter = middleware.NewObservedRateLimiter(
			cfg.AuthRateLimitRequests,
			cfg.AuthRateLimitWindow,
			metrics,
		)
		logger.Info("rate_limiter_configured", "backend", "in-memory")
	}

	rateLimited := func(h http.HandlerFunc) http.Handler {
		return authRateLimiter.Handler(http.HandlerFunc(h))
	}
	protected := func(h http.HandlerFunc) http.Handler {
		return authMiddleware(http.HandlerFunc(h))
	}
	protectedRateLimited := func(h http.HandlerFunc) http.Handler {
		return authMiddleware(authRateLimiter.Handler(http.HandlerFunc(h)))
	}
	protectedWorkspace := func(h http.HandlerFunc) http.Handler {
		return authMiddleware(workspaceMiddleware(http.HandlerFunc(h)))
	}
	protectedWorkspaceIdempotent := func(h http.HandlerFunc) http.Handler {
		return authMiddleware(workspaceMiddleware(idempotencyMiddleware(http.HandlerFunc(h))))
	}

	ready := readiness.New(db, redisClient, cfg.ReadinessTimeout, metrics)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			response.JSON(w, http.StatusMethodNotAllowed, response.Envelope{Success: false, Message: "method not allowed"})
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "API is healthy"})
	})
	mux.HandleFunc("/ready", ready.Handler)
	mux.Handle("/metrics", metrics.HandlerWithDependencies(db, redisClient))
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			response.JSON(w, http.StatusMethodNotAllowed, response.Envelope{Success: false, Message: "method not allowed"})
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: buildinfo.Current()})
	})
	apidocs.Register(mux)

	mux.Handle("/api/auth/register", rateLimited(authHandler.Register))
	mux.Handle("/api/auth/login", rateLimited(authHandler.Login))
	mux.Handle("/api/auth/refresh", rateLimited(authHandler.Refresh))
	mux.Handle("/api/auth/logout", rateLimited(authHandler.Logout))
	mux.Handle("/api/auth/forgot-password", rateLimited(authHandler.ForgotPassword))
	mux.Handle("/api/auth/reset-password", rateLimited(authHandler.ResetPassword))
	mux.Handle("/api/auth/email-verification/confirm", rateLimited(authHandler.VerifyEmail))

	mux.Handle("/api/auth/change-password", protectedRateLimited(authHandler.ChangePassword))
	mux.Handle("/api/auth/logout-all", protected(authHandler.LogoutAll))
	mux.Handle("/api/auth/sessions", protected(authHandler.Sessions))
	mux.Handle("/api/auth/sessions/", protected(authHandler.SessionByID))
	mux.Handle("/api/auth/email-verification/request", protectedRateLimited(authHandler.RequestEmailVerification))

	mux.Handle("/api/workspaces", protected(workspaceHandler.Workspaces))
	mux.Handle("/api/workspaces/{id}/webhooks", protected(webhookHandler.Subscriptions))
	mux.Handle("/api/workspaces/{id}/webhooks/{subscription_id}", protected(webhookHandler.SubscriptionByID))
	mux.Handle("/api/workspaces/", protected(workspaceHandler.WorkspaceByID))

	mux.Handle("/api/tasks", protectedWorkspaceIdempotent(taskHandler.Tasks))
	mux.Handle("/api/tasks/", protectedWorkspace(taskHandler.TaskByID))

	var root http.Handler = mux
	root = middleware.Recover(logger, metrics, root)
	root = middleware.AccessLog(logger, metrics, root)
	root = middleware.Trace(tracer, root)
	root = middleware.RequestID(root)
	root = middleware.APIVersion("v1", root)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           root,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("server_started", "address", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server_failed", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		logger.Info("shutdown_signal_received")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful_shutdown_failed", "error", err)
			_ = server.Close()
		}

		if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server_stop_error", "error", err)
		}
	}

	traceShutdownCtx, traceCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer traceCancel()
	if err := tracer.Shutdown(traceShutdownCtx); err != nil {
		logger.Warn("trace_shutdown_failed", "error", err)
	}

	logger.Info("server_stopped")
}
