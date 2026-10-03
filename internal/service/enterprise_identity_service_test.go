package service

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func newEnterpriseTestService(t *testing.T) (*EnterpriseIdentityService, model.User, model.WorkspaceAccess) {
	t.Helper()
	users := repository.NewInMemoryUserRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	enterprise := repository.NewInMemoryEnterpriseIdentityRepository()
	tokens := auth.NewTokenManager("12345678901234567890123456789012", time.Hour)

	now := time.Now()
	user, err := users.Create(model.User{
		Name: "Owner", Email: "owner@example.com", PasswordHash: "unused",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaces.Create(user.ID, "Acme", now)
	if err != nil {
		t.Fatal(err)
	}
	return NewEnterpriseIdentityService(enterprise, workspaces, users, tokens), user, workspace
}

func TestEnterpriseOAuthAuthorizationCodePKCEAndRevocation(t *testing.T) {
	svc, user, workspace := newEnterpriseTestService(t)
	clientResult, err := svc.CreateOAuthClient(user.ID, workspace.ID, model.CreateOAuthClientRequest{
		Name:          "CLI",
		RedirectURIs:  []string{"https://app.example.com/callback"},
		AllowedScopes: []string{model.ScopeTasksRead, model.ScopeTasksWrite},
	})
	if err != nil {
		t.Fatal(err)
	}

	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	code, err := svc.AuthorizeCode(user.ID, workspace.ID, model.OAuthAuthorizeRequest{
		ClientID:      clientResult.Client.ClientID,
		RedirectURI:   "https://app.example.com/callback",
		Scopes:        []string{model.ScopeTasksRead},
		CodeChallenge: challenge,
	})
	if err != nil {
		t.Fatal(err)
	}

	token, err := svc.ExchangeToken(model.OAuthTokenRequest{
		GrantType:    "authorization_code",
		ClientID:     clientResult.Client.ClientID,
		ClientSecret: clientResult.ClientSecret,
		Code:         code.Code,
		CodeVerifier: verifier,
		RedirectURI:  "https://app.example.com/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(token.Scopes) != 1 || token.Scopes[0] != model.ScopeTasksRead {
		t.Fatalf("scopes = %v", token.Scopes)
	}

	introspection, err := svc.Introspect(token.AccessToken)
	if err != nil || !introspection.Active {
		t.Fatalf("introspection = %+v, err=%v", introspection, err)
	}
	if err := svc.RevokeAccessToken(user.ID, token.AccessToken); err != nil {
		t.Fatal(err)
	}
	introspection, err = svc.Introspect(token.AccessToken)
	if err != nil || introspection.Active {
		t.Fatalf("revoked introspection = %+v, err=%v", introspection, err)
	}

	if _, err := svc.ExchangeToken(model.OAuthTokenRequest{
		GrantType:    "authorization_code",
		ClientID:     clientResult.Client.ClientID,
		ClientSecret: clientResult.ClientSecret,
		Code:         code.Code,
		CodeVerifier: verifier,
		RedirectURI:  "https://app.example.com/callback",
	}); err == nil {
		t.Fatal("authorization code reuse should fail")
	}
}

func TestEnterpriseClientCredentialsAndAPIKey(t *testing.T) {
	svc, user, workspace := newEnterpriseTestService(t)
	clientResult, err := svc.CreateOAuthClient(user.ID, workspace.ID, model.CreateOAuthClientRequest{
		Name:          "automation",
		RedirectURIs:  []string{"https://automation.example.com/callback"},
		AllowedScopes: []string{model.ScopeTasksRead},
	})
	if err != nil {
		t.Fatal(err)
	}

	token, err := svc.ExchangeToken(model.OAuthTokenRequest{
		GrantType:    "client_credentials",
		ClientID:     clientResult.Client.ClientID,
		ClientSecret: clientResult.ClientSecret,
		Scopes:       []string{model.ScopeTasksRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	introspection, err := svc.Introspect(token.AccessToken)
	if err != nil || !introspection.Active || introspection.TokenUse != model.TokenUseService {
		t.Fatalf("client token introspection = %+v, err=%v", introspection, err)
	}

	key, err := svc.CreateAPIKey(user.ID, workspace.ID, model.CreateAPIKeyRequest{
		Name:   "deployment",
		Scopes: []string{model.ScopeTasksRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	if key.Secret == "" || key.APIKey.KeyHash != "" {
		t.Fatalf("api key result should disclose secret once without hash: %+v", key)
	}
	apiToken, err := svc.ExchangeAPIKey(key.Secret)
	if err != nil {
		t.Fatal(err)
	}
	introspection, err = svc.Introspect(apiToken.AccessToken)
	if err != nil || !introspection.Active || introspection.WorkspaceID != workspace.ID {
		t.Fatalf("api key token introspection = %+v, err=%v", introspection, err)
	}
}

func TestEnterprisePolicyCanDisableServiceAccounts(t *testing.T) {
	svc, user, workspace := newEnterpriseTestService(t)
	_, err := svc.UpdatePolicy(user.ID, workspace.ID, model.UpdateEnterprisePolicyRequest{
		RequireMFA:           true,
		AllowServiceAccounts: false,
		MaxSessionAgeMinutes: 60,
		AllowedEmailDomains:  []string{"example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateAPIKey(user.ID, workspace.ID, model.CreateAPIKeyRequest{
		Name: "blocked", Scopes: []string{model.ScopeTasksRead},
	}); err != ErrServiceAccountsBlocked {
		t.Fatalf("CreateAPIKey() error = %v, want %v", err, ErrServiceAccountsBlocked)
	}
}
