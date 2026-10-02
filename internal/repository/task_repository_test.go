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
	if created.ID != 1 {
		t.Fatalf("Create() ID = %d, want 1", created.ID)
	}

	found, err := repo.FindByID(1, created.ID)
	if err != nil {
		t.Fatalf("FindByID() error = %v", err)
	}
	if found.Title != "Learn Go" {
		t.Fatalf("FindByID() title = %q, want %q", found.Title, "Learn Go")
	}

	if _, err := repo.FindByID(2, created.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("cross-user FindByID() error = %v, want ErrTaskNotFound", err)
	}

	all, err := repo.FindAll(1)
	if err != nil {
		t.Fatalf("FindAll() error = %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("FindAll() len = %d, want 1", len(all))
	}

	other, err := repo.FindAll(2)
	if err != nil {
		t.Fatalf("FindAll(other user) error = %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("FindAll(other user) len = %d, want 0", len(other))
	}

	found.Title = "Learn Go Testing"
	updated, err := repo.Update(found)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Title != "Learn Go Testing" {
		t.Fatalf("Update() title = %q, want %q", updated.Title, "Learn Go Testing")
	}

	if err := repo.Delete(2, created.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("cross-user Delete() error = %v, want ErrTaskNotFound", err)
	}
	if err := repo.Delete(1, created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}
