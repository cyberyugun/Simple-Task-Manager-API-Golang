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
	task, err := service.Create(1, model.CreateTaskRequest{Title: "  Learn Golang  ", Description: "  Build API  "})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if task.UserID != 1 || task.Title != "Learn Golang" || task.Description != "Build API" || task.Completed {
		t.Fatalf("Create() returned unexpected task: %+v", task)
	}
}

func TestTaskServiceFindAllDefaultsAndValidation(t *testing.T) {
	service := newTestService()
	for _, title := range []string{"C task", "A task", "B task"} {
		if _, err := service.Create(1, model.CreateTaskRequest{Title: title}); err != nil {
			t.Fatal(err)
		}
	}

	page, err := service.FindAll(1, model.TaskQuery{Sort: "title", Order: "asc"})
	if err != nil {
		t.Fatalf("FindAll() error = %v", err)
	}
	if page.Pagination.Page != 1 || page.Pagination.Limit != 10 || page.Pagination.Total != 3 {
		t.Fatalf("pagination = %+v", page.Pagination)
	}
	if page.Items[0].Title != "A task" {
		t.Fatalf("first title = %q, want A task", page.Items[0].Title)
	}

	invalid := []model.TaskQuery{
		{Page: -1},
		{Limit: 101},
		{Sort: "drop table tasks"},
		{Order: "sideways"},
	}
	for _, query := range invalid {
		if _, err := service.FindAll(1, query); !errors.Is(err, ErrInvalidTaskQuery) {
			t.Fatalf("FindAll(%+v) error = %v, want ErrInvalidTaskQuery", query, err)
		}
	}
}

func TestTaskServiceOwnership(t *testing.T) {
	service := newTestService()
	created, err := service.Create(1, model.CreateTaskRequest{Title: "Private Task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.FindByID(2, created.ID); !errors.Is(err, repository.ErrTaskNotFound) {
		t.Fatalf("FindByID(other user) error = %v", err)
	}
}
