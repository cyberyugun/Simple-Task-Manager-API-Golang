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
	refreshRepo := repository.NewPostgresRefreshTokenRepository(db)
	actionRepo := repository.NewPostgresAuthActionTokenRepository(db)
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

	if err := refreshRepo.Create(model.RefreshSession{
		UserID:     user1.ID,
		TokenHash:  "old-refresh-hash",
		UserAgent:  "integration-agent",
		IPAddress:  "203.0.113.20",
		ExpiresAt:  now.Add(time.Hour),
		LastUsedAt: now,
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("create refresh session: %v", err)
	}

	rotated, err := refreshRepo.Rotate(
		"old-refresh-hash",
		"new-refresh-hash",
		now.Add(2*time.Hour),
		now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("rotate refresh session: %v", err)
	}
	if rotated.UserID != user1.ID {
		t.Fatalf("rotated UserID = %d, want %d", rotated.UserID, user1.ID)
	}
	if _, err := refreshRepo.Rotate(
		"old-refresh-hash",
		"replay-refresh-hash",
		now.Add(2*time.Hour),
		now.Add(2*time.Minute),
	); !errors.Is(err, repository.ErrInvalidRefreshToken) {
		t.Fatalf("replayed refresh token error = %v, want ErrInvalidRefreshToken", err)
	}
	if err := refreshRepo.Revoke("new-refresh-hash", now.Add(3*time.Minute)); err != nil {
		t.Fatalf("revoke refresh session: %v", err)
	}
	if _, err := refreshRepo.Rotate(
		"new-refresh-hash",
		"after-logout-hash",
		now.Add(3*time.Hour),
		now.Add(4*time.Minute),
	); !errors.Is(err, repository.ErrInvalidRefreshToken) {
		t.Fatalf("refresh after revoke error = %v, want ErrInvalidRefreshToken", err)
	}

	if err := refreshRepo.Create(model.RefreshSession{
		UserID:     user1.ID,
		TokenHash:  "session-list-hash",
		UserAgent:  "second-agent",
		IPAddress:  "203.0.113.21",
		ExpiresAt:  now.Add(time.Hour),
		LastUsedAt: now.Add(5 * time.Minute),
		CreatedAt:  now.Add(5 * time.Minute),
	}); err != nil {
		t.Fatalf("create list session: %v", err)
	}
	sessions, err := refreshRepo.ListActive(user1.ID, now.Add(6*time.Minute))
	if err != nil {
		t.Fatalf("list active sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].TokenHash != "" || sessions[0].UserAgent != "second-agent" {
		t.Fatalf("unexpected active sessions: %+v", sessions)
	}
	if err := refreshRepo.RevokeByID(user1.ID, sessions[0].ID, now.Add(7*time.Minute)); err != nil {
		t.Fatalf("revoke session by id: %v", err)
	}

	if err := actionRepo.Create(model.AuthActionToken{
		UserID:    user1.ID,
		TokenHash: "reset-action-hash",
		Purpose:   model.ActionPasswordReset,
		ExpiresAt: now.Add(time.Hour),
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("create action token: %v", err)
	}
	action, err := actionRepo.Consume("reset-action-hash", model.ActionPasswordReset, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("consume action token: %v", err)
	}
	if action.UserID != user1.ID {
		t.Fatalf("action UserID = %d, want %d", action.UserID, user1.ID)
	}
	if _, err := actionRepo.Consume("reset-action-hash", model.ActionPasswordReset, now.Add(2*time.Minute)); !errors.Is(err, repository.ErrInvalidActionToken) {
		t.Fatalf("replayed action token error = %v, want ErrInvalidActionToken", err)
	}

	if err := userRepo.UpdatePassword(user1.ID, "new-integration-hash", now.Add(8*time.Minute)); err != nil {
		t.Fatalf("update password: %v", err)
	}
	if err := userRepo.MarkEmailVerified(user1.ID, now.Add(9*time.Minute)); err != nil {
		t.Fatalf("mark email verified: %v", err)
	}
	updatedUser, err := userRepo.FindByID(user1.ID)
	if err != nil {
		t.Fatalf("find updated user: %v", err)
	}
	if updatedUser.PasswordHash != "new-integration-hash" || updatedUser.EmailVerifiedAt == nil {
		t.Fatalf("unexpected updated user: %+v", updatedUser)
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
		"DROP TABLE IF EXISTS auth_action_tokens CASCADE",
		"DROP TABLE IF EXISTS refresh_tokens CASCADE",
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
		"004_refresh_tokens.sql",
		"005_account_security.sql",
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
