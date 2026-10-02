package repository

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
)

func TestInMemoryTaskRepositoryCRUD(t *testing.T) {
	repo := NewInMemoryTaskRepository()
	now := time.Now()

	created, err := repo.Create(model.Task{Title: "Learn Go", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID != 1 {
		t.Fatalf("Create() ID = %d, want 1", created.ID)
	}

	found, err := repo.FindByID(created.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if found.Title != "Learn Go" {
		t.Fatalf("FindByID() title = %q, want %q", found.Title, "Learn Go")
	}

	all, err := repo.FindAll()
	if err != nil {
		t.Fatalf("FindAll() error = %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("FindAll() len = %d, want 1", len(all))
	}

	found.Title = "Learn Go Testing"
	updated, err := repo.Update(found)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Title != "Learn Go Testing" {
		t.Fatalf("Update() title = %q, want %q", updated.Title, "Learn Go Testing")
	}

	if err := repo.Delete(created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err = repo.FindByID(created.ID)
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("FindByID() after delete error = %v, want ErrTaskNotFound", err)
	}
}

func TestInMemoryTaskRepositoryNotFound(t *testing.T) {
	repo := NewInMemoryTaskRepository()

	if _, err := repo.FindByID(999); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("FindByID() error = %v, want ErrTaskNotFound", err)
	}

	if _, err := repo.Update(model.Task{ID: 999, Title: "Missing"}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Update() error = %v, want ErrTaskNotFound", err)
	}

	if err := repo.Delete(999); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Delete() error = %v, want ErrTaskNotFound", err)
	}
}
