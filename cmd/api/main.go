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

	var db *sql.DB
	var redisClient *redis.Client
	var taskRepo repository.TaskRepository
	var userRepo repository.UserRepository
	var refreshRepo repository.RefreshTokenRepository
	var actionRepo repository.AuthActionTokenRepository

	if cfg.DatabaseURL != "" {
		db, err = appdb.OpenPostgres(cfg.DatabaseURL)
		if err != nil {
			logger.Error("postgres_connection_failed", "error", err)
			os.Exit(1)
		}
		defer db.Close()

		taskRepo = repository.NewPostgresTaskRepository(db)
		userRepo = repository.NewPostgresUserRepository(db)
		refreshRepo = repository.NewPostgresRefreshTokenRepository(db)
		actionRepo = repository.NewPostgresAuthActionTokenRepository(db)
		logger.Info("storage_configured", "backend", "postgresql")
	} else {
		taskRepo = repository.NewInMemoryTaskRepository()
		userRepo = repository.NewInMemoryUserRepository()
		refreshRepo = repository.NewInMemoryRefreshTokenRepository()
		actionRepo = repository.NewInMemoryAuthActionTokenRepository()
		logger.Warn("storage_configured", "backend", "in-memory")
	}

	if cfg.RedisURL != "" {
		redisClient, err = cache.OpenRedis(cfg.RedisURL)
		if err != nil {
			logger.Error("redis_connection_failed", "error", err)
			os.Exit(1)
		}
		defer redisClient.Close()
		logger.Info("redis_configured")
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
	authHandler := handler.NewAuthHandler(authService, cfg.ExposeAuthTokens)
	taskHandler := handler.NewTaskHandler(taskService)
	authMiddleware := middleware.Auth(tokenManager)

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
	mux.Handle("/metrics", metrics.Handler())
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

	mux.Handle("/api/tasks", protected(taskHandler.Tasks))
	mux.Handle("/api/tasks/", protected(taskHandler.TaskByID))

	var root http.Handler = mux
	root = middleware.Recover(logger, metrics, root)
	root = middleware.AccessLog(logger, metrics, root)
	root = middleware.RequestID(root)

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

	logger.Info("server_stopped")
}
