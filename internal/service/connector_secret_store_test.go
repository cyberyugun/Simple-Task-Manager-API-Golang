package service

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

type vaultTestState struct {
	mu      sync.Mutex
	payload string
	version int
	deleted bool
	token   string
	ns      string
}

func newVaultTestServer(t *testing.T) (*httptest.Server, *vaultTestState) {
	t.Helper()
	state := &vaultTestState{token: "vault-token", ns: "tenant-a"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != state.token {
			http.Error(w, "bad token", http.StatusForbidden)
			return
		}
		if r.Header.Get("X-Vault-Namespace") != state.ns {
			http.Error(w, "bad namespace", http.StatusForbidden)
			return
		}
		if !strings.Contains(r.URL.Path, "/v1/secret/") {
			http.NotFound(w, r)
			return
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/data/"):
			var body struct {
				Data map[string]string `json:"data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			state.payload = body.Data["payload"]
			state.version++
			state.deleted = false
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"version": state.version}})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/data/"):
			if state.deleted || state.payload == "" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"data": map[string]any{"payload": state.payload},
					"metadata": map[string]any{"version": state.version},
				},
			})
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/metadata/"):
			state.deleted = true
			state.payload = ""
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unsupported", http.StatusBadRequest)
		}
	}))
	return server, state
}

func TestHashiCorpVaultSecretStoreKVv2Lifecycle(t *testing.T) {
	server, state := newVaultTestServer(t)
	defer server.Close()

	store, err := NewHashiCorpVaultSecretStore(HashiCorpVaultSecretStoreConfig{
		Address: server.URL, Token: state.token, Namespace: state.ns,
		Mount: "secret", Prefix: "simple-task-manager/connectors",
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ref := "org/7/integration/9"
	version, err := store.Put(t.Context(), ref, []byte(`{"access_token":"secret-token"}`))
	if err != nil || version != 1 {
		t.Fatalf("put version=%d err=%v", version, err)
	}
	raw, version, err := store.Get(t.Context(), ref)
	if err != nil || version != 1 || string(raw) != `{"access_token":"secret-token"}` {
		t.Fatalf("get raw=%s version=%d err=%v", raw, version, err)
	}
	if err := store.Delete(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Get(t.Context(), ref); err == nil {
		t.Fatal("expected deleted Vault secret to be unavailable")
	}
}

func TestHashiCorpVaultSecretStoreRejectsUnsafeConfiguration(t *testing.T) {
	if _, err := NewHashiCorpVaultSecretStore(HashiCorpVaultSecretStoreConfig{
		Address: "http://vault.example.test", Token: "token",
	}); err == nil {
		t.Fatal("expected insecure HTTP Vault address to be rejected")
	}
	if _, err := NewHashiCorpVaultSecretStore(HashiCorpVaultSecretStoreConfig{
		Address: "https://vault.example.test", Token: "token", Prefix: "../escape",
	}); err == nil {
		t.Fatal("expected unsafe Vault prefix to be rejected")
	}
}

func TestConnectorVaultMigrationBecomesAuthoritativeForRuntimeDelivery(t *testing.T) {
	vaultServer, vaultState := newVaultTestServer(t)
	defer vaultServer.Close()

	var seenAuthorization string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuthorization = r.Header.Get("Authorization")
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	now := time.Now().UTC()
	orgs := repository.NewInMemoryOrganizationRepository()
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Vault Org", Status: model.OrganizationStatusActive, OwnerUserID: 7,
		CreatedByUserID: 7, MaxMembers: 20, MaxWorkspaces: 20, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	integrations := repository.NewInMemoryIntegrationRepository()
	securityRepo := repository.NewInMemoryConnectorSecurityRepository()
	cipher, err := NewIntegrationCredentialCipher("unit-test-vault-runtime-key")
	if err != nil {
		t.Fatal(err)
	}
	integrationSvc := NewIntegrationService(integrations, orgs, cipher, true)
	connection, err := integrationSvc.CreateConnection(7, org.ID, model.CreateIntegrationConnectionRequest{
		Provider: model.IntegrationProviderGitHub,
		Name: "Vault-backed GitHub",
		AuthType: model.IntegrationAuthBearerToken,
		Config: map[string]any{"target_url": target.URL},
		Credentials: map[string]any{"access_token": "token-before-vault"},
	})
	if err != nil {
		t.Fatal(err)
	}

	securitySvc := NewConnectorSecurityService(securityRepo, integrations, orgs, cipher, true)
	vaultStore, err := NewHashiCorpVaultSecretStore(HashiCorpVaultSecretStoreConfig{
		Address: vaultServer.URL, Token: vaultState.token, Namespace: vaultState.ns,
		Mount: "secret", Prefix: "simple-task-manager/connectors", AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	securitySvc.RegisterSecretStore(vaultStore)
	integrationSvc.SetCredentialProvider(securitySvc)

	backends := securitySvc.SecretBackends()
	var vaultConfigured, awsConfigured bool
	for _, backend := range backends {
		if backend.Key == model.ConnectorSecretBackendVault {
			vaultConfigured = backend.Configured && backend.NativeAdapter
		}
		if backend.Key == model.ConnectorSecretBackendAWS {
			awsConfigured = backend.Configured
		}
	}
	if !vaultConfigured || awsConfigured {
		t.Fatalf("unexpected backend readiness vault=%v aws=%v", vaultConfigured, awsConfigured)
	}

	meta, err := securitySvc.Rotate(7, org.ID, connection.ID, model.RotateConnectorCredentialRequest{
		SecretBackend: model.ConnectorSecretBackendVault,
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.SecretBackend != model.ConnectorSecretBackendVault || meta.KeyVersion != 1 {
		t.Fatalf("meta=%+v", meta)
	}
	dbSecret, err := integrations.GetIntegrationConnectionSecret(connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dbSecret.EncryptedCredentials != "" {
		t.Fatal("database credential copy was not scrubbed after Vault migration")
	}

	if _, err := integrationSvc.QueueDelivery(7, org.ID, model.CreateIntegrationDeliveryRequest{
		ConnectionID: connection.ID, EventType: "task.updated", Payload: map[string]any{"id": 1},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := integrationSvc.ProcessBatch("test-worker", 10); err != nil {
		t.Fatal(err)
	}
	if seenAuthorization != "Bearer token-before-vault" {
		t.Fatalf("authorization=%q", seenAuthorization)
	}

	connection, err = integrationSvc.UpdateConnection(7, org.ID, connection.ID, model.UpdateIntegrationConnectionRequest{
		Name: connection.Name, Status: model.IntegrationConnectionActive, Config: map[string]any{"target_url": target.URL},
		Credentials: map[string]any{"access_token": "token-after-vault"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if connection.ID == 0 {
		t.Fatal("updated connection missing")
	}
	if _, err := integrationSvc.QueueDelivery(7, org.ID, model.CreateIntegrationDeliveryRequest{
		ConnectionID: connection.ID, EventType: "task.completed", Payload: map[string]any{"id": 2},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := integrationSvc.ProcessBatch("test-worker", 10); err != nil {
		t.Fatal(err)
	}
	if seenAuthorization != "Bearer token-after-vault" {
		t.Fatalf("updated authorization=%q", seenAuthorization)
	}

	vaultState.mu.Lock()
	payload := vaultState.payload
	version := vaultState.version
	vaultState.mu.Unlock()
	decoded, err := base64.RawStdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(decoded, &stored); err != nil {
		t.Fatal(err)
	}
	if stored["access_token"] != "token-after-vault" || version < 2 {
		t.Fatalf("stored=%v version=%d", stored, version)
	}
}

func TestConnectorRejectsUnconfiguredExternalBackendRotation(t *testing.T) {
	now := time.Now().UTC()
	orgs := repository.NewInMemoryOrganizationRepository()
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Unavailable Vault Org", Status: model.OrganizationStatusActive, OwnerUserID: 5,
		CreatedByUserID: 5, MaxMembers: 10, MaxWorkspaces: 10, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	integrations := repository.NewInMemoryIntegrationRepository()
	cipher, err := NewIntegrationCredentialCipher("unit-test-unavailable-backend-key")
	if err != nil {
		t.Fatal(err)
	}
	integrationSvc := NewIntegrationService(integrations, orgs, cipher, true)
	connection, err := integrationSvc.CreateConnection(5, org.ID, model.CreateIntegrationConnectionRequest{
		Provider: model.IntegrationProviderGitHub, Name: "GitHub", AuthType: model.IntegrationAuthBearerToken,
		Config: map[string]any{"target_url": "http://localhost:9999"},
		Credentials: map[string]any{"access_token": "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	securitySvc := NewConnectorSecurityService(repository.NewInMemoryConnectorSecurityRepository(), integrations, orgs, cipher, true)
	_, err = securitySvc.Rotate(5, org.ID, connection.ID, model.RotateConnectorCredentialRequest{
		SecretBackend: model.ConnectorSecretBackendAWS,
	})
	if err != ErrConnectorSecretBackendUnavailable {
		t.Fatalf("error=%v want=%v", err, ErrConnectorSecretBackendUnavailable)
	}
}
