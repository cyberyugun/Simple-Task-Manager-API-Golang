package main

import (
	"log"
	"net/http"
	"os"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/handler"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

func main() {
	var taskRepo repository.TaskRepository

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL != "" {
		db, err := appdb.OpenPostgres(databaseURL)
		if err != nil {
			log.Fatalf("failed to connect to PostgreSQL: %v", err)
		}
		defer db.Close()

		taskRepo = repository.NewPostgresTaskRepository(db)
		log.Println("storage: PostgreSQL")
	} else {
		taskRepo = repository.NewInMemoryTaskRepository()
		log.Println("storage: in-memory (set DATABASE_URL to use PostgreSQL)")
	}

	taskService := service.NewTaskService(taskRepo)
	taskHandler := handler.NewTaskHandler(taskService)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			response.JSON(w, http.StatusMethodNotAllowed, response.Envelope{Success: false, Message: "method not allowed"})
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "API is healthy"})
	})
	mux.HandleFunc("/api/tasks", taskHandler.Tasks)
	mux.HandleFunc("/api/tasks/", taskHandler.TaskByID)

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
