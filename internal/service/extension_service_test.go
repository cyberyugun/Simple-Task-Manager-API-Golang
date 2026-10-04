package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func newExtensionTestService(t *testing.T) (*ExtensionService, *repository.InMemoryExtensionRepository, *auth.TokenManager, int64, int64) {
	t.Helper()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	orgs := repository.NewInMemoryOrganizationRepository()
	eventRepo := repository.NewInMemoryEventRepository()
	extensionRepo := repository.NewInMemoryExtensionRepository()
	tokens := auth.NewTokenManager("phase-45-extension-test-secret-1234567890", 15*time.Minute)

	workspace, err := workspaces.Create(1, "Marketplace Publisher Workspace", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Extension Customer", Status: model.OrganizationStatusActive, OwnerUserID: 1,
		MaxWorkspaces: 10, MaxMembers: 10, CreatedByUserID: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orgs.AttachWorkspace(model.OrganizationWorkspace{
		OrganizationID: org.ID, WorkspaceID: workspace.ID, AttachedByID: 1, AttachedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return NewExtensionService(extensionRepo, workspaces, orgs, eventRepo, tokens, false), extensionRepo, tokens, workspace.ID, org.ID
}

func publishExtensionTestApp(t *testing.T, svc *ExtensionService, workspaceID int64, daily int64) model.MarketplaceApplication {
	t.Helper()
	publisher, err := svc.CreatePublisher(1, workspaceID, model.CreateExtensionPublisherRequest{
		Name: "Acme Extensions", Slug: "acme-extensions", WebsiteURL: "https://example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	publisher, err = svc.SubmitPublisher(1, workspaceID, publisher.ID)
	if err != nil {
		t.Fatal(err)
	}
	publisher, err = svc.ReviewPublisher(1, workspaceID, publisher.ID, model.ReviewExtensionPublisherRequest{Decision: "verify"})
	if err != nil || publisher.Status != model.ExtensionPublisherVerified {
		t.Fatalf("publisher=%+v err=%v", publisher, err)
	}

	app, err := svc.CreateApplication(1, workspaceID, model.CreateMarketplaceApplicationRequest{
		PublisherID: publisher.ID,
		Slug: "task-insights",
		Name: "Task Insights",
		Summary: "Remote analytics extension",
		Description: "Reads tasks and receives task events without executing code in the core process.",
		Version: "1.0.0",
		HomepageURL: "https://example.com/task-insights",
		PrivacyURL: "https://example.com/privacy",
		Categories: []string{"analytics", "productivity"},
		RequestedScopes: []string{model.ScopeTasksRead, model.ScopeWorkspaceRead},
		EventTypes: []string{model.EventTaskCreated, model.EventTaskUpdated},
		ConfigSchema: []model.ExtensionConfigField{
			{Key: "region", Label: "Region", Required: true},
			{Key: "api-secret", Label: "API Secret", Required: true, Secret: true},
		},
		Packs: []model.ExtensionPack{
			{Type: model.ExtensionPackReporting, Name: "Task Insights Dashboard", Version: "1.0.0", Definition: map[string]any{"dataset": "tasks"}},
		},
		DailyRequestLimit: daily,
		MonthlyRequestLimit: daily * 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	app, err = svc.SubmitApplication(1, workspaceID, app.ID)
	if err != nil {
		t.Fatal(err)
	}
	app, err = svc.ReviewApplication(1, workspaceID, app.ID, model.ReviewMarketplaceApplicationRequest{Decision: "approve"})
	if err != nil || app.Status != model.MarketplaceAppApproved {
		t.Fatalf("app=%+v err=%v", app, err)
	}
	return app
}

func TestExtensionMarketplaceInstallTokenQuotaAndUninstall(t *testing.T) {
	svc, _, tokens, workspaceID, orgID := newExtensionTestService(t)
	app := publishExtensionTestApp(t, svc, workspaceID, 2)

	listings, err := svc.MarketplaceApps("analytics")
	if err != nil || len(listings) != 1 || listings[0].Application.ID != app.ID {
		t.Fatalf("listings=%+v err=%v", listings, err)
	}

	installed, err := svc.Install(1, orgID, model.InstallExtensionRequest{
		ApplicationID: app.ID,
		WorkspaceID: workspaceID,
		GrantedScopes: []string{model.ScopeTasksRead},
		Config: map[string]string{"region": "ap-southeast"},
		SecretRefs: map[string]string{"api-secret": "secret://vault/task-insights"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if installed.Installation.Status != model.ExtensionInstallationActive || !strings.HasPrefix(installed.Secret, "stm_ext_") {
		t.Fatalf("unexpected installation: %+v", installed)
	}

	tokenResult, err := svc.ExchangeToken(model.ExtensionTokenRequest{Secret: installed.Secret})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := tokens.ParseClaims(tokenResult.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.ClientID != "extension:"+formatTestInt(installed.Installation.ID) ||
		claims.WorkspaceID != workspaceID || len(claims.Scopes) != 1 || claims.Scopes[0] != model.ScopeTasksRead {
		t.Fatalf("unexpected claims: %+v", claims)
	}

	first := svc.AuthorizeExtensionRequest(claims.ClientID, workspaceID, time.Now().UTC())
	second := svc.AuthorizeExtensionRequest(claims.ClientID, workspaceID, time.Now().UTC())
	third := svc.AuthorizeExtensionRequest(claims.ClientID, workspaceID, time.Now().UTC())
	if !first.Allowed || !second.Allowed || third.Allowed || third.StatusCode != 429 {
		t.Fatalf("decisions first=%+v second=%+v third=%+v", first, second, third)
	}
	if err := svc.RecordExtensionResponse(installed.Installation.ID, workspaceID, 200, 25*time.Millisecond, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	usage, err := svc.Usage(1, orgID, installed.Installation.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if usage.TodayRequests != 2 || usage.TotalRequests != 2 || usage.AverageLatencyMS <= 0 {
		t.Fatalf("unexpected usage: %+v", usage)
	}

	uninstalled, err := svc.Uninstall(1, orgID, installed.Installation.ID)
	if err != nil || uninstalled.Status != model.ExtensionInstallationUninstalled {
		t.Fatalf("uninstalled=%+v err=%v", uninstalled, err)
	}
	decision := svc.AuthorizeExtensionRequest(claims.ClientID, workspaceID, time.Now().UTC())
	if decision.Allowed || decision.StatusCode != 403 {
		t.Fatalf("post-uninstall decision=%+v", decision)
	}
	if _, err := svc.ExchangeToken(model.ExtensionTokenRequest{Secret: installed.Secret}); !errors.Is(err, ErrInvalidExtensionSecret) {
		t.Fatalf("exchange after uninstall error=%v", err)
	}
}

func TestExtensionWebhookSubscriptionAndSecretRotation(t *testing.T) {
	svc, _, _, workspaceID, orgID := newExtensionTestService(t)
	app := publishExtensionTestApp(t, svc, workspaceID, 100)
	installed, err := svc.Install(1, orgID, model.InstallExtensionRequest{
		ApplicationID: app.ID,
		WorkspaceID: workspaceID,
		Config: map[string]string{"region": "global"},
		SecretRefs: map[string]string{"api-secret": "secret://vault/acme"},
	})
	if err != nil {
		t.Fatal(err)
	}

	subscription, err := svc.CreateSubscription(1, orgID, installed.Installation.ID, model.CreateExtensionEventSubscriptionRequest{
		URL: "https://example.com/hooks/tasks",
		EventTypes: []string{model.EventTaskCreated},
	})
	if err != nil {
		t.Fatal(err)
	}
	if subscription.Subscription.WebhookSubscriptionID <= 0 || !strings.HasPrefix(subscription.SigningSecret, "whsec_") {
		t.Fatalf("subscription=%+v", subscription)
	}
	if _, err := svc.CreateSubscription(1, orgID, installed.Installation.ID, model.CreateExtensionEventSubscriptionRequest{
		URL: "https://example.com/hooks/comments",
		EventTypes: []string{model.EventTaskCommentCreated},
	}); !errors.Is(err, ErrInvalidExtensionSubscription) {
		t.Fatalf("unexpected undeclared-event error: %v", err)
	}

	rotated, err := svc.RotateSecret(1, orgID, installed.Installation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Secret == installed.Secret || rotated.Installation.InstallSecretPrefix == installed.Installation.InstallSecretPrefix {
		t.Fatalf("secret was not rotated: before=%+v after=%+v", installed, rotated)
	}
	if _, err := svc.ExchangeToken(model.ExtensionTokenRequest{Secret: installed.Secret}); !errors.Is(err, ErrInvalidExtensionSecret) {
		t.Fatalf("old secret error=%v", err)
	}
	if _, err := svc.ExchangeToken(model.ExtensionTokenRequest{Secret: rotated.Secret}); err != nil {
		t.Fatalf("rotated secret exchange: %v", err)
	}

	if err := svc.DeleteSubscription(1, orgID, installed.Installation.ID, subscription.Subscription.ID); err != nil {
		t.Fatal(err)
	}
	items, err := svc.Subscriptions(1, orgID, installed.Installation.ID)
	if err != nil || len(items) != 0 {
		t.Fatalf("subscriptions=%+v err=%v", items, err)
	}
}

func TestExtensionInstallationRejectsPlaintextSecretConfigAndExcessScope(t *testing.T) {
	svc, _, _, workspaceID, orgID := newExtensionTestService(t)
	app := publishExtensionTestApp(t, svc, workspaceID, 100)

	_, err := svc.Install(1, orgID, model.InstallExtensionRequest{
		ApplicationID: app.ID,
		WorkspaceID: workspaceID,
		GrantedScopes: []string{model.ScopeTasksWrite},
		Config: map[string]string{"region": "global"},
		SecretRefs: map[string]string{"api-secret": "secret://vault/acme"},
	})
	if !errors.Is(err, ErrInvalidExtensionInstallation) {
		t.Fatalf("excess scope error=%v", err)
	}

	_, err = svc.Install(1, orgID, model.InstallExtensionRequest{
		ApplicationID: app.ID,
		WorkspaceID: workspaceID,
		Config: map[string]string{"region": "global", "api-secret": "plaintext-secret"},
	})
	if !errors.Is(err, ErrInvalidExtensionConfiguration) {
		t.Fatalf("plaintext secret error=%v", err)
	}
}

func formatTestInt(value int64) string {
	return strconv.FormatInt(value, 10)
}
