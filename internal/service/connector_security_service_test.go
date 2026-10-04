package service

import (
	"encoding/json"
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
		SecretBackend: model.ConnectorSecretBackendAWS,
		KeyVersion:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rotated.SecretBackend != model.ConnectorSecretBackendAWS || rotated.KeyVersion != 2 || rotated.CredentialVersion != 2 {
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
}
