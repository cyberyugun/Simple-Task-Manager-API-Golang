package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestDeveloperPlatformLifecycleCredentialSandboxQuotaAndPortal(t *testing.T) {
	users := repository.NewInMemoryUserRepository()
	user, err := users.Create(model.User{
		Name: "Developer Owner", Email: "developer-owner@example.com", PasswordHash: "hash",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	workspaces := repository.NewInMemoryWorkspaceRepository()
	access, err := workspaces.Create(user.ID, "Developer Workspace", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	identityRepo := repository.NewInMemoryEnterpriseIdentityRepository()
	identity := NewEnterpriseIdentityService(identityRepo, workspaces, users, auth.NewTokenManager("developer-platform-test-secret", 15*time.Minute))
	repo := repository.NewInMemoryDeveloperPlatformRepository()
	svc := NewDeveloperPlatformService(repo, workspaces, identity, false, []byte("paths:\n  /api/tasks:\n    get:\n      summary: List tasks\n"))

	app, err := svc.CreateApplication(user.ID, access.ID, model.CreateDeveloperApplicationRequest{
		Name: "Task Companion", AllowedScopes: []string{model.ScopeTasksRead, model.ScopeTasksWrite},
		DailyRequestLimit: 1, MonthlyRequestLimit: 2,
	})
	if err != nil || app.Status != model.DeveloperAppStatusDraft || !app.SandboxEnabled {
		t.Fatalf("app=%+v err=%v", app, err)
	}

	sandbox, err := svc.CreateCredential(user.ID, access.ID, app.ID, model.CreateDeveloperCredentialRequest{
		Kind: model.DeveloperCredentialAPIKey, Environment: model.DeveloperEnvironmentSandbox,
		Scopes: []string{model.ScopeTasksRead},
	})
	if err != nil || sandbox.APIKey == "" || sandbox.Credential.ExternalID == "" {
		t.Fatalf("sandbox credential=%+v err=%v", sandbox, err)
	}
	if _, err := svc.CreateCredential(user.ID, access.ID, app.ID, model.CreateDeveloperCredentialRequest{
		Kind: model.DeveloperCredentialOAuthClient, Environment: model.DeveloperEnvironmentProduction,
		Scopes: []string{model.ScopeTasksRead}, RedirectURIs: []string{"https://client.example.com/callback"},
	}); !errors.Is(err, ErrDeveloperAppApprovalRequired) {
		t.Fatalf("production credential before approval error=%v", err)
	}

	decision := svc.AuthorizeDeveloperRequest(sandbox.Credential.ExternalID, access.ID, "/api/developer/sandbox/echo", time.Now().UTC())
	if !decision.Allowed || !decision.IsDeveloper || decision.Environment != model.DeveloperEnvironmentSandbox {
		t.Fatalf("unexpected sandbox decision: %+v", decision)
	}
	blocked := svc.AuthorizeDeveloperRequest(sandbox.Credential.ExternalID, access.ID, "/api/tasks", time.Now().UTC())
	if blocked.Allowed || blocked.StatusCode != http.StatusForbidden {
		t.Fatalf("sandbox production access should be blocked: %+v", blocked)
	}
	exhausted := svc.AuthorizeDeveloperRequest(sandbox.Credential.ExternalID, access.ID, "/api/developer/sandbox/echo", time.Now().UTC())
	if exhausted.Allowed || exhausted.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected quota denial: %+v", exhausted)
	}
	contract, err := svc.SandboxContext(sandbox.Credential.ExternalID, access.ID)
	if err != nil || !contract.Isolated || contract.AppID != app.ID {
		t.Fatalf("contract=%+v err=%v", contract, err)
	}

	submitted, err := svc.SubmitApplication(user.ID, access.ID, app.ID)
	if err != nil || submitted.Status != model.DeveloperAppStatusSubmitted {
		t.Fatalf("submitted=%+v err=%v", submitted, err)
	}
	approved, err := svc.ReviewApplication(user.ID, access.ID, app.ID, model.ReviewDeveloperApplicationRequest{Decision: "approve"})
	if err != nil || approved.Status != model.DeveloperAppStatusApproved {
		t.Fatalf("approved=%+v err=%v", approved, err)
	}

	production, err := svc.CreateCredential(user.ID, access.ID, app.ID, model.CreateDeveloperCredentialRequest{
		Kind: model.DeveloperCredentialOAuthClient, Environment: model.DeveloperEnvironmentProduction,
		Scopes: []string{model.ScopeTasksRead}, RedirectURIs: []string{"https://client.example.com/callback"},
	})
	if err != nil || production.ClientID == "" || production.ClientSecret == "" {
		t.Fatalf("production credential=%+v err=%v", production, err)
	}
	rotated, err := svc.RotateCredential(user.ID, access.ID, app.ID, production.Credential.ID)
	if err != nil || rotated.ClientID == "" || rotated.ClientID == production.ClientID {
		t.Fatalf("rotated=%+v err=%v", rotated, err)
	}
	credentials, err := svc.Credentials(user.ID, access.ID, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundRevoked := false
	for _, item := range credentials {
		if item.ID == production.Credential.ID && item.Status == model.DeveloperCredentialRevoked {
			foundRevoked = true
		}
	}
	if !foundRevoked {
		t.Fatalf("rotated credential was not revoked: %+v", credentials)
	}

	docs := svc.SearchDocs("list tasks")
	if len(docs) != 1 || docs[0].Path != "/api/tasks" || docs[0].Method != http.MethodGet {
		t.Fatalf("unexpected docs: %+v", docs)
	}
	if len(svc.SDKs()) != 5 {
		t.Fatalf("expected five SDK targets")
	}

	dryRun := true
	webhook, err := svc.TestWebhook(context.Background(), user.ID, access.ID, app.ID, model.DeveloperWebhookTestRequest{
		URL: "https://example.com/hooks/test", EventType: "task.created",
		Payload: map[string]any{"task_id": 42}, DryRun: &dryRun,
	})
	if err != nil || webhook.Status != "validated" || !webhook.DryRun {
		t.Fatalf("webhook=%+v err=%v", webhook, err)
	}
}
