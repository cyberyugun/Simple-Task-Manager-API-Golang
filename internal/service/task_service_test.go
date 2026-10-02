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

	task, err := service.Create(model.CreateTaskRequest{
		Title:       "  Learn Golang  ",
		Description: "  Build API  ",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if task.ID != 1 {
		t.Fatalf("Create() ID = %d, want 1", task.ID)
	}
	if task.Title != "Learn Golang" {
		t.Fatalf("Create() title = %q, want trimmed title", task.Title)
	}
	if task.Description != "Build API" {
		t.Fatalf("Create() description = %q, want trimmed description", task.Description)
	}
	if task.Completed {
		t.Fatal("Create() completed = true, want false")
	}
	if task.CreatedAt.IsZero() || task.UpdatedAt.IsZero() {
		t.Fatal("Create() timestamps should be populated")
	}
}

func TestTaskServiceCreateRequiresTitle(t *testing.T) {
	service := newTestService()

	_, err := service.Create(model.CreateTaskRequest{Title: "   "})
	if !errors.Is(err, ErrInvalidTask) {
		t.Fatalf("Create() error = %v, want ErrInvalidTask", err)
	}
}

func TestTaskServiceUpdateCompleteAndDelete(t *testing.T) {
	service := newTestService()

	created, err := service.Create(model.CreateTaskRequest{Title: "Initial"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	updated, err := service.Update(created.ID, model.UpdateTaskRequest{
		Title:       "Updated",
		Description: "New description",
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Title != "Updated" || updated.Description != "New description" {
		t.Fatalf("Update() returned unexpected task: %+v", updated)
	}

	completed, err := service.Complete(created.ID)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if !completed.Completed {
		t.Fatal("Complete() completed = false, want true")
	}

	if err := service.Delete(created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err = service.FindByID(created.ID)
	if !errors.Is(err, repository.ErrTaskNotFound) {
		t.Fatalf("FindByID() after delete error = %v, want ErrTaskNotFound", err)
	}
}
