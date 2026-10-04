//go:build integration

package repository_test

import (
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/auth"
	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
)

func TestIntegrationPostgresPlatformExtensionsMarketplace(t *testing.T) {
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

	users := repository.NewPostgresUserRepository(db)
	workspaces := repository.NewPostgresWorkspaceRepository(db)
	orgs := repository.NewPostgresOrganizationRepository(db)
	eventsRepo := repository.NewPostgresEventRepository(db)
	extensions := repository.NewPostgresExtensionRepository(db)
	tokens := auth.NewTokenManager("phase-45-integration-secret-1234567890", 15*time.Minute)
	marketplace := service.NewExtensionService(extensions, workspaces, orgs, eventsRepo, tokens, false)

	now := time.Now().UTC().Truncate(time.Microsecond)
	owner, err := users.Create(model.User{
		Name: "Marketplace Owner", Email: "phase45-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaces.Create(owner.ID, "Phase 45 Publisher Workspace", now)
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Phase 45 Customer", Status: model.OrganizationStatusActive, OwnerUserID: owner.ID,
		MaxWorkspaces: 20, MaxMembers: 100, CreatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orgs.AttachWorkspace(model.OrganizationWorkspace{
		OrganizationID: org.ID, WorkspaceID: workspace.ID, AttachedByID: owner.ID, AttachedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	publisher, err := marketplace.CreatePublisher(owner.ID, workspace.ID, model.CreateExtensionPublisherRequest{
		Name: "Phase 45 Publisher", Slug: "phase-45-publisher", WebsiteURL: "https://example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := marketplace.SubmitPublisher(owner.ID, workspace.ID, publisher.ID); err != nil {
		t.Fatal(err)
	}
	publisher, err = marketplace.ReviewPublisher(owner.ID, workspace.ID, publisher.ID, model.ReviewExtensionPublisherRequest{Decision: "verify"})
	if err != nil || publisher.Status != model.ExtensionPublisherVerified {
		t.Fatalf("publisher=%+v err=%v", publisher, err)
	}

	app, err := marketplace.CreateApplication(owner.ID, workspace.ID, model.CreateMarketplaceApplicationRequest{
		PublisherID: publisher.ID, Slug: "phase-45-task-pack", Name: "Phase 45 Task Pack",
		Summary: "Marketplace integration test", Description: "Remote extension using scoped API and event webhooks.",
		Version: "1.0.0", Categories: []string{"automation", "reporting"},
		RequestedScopes: []string{model.ScopeTasksRead, model.ScopeWorkspaceRead},
		EventTypes: []string{model.EventTaskCreated},
		ConfigSchema: []model.ExtensionConfigField{
			{Key: "region", Label: "Region", Required: true},
			{Key: "api-secret", Label: "API Secret", Required: true, Secret: true},
		},
		Packs: []model.ExtensionPack{
			{Type: model.ExtensionPackWorkflow, Name: "Triage Workflow", Version: "1.0.0", Definition: map[string]any{"trigger": "task.created"}},
			{Type: model.ExtensionPackReporting, Name: "Task Report", Version: "1.0.0", Definition: map[string]any{"dataset": "tasks"}},
		},
		DailyRequestLimit: 10, MonthlyRequestLimit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := marketplace.SubmitApplication(owner.ID, workspace.ID, app.ID); err != nil {
		t.Fatal(err)
	}
	app, err = marketplace.ReviewApplication(owner.ID, workspace.ID, app.ID, model.ReviewMarketplaceApplicationRequest{Decision: "approve"})
	if err != nil || app.Status != model.MarketplaceAppApproved {
		t.Fatalf("app=%+v err=%v", app, err)
	}

	installed, err := marketplace.Install(owner.ID, org.ID, model.InstallExtensionRequest{
		ApplicationID: app.ID, WorkspaceID: workspace.ID,
		GrantedScopes: []string{model.ScopeTasksRead},
		Config: map[string]string{"region": "ap-southeast"},
		SecretRefs: map[string]string{"api-secret": "secret://phase45/app"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(installed.Secret, "stm_ext_") || installed.Installation.Status != model.ExtensionInstallationActive {
		t.Fatalf("installation=%+v", installed)
	}
	tokenResult, err := marketplace.ExchangeToken(model.ExtensionTokenRequest{Secret: installed.Secret})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := tokens.ParseClaims(tokenResult.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.WorkspaceID != workspace.ID || claims.ClientID == "" || claims.Scopes[0] != model.ScopeTasksRead {
		t.Fatalf("claims=%+v", claims)
	}

	decision := marketplace.AuthorizeExtensionRequest(claims.ClientID, workspace.ID, time.Now().UTC())
	if !decision.Allowed {
		t.Fatalf("runtime authorization=%+v", decision)
	}
	if err := marketplace.RecordExtensionResponse(installed.Installation.ID, workspace.ID, 200, 12*time.Millisecond, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	usage, err := marketplace.Usage(owner.ID, org.ID, installed.Installation.ID, 30)
	if err != nil || usage.TotalRequests != 1 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}

	subscription, err := marketplace.CreateSubscription(owner.ID, org.ID, installed.Installation.ID, model.CreateExtensionEventSubscriptionRequest{
		URL: "https://example.com/hooks/phase45", EventTypes: []string{model.EventTaskCreated},
	})
	if err != nil {
		t.Fatal(err)
	}
	if subscription.Subscription.WebhookSubscriptionID <= 0 || !strings.HasPrefix(subscription.SigningSecret, "whsec_") {
		t.Fatalf("subscription=%+v", subscription)
	}

	var webhookCount, lifecycleEvents int
	if err := db.QueryRow(`SELECT COUNT(*) FROM webhook_subscriptions WHERE workspace_id=$1`, workspace.ID).Scan(&webhookCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE workspace_id=$1 AND event_type='extension.installed'`, workspace.ID).Scan(&lifecycleEvents); err != nil {
		t.Fatal(err)
	}
	if webhookCount != 1 || lifecycleEvents != 1 {
		t.Fatalf("webhook_count=%d lifecycle_events=%d", webhookCount, lifecycleEvents)
	}

	uninstalled, err := marketplace.Uninstall(owner.ID, org.ID, installed.Installation.ID)
	if err != nil || uninstalled.Status != model.ExtensionInstallationUninstalled {
		t.Fatalf("uninstalled=%+v err=%v", uninstalled, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM webhook_subscriptions WHERE workspace_id=$1`, workspace.ID).Scan(&webhookCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE workspace_id=$1 AND event_type='extension.uninstalled'`, workspace.ID).Scan(&lifecycleEvents); err != nil {
		t.Fatal(err)
	}
	if webhookCount != 0 || lifecycleEvents != 1 {
		t.Fatalf("post-uninstall webhook_count=%d lifecycle_events=%d", webhookCount, lifecycleEvents)
	}
}
