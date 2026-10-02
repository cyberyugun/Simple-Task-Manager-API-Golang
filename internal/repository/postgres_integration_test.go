//go:build integration

package repository_test

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestIntegrationPostgresRepositories(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	db, err := appdb.OpenPostgres(databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgres() error = %v", err)
	}

	if err := resetDatabase(db); err != nil {
		_ = db.Close()
		t.Fatalf("reset database: %v", err)
	}

	t.Cleanup(func() {
		if err := resetDatabase(db); err != nil {
			t.Errorf("cleanup database: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	applyMigrations(t, db)

	userRepo := repository.NewPostgresUserRepository(db)
	taskRepo := repository.NewPostgresTaskRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	user1, err := userRepo.Create(model.User{
		Name:         "User One",
		Email:        "one@example.com",
		PasswordHash: "integration-test-hash",
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("create user1: %v", err)
	}

	user2, err := userRepo.Create(model.User{
		Name:         "User Two",
		Email:        "two@example.com",
		PasswordHash: "integration-test-hash",
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("create user2: %v", err)
	}

	if _, err := userRepo.Create(model.User{
		Name:         "Duplicate",
		Email:        "one@example.com",
		PasswordHash: "integration-test-hash",
		CreatedAt:    now,
		UpdatedAt:    now,
	}); !errors.Is(err, repository.ErrEmailExists) {
		t.Fatalf("duplicate email error = %v, want ErrEmailExists", err)
	}

	first, err := taskRepo.Create(model.Task{
		UserID:      user1.ID,
		Title:       "Learn Go",
		Description: "Integration API test",
		Completed:   false,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("create first task: %v", err)
	}

	_, err = taskRepo.Create(model.Task{
		UserID:      user1.ID,
		Title:       "Completed task",
		Description: "Already done",
		Completed:   true,
		CreatedAt:   now.Add(time.Second),
		UpdatedAt:   now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("create completed task: %v", err)
	}

	_, err = taskRepo.Create(model.Task{
		UserID:      user2.ID,
		Title:       "Other user task",
		Description: "Must stay private",
		Completed:   false,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("create other user task: %v", err)
	}

	completed := false
	page, err := taskRepo.FindAll(user1.ID, model.TaskQuery{
		Page:      1,
		Limit:     10,
		Search:    "go",
		Completed: &completed,
		Sort:      "created_at",
		Order:     "desc",
	})
	if err != nil {
		t.Fatalf("FindAll() error = %v", err)
	}
	if page.Pagination.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != first.ID {
		t.Fatalf("unexpected page: %+v", page)
	}

	if _, err := taskRepo.FindByID(user2.ID, first.ID); !errors.Is(err, repository.ErrTaskNotFound) {
		t.Fatalf("cross-user FindByID() error = %v, want ErrTaskNotFound", err)
	}

	first.Title = "Updated Go Task"
	first.UpdatedAt = now.Add(2 * time.Second)
	updated, err := taskRepo.Update(first)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Title != "Updated Go Task" {
		t.Fatalf("updated title = %q", updated.Title)
	}

	if err := taskRepo.Delete(user1.ID, first.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := taskRepo.FindByID(user1.ID, first.ID); !errors.Is(err, repository.ErrTaskNotFound) {
		t.Fatalf("FindByID() after delete error = %v, want ErrTaskNotFound", err)
	}
}

func resetDatabase(db *sql.DB) error {
	for _, statement := range []string{
		"DROP TABLE IF EXISTS tasks CASCADE",
		"DROP TABLE IF EXISTS users CASCADE",
	} {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("%s: %w", statement, err)
		}
	}
	return nil
}

func applyMigrations(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, name := range []string{
		"001_create_tasks.sql",
		"002_add_users_and_task_ownership.sql",
		"003_task_list_indexes.sql",
	} {
		data, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}

		for _, statement := range strings.Split(string(data), ";") {
			statement = strings.TrimSpace(statement)
			if statement == "" {
				continue
			}
			if _, err := db.Exec(statement); err != nil {
				t.Fatalf("apply migration %s: %v", name, err)
			}
		}
	}
}
