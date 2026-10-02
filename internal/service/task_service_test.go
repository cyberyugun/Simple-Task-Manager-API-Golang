package service

import (
	"errors"
	"testing"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func newTestService() *TaskService {
	return NewTaskService(repository.NewInMemoryTaskRepository())
}

func TestTaskServiceCreate(t *testing.T) {
	service := newTestService()

	task, err := service.Create(1, model.CreateTaskRequest{
		Title:       "  Learn Golang  ",
		Description: "  Build API  ",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if task.UserID != 1 {
		t.Fatalf("Create() UserID = %d, want 1", task.UserID)
	}
	if task.Title != "Learn Golang" || task.Description != "Build API" {
		t.Fatalf("Create() returned unexpected task: %+v", task)
	}
	if task.Completed {
		t.Fatal("Create() completed = true, want false")
	}
}

func TestTaskServiceOwnership(t *testing.T) {
	service := newTestService()
	created, err := service.Create(1, model.CreateTaskRequest{Title: "Private Task"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, err := service.FindByID(2, created.ID); !errors.Is(err, repository.ErrTaskNotFound) {
		t.Fatalf("FindByID(other user) error = %v, want ErrTaskNotFound", err)
	}

	if _, err := service.Update(2, created.ID, model.UpdateTaskRequest{Title: "Hijack"}); !errors.Is(err, repository.ErrTaskNotFound) {
		t.Fatalf("Update(other user) error = %v, want ErrTaskNotFound", err)
	}

	if err := service.Delete(2, created.ID); !errors.Is(err, repository.ErrTaskNotFound) {
		t.Fatalf("Delete(other user) error = %v, want ErrTaskNotFound", err)
	}
}
