package repository

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
)

func TestInMemoryTaskRepositoryCRUDAndOwnership(t *testing.T) {
	repo := NewInMemoryTaskRepository()
	now := time.Now()

	created, err := repo.Create(model.Task{UserID: 1, Title: "Learn Go", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	found, err := repo.FindByID(1, created.ID)
	if err != nil || found.Title != "Learn Go" {
		t.Fatalf("FindByID() = %+v, %v", found, err)
	}
	if _, err := repo.FindByID(2, created.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("cross-user FindByID() error = %v, want ErrTaskNotFound", err)
	}

	if err := repo.Delete(2, created.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("cross-user Delete() error = %v, want ErrTaskNotFound", err)
	}
	if err := repo.Delete(1, created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestInMemoryTaskRepositoryQuery(t *testing.T) {
	repo := NewInMemoryTaskRepository()
	base := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)

	tasks := []model.Task{
		{UserID: 1, Title: "Learn Go", Description: "API basics", Completed: false, CreatedAt: base, UpdatedAt: base},
		{UserID: 1, Title: "PostgreSQL", Description: "Learn database with Go", Completed: true, CreatedAt: base.Add(time.Minute), UpdatedAt: base.Add(time.Minute)},
		{UserID: 1, Title: "Docker", Description: "Containerize API", Completed: false, CreatedAt: base.Add(2 * time.Minute), UpdatedAt: base.Add(2 * time.Minute)},
		{UserID: 2, Title: "Other User", Description: "hidden", Completed: false, CreatedAt: base.Add(3 * time.Minute), UpdatedAt: base.Add(3 * time.Minute)},
	}
	for _, task := range tasks {
		if _, err := repo.Create(task); err != nil {
			t.Fatal(err)
		}
	}

	completed := false
	page, err := repo.FindAll(1, model.TaskQuery{
		Page:      1,
		Limit:     1,
		Search:    "api",
		Completed: &completed,
		Sort:      "created_at",
		Order:     "desc",
	})
	if err != nil {
		t.Fatalf("FindAll() error = %v", err)
	}
	if page.Pagination.Total != 2 || page.Pagination.TotalPages != 2 {
		t.Fatalf("pagination = %+v, want total=2 total_pages=2", page.Pagination)
	}
	if len(page.Items) != 1 || page.Items[0].Title != "Docker" {
		t.Fatalf("items = %+v, want Docker first", page.Items)
	}

	second, err := repo.FindAll(1, model.TaskQuery{
		Page:      2,
		Limit:     1,
		Search:    "api",
		Completed: &completed,
		Sort:      "created_at",
		Order:     "desc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].Title != "Learn Go" {
		t.Fatalf("second page = %+v, want Learn Go", second.Items)
	}
}
