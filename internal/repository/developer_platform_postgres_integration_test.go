//go:build integration

package repository_test

import (
	"net/http"
	"testing"
	"time"

	"go-simple-task-api/internal/auth"
	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
)

func TestIntegrationPostgresDeveloperPlatform(t *testing.T) {
	databaseURL := getenvRequired(t, "TEST_DATABASE_URL")
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

	now := time.Now().UTC().Truncate(time.Microsecond)
	users := repository.NewPostgresUserRepository(db)
	workspaces := repository.NewPostgresWorkspaceRepository(db)
	identityRepo := repository.NewPostgresEnterpriseIdentityRepository(db)
	developerRepo := repository.NewPostgresDeveloperPlatformRepository(db)

	owner, err := users.Create(model.User{
		Name: "Developer Platform Owner", Email: "developer-platform-owner@example.com",
		PasswordHash: "integration-hash", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaces.Create(owner.ID, "Developer Platform", now)
	if err != nil {
		t.Fatal(err)
	}
	identity := service.NewEnterpriseIdentityService(identityRepo, workspaces, users, auth.NewTokenManager("postgres-developer-platform-key", 15*time.Minute))
	platform := service.NewDeveloperPlatformService(developerRepo, workspaces, identity, false, []byte("paths:\n  /api/tasks:\n    get:\n      summary: List tasks\n"))

	app, err := platform.CreateApplication(owner.ID, workspace.ID, model.CreateDeveloperApplicationRequest{
		Name:              "Integration App",
		AllowedScopes:     []string{model.ScopeTasksRead, model.ScopeTasksWrite},
		DailyRequestLimit: 2, MonthlyRequestLimit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := platform.SubmitApplication(owner.ID, workspace.ID, app.ID); err != nil {
		t.Fatal(err)
	}
	app, err = platform.ReviewApplication(owner.ID, workspace.ID, app.ID, model.ReviewDeveloperApplicationRequest{Decision: "approve"})
	if err != nil || app.Status != model.DeveloperAppStatusApproved {
		t.Fatalf("app=%+v err=%v", app, err)
	}

	credential, err := platform.CreateCredential(owner.ID, workspace.ID, app.ID, model.CreateDeveloperCredentialRequest{
		Kind: model.DeveloperCredentialAPIKey, Environment: model.DeveloperEnvironmentProduction,
		Scopes: []string{model.ScopeTasksRead},
	})
	if err != nil || credential.APIKey == "" || credential.Credential.ExternalID == "" {
		t.Fatalf("credential=%+v err=%v", credential, err)
	}

	decision := platform.AuthorizeDeveloperRequest(credential.Credential.ExternalID, workspace.ID, "/api/tasks", now.Add(time.Minute))
	if !decision.Allowed || decision.AppID != app.ID {
		t.Fatalf("decision=%+v", decision)
	}
	if err := platform.RecordDeveloperResponse(app.ID, workspace.ID, http.StatusOK, 25*time.Millisecond, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	second := platform.AuthorizeDeveloperRequest(credential.Credential.ExternalID, workspace.ID, "/api/tasks", now.Add(2*time.Minute))
	if !second.Allowed {
		t.Fatalf("second request denied: %+v", second)
	}
	third := platform.AuthorizeDeveloperRequest(credential.Credential.ExternalID, workspace.ID, "/api/tasks", now.Add(3*time.Minute))
	if third.Allowed || third.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("quota should deny third request: %+v", third)
	}

	analytics, err := platform.Analytics(owner.ID, workspace.ID, app.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if analytics.TodayRequests != 2 || analytics.MonthRequests != 2 || analytics.TotalRequests < 2 {
		t.Fatalf("analytics=%+v", analytics)
	}

	rotated, err := platform.RotateCredential(owner.ID, workspace.ID, app.ID, credential.Credential.ID)
	if err != nil || rotated.APIKey == "" || rotated.Credential.ID == credential.Credential.ID {
		t.Fatalf("rotated=%+v err=%v", rotated, err)
	}
	old, err := developerRepo.GetCredential(workspace.ID, credential.Credential.ID)
	if err != nil || old.Status != model.DeveloperCredentialRevoked {
		t.Fatalf("old=%+v err=%v", old, err)
	}
	if _, _, err := developerRepo.FindCredentialByExternalID(credential.Credential.ExternalID); err == nil {
		t.Fatal("revoked credential remained resolvable")
	}

	dryRun := true
	webhook, err := platform.TestWebhook(t.Context(), owner.ID, workspace.ID, app.ID, model.DeveloperWebhookTestRequest{
		URL: "https://example.com/developer-hook", EventType: "task.created",
		Payload: map[string]any{"source": "integration"}, DryRun: &dryRun,
	})
	if err != nil || webhook.Status != "validated" {
		t.Fatalf("webhook=%+v err=%v", webhook, err)
	}
	listed, err := developerRepo.ListWebhookTests(workspace.ID, app.ID, 10)
	if err != nil || len(listed) != 1 || listed[0].ID != webhook.ID {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
}
