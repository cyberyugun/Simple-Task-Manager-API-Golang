package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestConnectorOAuthPKCERefreshRotationAndHealth(t *testing.T) {
	var tokenCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls++
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("grant_type") == "authorization_code" && r.Form.Get("code_verifier") == "" {
				t.Fatal("missing PKCE verifier")
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "runtime-access-token",
				"refresh_token": "runtime-refresh-token",
				"scope":         "tasks:read tasks:write",
				"expires_in":    1,
				"token_type":    "Bearer",
			})
		case "/health":
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	now := time.Now().UTC()
	orgs := repository.NewInMemoryOrganizationRepository()
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "OAuth Org", Status: model.OrganizationStatusActive, OwnerUserID: 7,
		CreatedByUserID: 7, MaxMembers: 20, MaxWorkspaces: 20, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	integrations := repository.NewInMemoryIntegrationRepository()
	securityRepo := repository.NewInMemoryConnectorSecurityRepository()
	cipher, err := NewIntegrationCredentialCipher("unit-test-runtime-key")
	if err != nil {
		t.Fatal(err)
	}
	integrationSvc := NewIntegrationService(integrations, orgs, cipher, true)
	connection, err := integrationSvc.CreateConnection(7, org.ID, model.CreateIntegrationConnectionRequest{
		Provider: model.IntegrationProviderGitHub,
		Name:     "GitHub OAuth",
		AuthType: model.IntegrationAuthOAuth2,
		Config: map[string]any{
			"authorize_url": server.URL + "/authorize",
			"token_url":     server.URL + "/token",
			"health_url":    server.URL + "/health",
			"client_id":     "client-id",
			"redirect_uri":  "http://localhost/callback",
			"scopes":        []any{"tasks:read", "tasks:write"},
		},
		Credentials: map[string]any{"client_secret": "runtime-client-secret"},
	})
	if err != nil {
		t.Fatal(err)
	}

	svc := NewConnectorSecurityService(securityRepo, integrations, orgs, cipher, true)
	svc.SetHTTPClient(server.Client())

	start, err := svc.BeginOAuth(7, org.ID, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(start.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("code_challenge_method") != "S256" || parsed.Query().Get("code_challenge") == "" || start.State == "" {
		t.Fatalf("invalid PKCE start: %+v", start)
	}

	meta, err := svc.CompleteOAuth(7, org.ID, connection.ID, model.CompleteConnectorOAuthRequest{State: start.State, Code: "authorization-code"})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Status != model.ConnectorCredentialActive || meta.CredentialVersion != 1 || len(meta.GrantedScopes) != 2 {
		t.Fatalf("unexpected metadata: %+v", meta)
	}

	testResult, err := svc.TestConnection(7, org.ID, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !testResult.Healthy || !testResult.ScopesValid || testResult.HTTPStatus != http.StatusNoContent {
		t.Fatalf("unexpected connection test: %+v", testResult)
	}

	rotated, err := svc.Rotate(7, org.ID, connection.ID, model.RotateConnectorCredentialRequest{
		SecretBackend: model.ConnectorSecretBackendDatabase,
		KeyVersion:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rotated.SecretBackend != model.ConnectorSecretBackendDatabase || rotated.KeyVersion != 2 || rotated.CredentialVersion != 2 {
		t.Fatalf("unexpected rotation: %+v", rotated)
	}

	time.Sleep(1100 * time.Millisecond)
	refreshed, err := svc.RefreshDueSystem(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed) != 1 || refreshed[0].CredentialVersion != 3 || tokenCalls < 2 {
		t.Fatalf("unexpected refresh: %+v calls=%d", refreshed, tokenCalls)
	}
	audit, err := svc.AccessAudit(7, org.ID, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]bool{}
	for _, item := range audit {
		actions[item.Action] = true
	}
	for _, action := range []string{"oauth_exchange_read", "write", "connection_test_read", "rotate_read", "rotate", "automatic_refresh_read", "automatic_refresh"} {
		if !actions[action] {
			t.Fatalf("missing credential access audit action %q: %+v", action, audit)
		}
	}
}


func TestConnectorCredentialPolicyLeastPrivilegeAndAutomaticRotation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "least-privilege-access-token",
				"refresh_token": "least-privilege-refresh-token",
				"scope":         "tasks:read admin:all",
				"expires_in":    3600,
				"token_type":    "Bearer",
			})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	now := time.Now().UTC()
	orgs := repository.NewInMemoryOrganizationRepository()
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Governed OAuth Org", Status: model.OrganizationStatusActive, OwnerUserID: 17,
		CreatedByUserID: 17, MaxMembers: 20, MaxWorkspaces: 20, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	integrations := repository.NewInMemoryIntegrationRepository()
	securityRepo := repository.NewInMemoryConnectorSecurityRepository()
	cipher, err := NewIntegrationCredentialCipher("unit-test-governance-key")
	if err != nil {
		t.Fatal(err)
	}
	integrationSvc := NewIntegrationService(integrations, orgs, cipher, true)
	connection, err := integrationSvc.CreateConnection(17, org.ID, model.CreateIntegrationConnectionRequest{
		Provider: model.IntegrationProviderGitHub,
		Name:     "Least Privilege OAuth",
		AuthType: model.IntegrationAuthOAuth2,
		Config: map[string]any{
			"authorize_url": server.URL + "/authorize",
			"token_url":     server.URL + "/token",
			"client_id":     "governed-client",
			"redirect_uri":  "http://localhost/callback",
			"scopes":        []any{"tasks:read"},
		},
		Credentials: map[string]any{"client_secret": "governed-client-secret"},
	})
	if err != nil {
		t.Fatal(err)
	}

	svc := NewConnectorSecurityService(securityRepo, integrations, orgs, cipher, true)
	svc.SetHTTPClient(server.Client())

	policy, err := svc.UpdateCredentialPolicy(17, org.ID, model.UpdateConnectorCredentialPolicyRequest{
		RotationEnabled:             true,
		AutoRotate:                  true,
		RotationIntervalDays:        1,
		EnforceLeastPrivilegeScopes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.RotationEnabled || !policy.AutoRotate || !policy.EnforceLeastPrivilegeScopes {
		t.Fatalf("unexpected policy: %+v", policy)
	}

	start, err := svc.BeginOAuth(17, org.ID, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CompleteOAuth(17, org.ID, connection.ID, model.CompleteConnectorOAuthRequest{
		State: start.State,
		Code:  "strict-scope-code",
	})
	if !errors.Is(err, ErrConnectorScopeExcess) {
		t.Fatalf("expected least-privilege rejection, got %v", err)
	}

	_, err = svc.UpdateCredentialPolicy(17, org.ID, model.UpdateConnectorCredentialPolicyRequest{
		RotationEnabled:             true,
		AutoRotate:                  true,
		RotationIntervalDays:        1,
		EnforceLeastPrivilegeScopes: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	start, err = svc.BeginOAuth(17, org.ID, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := svc.CompleteOAuth(17, org.ID, connection.ID, model.CompleteConnectorOAuthRequest{
		State: start.State,
		Code:  "permissive-scope-code",
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.CredentialVersion != 1 || len(meta.GrantedScopes) != 2 {
		t.Fatalf("unexpected metadata: %+v", meta)
	}

	_, err = svc.UpdateCredentialPolicy(17, org.ID, model.UpdateConnectorCredentialPolicyRequest{
		RotationEnabled:             true,
		AutoRotate:                  true,
		RotationIntervalDays:        1,
		EnforceLeastPrivilegeScopes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := svc.TestConnection(17, org.ID, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.ScopesValid || assessment.LeastPrivilegeValid || len(assessment.ExcessScopes) != 1 || assessment.ExcessScopes[0] != "admin:all" {
		t.Fatalf("unexpected least-privilege assessment: %+v", assessment)
	}

	past := time.Now().UTC().Add(-48 * time.Hour)
	meta.RotatedAt = &past
	meta.UpdatedAt = past
	if _, err := securityRepo.UpsertCredentialMetadata(meta); err != nil {
		t.Fatal(err)
	}
	rotated, err := svc.RotateDueSystem(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rotated) != 1 || rotated[0].CredentialVersion != 2 || rotated[0].KeyVersion != 2 || rotated[0].RotationDueAt == nil {
		t.Fatalf("unexpected automatic rotation: %+v", rotated)
	}

	current, err := svc.Credential(17, org.ID, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.RotationDueAt == nil || !current.RotationDueAt.After(time.Now().UTC()) {
		t.Fatalf("expected future rotation due date: %+v", current)
	}

	audit, err := svc.AccessAudit(17, org.ID, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]bool{}
	for _, item := range audit {
		actions[item.Action] = true
	}
	for _, action := range []string{"oauth_exchange_read", "connection_test_read", "automatic_rotate_read", "automatic_rotate"} {
		if !actions[action] {
			t.Fatalf("missing governed access action %q: %+v", action, audit)
		}
	}
}
