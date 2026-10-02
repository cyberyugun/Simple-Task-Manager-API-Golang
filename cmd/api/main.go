package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"go-simple-task-api/internal/apidocs"
	"go-simple-task-api/internal/auth"
	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/handler"
	"go-simple-task-api/internal/middleware"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

func main() {
	jwtSecret := os.Getenv("JWT_SECRET")
	if len(jwtSecret) < 32 {
		log.Fatal("JWT_SECRET is required and must be at least 32 characters")
	}

	var taskRepo repository.TaskRepository
	var userRepo repository.UserRepository

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL != "" {
		db, err := appdb.OpenPostgres(databaseURL)
		if err != nil {
			log.Fatalf("failed to connect to PostgreSQL: %v", err)
		}
		defer db.Close()

		taskRepo = repository.NewPostgresTaskRepository(db)
		userRepo = repository.NewPostgresUserRepository(db)
		log.Println("storage: PostgreSQL")
	} else {
		taskRepo = repository.NewInMemoryTaskRepository()
		userRepo = repository.NewInMemoryUserRepository()
		log.Println("storage: in-memory (set DATABASE_URL to use PostgreSQL)")
	}

	tokenManager := auth.NewTokenManager(jwtSecret, 24*time.Hour)
	authService := service.NewAuthService(userRepo, tokenManager)
	taskService := service.NewTaskService(taskRepo)
	authHandler := handler.NewAuthHandler(authService)
	taskHandler := handler.NewTaskHandler(taskService)
	authMiddleware := middleware.Auth(tokenManager)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			response.JSON(w, http.StatusMethodNotAllowed, response.Envelope{Success: false, Message: "method not allowed"})
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "API is healthy"})
	})
	apidocs.Register(mux)
	mux.HandleFunc("/api/auth/register", authHandler.Register)
	mux.HandleFunc("/api/auth/login", authHandler.Login)
	mux.Handle("/api/tasks", authMiddleware(http.HandlerFunc(taskHandler.Tasks)))
	mux.Handle("/api/tasks/", authMiddleware(http.HandlerFunc(taskHandler.TaskByID)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	log.Printf("server running on http://localhost:%s", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
