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
	assertPerformanceIndexes(t, db)

	userRepo := repository.NewPostgresUserRepository(db)
	taskRepo := repository.NewPostgresTaskRepository(db)
	refreshRepo := repository.NewPostgresRefreshTokenRepository(db)
	actionRepo := repository.NewPostgresAuthActionTokenRepository(db)
	workspaceRepo := repository.NewPostgresWorkspaceRepository(db)
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

	workspace1, err := workspaceRepo.ResolveDefault(user1.ID, now)
	if err != nil {
		t.Fatalf("resolve user1 personal workspace: %v", err)
	}
	workspace2, err := workspaceRepo.ResolveDefault(user2.ID, now)
	if err != nil {
		t.Fatalf("resolve user2 personal workspace: %v", err)
	}
	if workspace1.ID == workspace2.ID || !workspace1.IsPersonal || !workspace2.IsPersonal {
		t.Fatalf("unexpected personal workspaces: user1=%+v user2=%+v", workspace1, workspace2)
	}

	shared, err := workspaceRepo.Create(user1.ID, "Shared Team", now)
	if err != nil {
		t.Fatalf("create shared workspace: %v", err)
	}
	if _, err := workspaceRepo.AddMember(shared.ID, user2.ID, model.WorkspaceRoleMember, now); err != nil {
		t.Fatalf("add shared member: %v", err)
	}
	if _, err := workspaceRepo.ResolveAccess(user2.ID, shared.ID, now); err != nil {
		t.Fatalf("user2 shared access: %v", err)
	}
	if _, err := workspaceRepo.ResolveAccess(user2.ID, workspace1.ID, now); !errors.Is(err, repository.ErrWorkspaceNotFound) {
		t.Fatalf("cross-tenant workspace access error = %v, want ErrWorkspaceNotFound", err)
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
		WorkspaceID: workspace1.ID,
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
		WorkspaceID: workspace1.ID,
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
		WorkspaceID: workspace2.ID,
		UserID:      user2.ID,
		Title:       "Other workspace task",
		Description: "Must stay private",
		Completed:   false,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("create other user task: %v", err)
	}

	completed := false
	page, err := taskRepo.FindAll(workspace1.ID, model.TaskQuery{
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

	if _, err := taskRepo.FindByID(workspace2.ID, first.ID); !errors.Is(err, repository.ErrTaskNotFound) {
		t.Fatalf("cross-workspace FindByID() error = %v, want ErrTaskNotFound", err)
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

	if err := taskRepo.Delete(workspace1.ID, first.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := taskRepo.FindByID(workspace1.ID, first.ID); !errors.Is(err, repository.ErrTaskNotFound) {
		t.Fatalf("FindByID() after delete error = %v, want ErrTaskNotFound", err)
	}
}

func resetDatabase(db *sql.DB) error {
	for _, statement := range []string{
		"DROP TABLE IF EXISTS audit_events CASCADE",
		"DROP TABLE IF EXISTS workspace_members CASCADE",
		"DROP TABLE IF EXISTS auth_action_tokens CASCADE",
		"DROP TABLE IF EXISTS refresh_tokens CASCADE",
		"DROP TABLE IF EXISTS tasks CASCADE",
		"DROP TABLE IF EXISTS workspaces CASCADE",
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
		"006_performance_indexes.sql",
		"007_workspaces_rbac_audit.sql",
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

func assertPerformanceIndexes(t *testing.T, db *sql.DB) {
	t.Helper()

	var extensionExists bool
	if err := db.QueryRow("SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm')").Scan(&extensionExists); err != nil {
		t.Fatalf("check pg_trgm extension: %v", err)
	}
	if !extensionExists {
		t.Fatal("pg_trgm extension is not installed")
	}

	expectedNames := []string{
		"idx_tasks_user_title_trgm",
		"idx_tasks_user_description_trgm",
		"idx_tasks_user_completed_created_at",
		"idx_refresh_tokens_user_active_last_used",
		"idx_tasks_workspace_completed_created_at",
	}
	foundIndexes := make(map[string]bool, len(expectedNames))

	rows, err := db.Query(`
		SELECT indexname
		FROM pg_indexes
		WHERE schemaname = 'public'
		  AND indexname IN (
		    'idx_tasks_user_title_trgm',
		    'idx_tasks_user_description_trgm',
		    'idx_tasks_user_completed_created_at',
		    'idx_refresh_tokens_user_active_last_used',
		    'idx_tasks_workspace_completed_created_at'
		  )
	`)
	if err != nil {
		t.Fatalf("list performance indexes: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan performance index: %v", err)
		}
		foundIndexes[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate performance indexes: %v", err)
	}
	for _, name := range expectedNames {
		if !foundIndexes[name] {
			t.Fatalf("performance index %s is missing", name)
		}
	}

	assertPlanUsesIndex(t, db, `
		SELECT id, user_id, title, description, completed, created_at, updated_at
		FROM tasks
		WHERE user_id = 1 AND completed = false
		ORDER BY created_at DESC, id DESC
		LIMIT 20
	`, "idx_tasks_user_completed_created_at")

	assertPlanUsesIndex(t, db, `
		SELECT id
		FROM tasks
		WHERE title ILIKE '%needle%'
	`, "idx_tasks_user_title_trgm")
}

func assertPlanUsesIndex(t *testing.T, db *sql.DB, statement, indexName string) {
	t.Helper()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin explain transaction: %v", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("disable seqscan for explain: %v", err)
	}

	rows, err := tx.Query("EXPLAIN (COSTS OFF) " + statement)
	if err != nil {
		t.Fatalf("explain query: %v", err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain row: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate explain rows: %v", err)
	}
	if !strings.Contains(plan.String(), indexName) {
		t.Fatalf("query plan does not use %s:\n%s", indexName, plan.String())
	}
}
