package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"go-simple-task-api/internal/apidocs"
	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/config"
	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/handler"
	"go-simple-task-api/internal/middleware"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	var taskRepo repository.TaskRepository
	var userRepo repository.UserRepository
	var refreshRepo repository.RefreshTokenRepository
	var actionRepo repository.AuthActionTokenRepository

	if cfg.DatabaseURL != "" {
		db, err := appdb.OpenPostgres(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to connect to PostgreSQL: %v", err)
		}
		defer db.Close()

		taskRepo = repository.NewPostgresTaskRepository(db)
		userRepo = repository.NewPostgresUserRepository(db)
		refreshRepo = repository.NewPostgresRefreshTokenRepository(db)
		actionRepo = repository.NewPostgresAuthActionTokenRepository(db)
		log.Println("storage: PostgreSQL")
	} else {
		taskRepo = repository.NewInMemoryTaskRepository()
		userRepo = repository.NewInMemoryUserRepository()
		refreshRepo = repository.NewInMemoryRefreshTokenRepository()
		actionRepo = repository.NewInMemoryAuthActionTokenRepository()
		log.Println("storage: in-memory (set DATABASE_URL to use PostgreSQL)")
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
	authRateLimiter := middleware.NewRateLimiter(cfg.AuthRateLimitRequests, cfg.AuthRateLimitWindow)

	rateLimited := func(h http.HandlerFunc) http.Handler {
		return authRateLimiter.Handler(http.HandlerFunc(h))
	}
	protected := func(h http.HandlerFunc) http.Handler {
		return authMiddleware(http.HandlerFunc(h))
	}
	protectedRateLimited := func(h http.HandlerFunc) http.Handler {
		return authMiddleware(authRateLimiter.Handler(http.HandlerFunc(h)))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			response.JSON(w, http.StatusMethodNotAllowed, response.Envelope{Success: false, Message: "method not allowed"})
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "API is healthy"})
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

	mux.Handle("/api/tasks", protected(taskHandler.Tasks))
	mux.Handle("/api/tasks/", protected(taskHandler.TaskByID))

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("server running on http://localhost:%s", cfg.Port)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	case <-ctx.Done():
		log.Println("shutdown signal received")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v; forcing close", err)
			_ = server.Close()
		}

		if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server stopped with error: %v", err)
		}
	}

	log.Println("server stopped")
}
