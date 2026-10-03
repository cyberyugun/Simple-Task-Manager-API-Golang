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

func workspaceAccess(id int64) model.WorkspaceAccess {
	return model.WorkspaceAccess{Workspace: model.Workspace{ID: id}, Role: model.WorkspaceRoleMember}
}

func TestTaskServiceCreate(t *testing.T) {
	service := newTestService()
	task, err := service.Create(1, workspaceAccess(10), model.CreateTaskRequest{Title: "  Learn Golang  ", Description: "  Build API  "})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if task.UserID != 1 || task.WorkspaceID != 10 || task.Title != "Learn Golang" || task.Description != "Build API" || task.Completed {
		t.Fatalf("Create() returned unexpected task: %+v", task)
	}
}

func TestTaskServiceFindAllDefaultsAndValidation(t *testing.T) {
	service := newTestService()
	for _, title := range []string{"C task", "A task", "B task"} {
		if _, err := service.Create(1, workspaceAccess(10), model.CreateTaskRequest{Title: title}); err != nil {
			t.Fatal(err)
		}
	}

	page, err := service.FindAll(10, model.TaskQuery{Sort: "title", Order: "asc"})
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
		if _, err := service.FindAll(10, query); !errors.Is(err, ErrInvalidTaskQuery) {
			t.Fatalf("FindAll(%+v) error = %v, want ErrInvalidTaskQuery", query, err)
		}
	}
}

func TestTaskServiceWorkspaceIsolation(t *testing.T) {
	service := newTestService()
	created, err := service.Create(1, workspaceAccess(10), model.CreateTaskRequest{Title: "Private Task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.FindByID(20, created.ID); !errors.Is(err, repository.ErrTaskNotFound) {
		t.Fatalf("FindByID(other workspace) error = %v", err)
	}
}

func TestTaskServiceIdempotency(t *testing.T) {
	service := newTestService()
	access := workspaceAccess(10)
	req := model.CreateTaskRequest{Title: "Retry safe", Description: "same payload"}

	first, err := service.CreateIdempotent(1, access, req, "idem-key-1234")
	if err != nil {
		t.Fatalf("first CreateIdempotent() error = %v", err)
	}
	if first.Replayed {
		t.Fatal("first idempotent create must not be replayed")
	}

	second, err := service.CreateIdempotent(1, access, req, "idem-key-1234")
	if err != nil {
		t.Fatalf("second CreateIdempotent() error = %v", err)
	}
	if !second.Replayed || second.Task.ID != first.Task.ID {
		t.Fatalf("unexpected replay result: first=%+v second=%+v", first, second)
	}

	_, err = service.CreateIdempotent(1, access, model.CreateTaskRequest{Title: "Different"}, "idem-key-1234")
	if !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("conflicting idempotency key error = %v, want ErrIdempotencyConflict", err)
	}

	if _, err := service.CreateIdempotent(1, access, req, "short"); !errors.Is(err, ErrInvalidIdempotencyKey) {
		t.Fatalf("short key error = %v, want ErrInvalidIdempotencyKey", err)
	}
}
