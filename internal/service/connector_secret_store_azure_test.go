package service

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

type azureVaultTestState struct {
	mu           sync.Mutex
	token        string
	secrets      map[string]string
	deleted      map[string]bool
	encryptCalls int
	decryptCalls int
}

func newAzureVaultTestServer(t *testing.T, token string) (*httptest.Server, *azureVaultTestState) {
	t.Helper()
	state := &azureVaultTestState{
		token: token, secrets: map[string]string{}, deleted: map[string]bool{},
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+state.token {
			http.Error(w, "bad authorization", http.StatusUnauthorized)
			return
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasPrefix(r.URL.Path, "/secrets/"):
			name := strings.TrimPrefix(r.URL.Path, "/secrets/")
			if r.URL.Query().Get("api-version") != azureKeyVaultAPIVersion {
				http.Error(w, "bad api version", http.StatusBadRequest)
				return
			}
			switch r.Method {
			case http.MethodGet:
				value, ok := state.secrets[name]
				if !ok || state.deleted[name] {
					w.WriteHeader(http.StatusNotFound)
					_, _ = io.WriteString(w, `{"error":{"code":"SecretNotFound","message":"not found"}}`)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
			case http.MethodPut:
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				state.secrets[name] = strings.TrimSpace(body["value"].(string))
				state.deleted[name] = false
				_ = json.NewEncoder(w).Encode(map[string]any{"id": server.URL + "/secrets/" + name + "/v1"})
			case http.MethodDelete:
				state.deleted[name] = true
				_ = json.NewEncoder(w).Encode(map[string]any{"recoveryId": "deleted/" + name})
			default:
				http.Error(w, "unsupported", http.StatusMethodNotAllowed)
			}
		case strings.HasSuffix(r.URL.Path, "/wrapkey"):
			var body struct {
				Alg   string `json:"alg"`
				Value string `json:"value"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Alg != "RSA-OAEP-256" {
				t.Fatalf("alg=%q", body.Alg)
			}
			raw, err := base64.RawURLEncoding.DecodeString(body.Value)
			if err != nil {
				t.Fatal(err)
			}
			state.encryptCalls++
			wrapped := append([]byte("wrapped:"), raw...)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"kid":   server.URL + "/keys/cmk/v1",
				"value": base64.RawURLEncoding.EncodeToString(wrapped),
			})
		case strings.HasSuffix(r.URL.Path, "/unwrapkey"):
			var body struct {
				Alg   string `json:"alg"`
				Value string `json:"value"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			raw, err := base64.RawURLEncoding.DecodeString(body.Value)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(raw), "wrapped:") {
				t.Fatalf("unexpected wrapped key %q", raw)
			}
			state.decryptCalls++
			plain := strings.TrimPrefix(string(raw), "wrapped:")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"value": base64.RawURLEncoding.EncodeToString([]byte(plain)),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	return server, state
}

func TestAzureKeyVaultSecretStoreLifecycleWithCMK(t *testing.T) {
	server, state := newAzureVaultTestServer(t, "azure-test-token")
	defer server.Close()

	store, err := NewAzureKeyVaultSecretStore(AzureKeyVaultConfig{
		VaultURL: server.URL, Prefix: "stm-connectors", AccessToken: "azure-test-token",
		CMKKeyID: server.URL + "/keys/cmk/v1", AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ref := "org/7/integration/9"
	version, err := store.Put(t.Context(), ref, []byte(`{"access_token":"first"}`))
	if err != nil || version != 1 {
		t.Fatalf("first put version=%d err=%v", version, err)
	}
	version, err = store.Put(t.Context(), ref, []byte(`{"access_token":"second"}`))
	if err != nil || version != 2 {
		t.Fatalf("second put version=%d err=%v", version, err)
	}
	raw, version, err := store.Get(t.Context(), ref)
	if err != nil || version != 2 || string(raw) != `{"access_token":"second"}` {
		t.Fatalf("get raw=%s version=%d err=%v", raw, version, err)
	}
	if err := store.Delete(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Get(t.Context(), ref); err == nil {
		t.Fatal("expected deleted Azure secret to be unavailable")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.encryptCalls < 2 || state.decryptCalls < 2 {
		t.Fatalf("encrypt=%d decrypt=%d", state.encryptCalls, state.decryptCalls)
	}
}

func TestAzureWorkloadIdentityTokenExchange(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "federated-token")
	if err := os.WriteFile(tokenFile, []byte("federated-assertion"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("client_id") != "client-id" ||
			r.Form.Get("scope") != "https://vault.azure.net/.default" ||
			r.Form.Get("client_assertion") != "federated-assertion" {
			t.Fatalf("unexpected token form %v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "workload-access-token",
			"expires_in":   3600,
		})
	}))
	defer identity.Close()

	vault, _ := newAzureVaultTestServer(t, "workload-access-token")
	defer vault.Close()
	store, err := NewAzureKeyVaultSecretStore(AzureKeyVaultConfig{
		VaultURL: vault.URL, TenantID: "tenant", ClientID: "client-id",
		FederatedTokenFile: tokenFile, AuthorityHost: identity.URL, AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(t.Context(), "org/1/integration/1", []byte(`{"token":"workload"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestAzureManagedIdentityTokenProvider(t *testing.T) {
	identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata") != "true" {
			t.Fatal("managed identity Metadata header missing")
		}
		if r.URL.Query().Get("resource") != "https://vault.azure.net" ||
			r.URL.Query().Get("client_id") != "managed-client" {
			t.Fatalf("unexpected query %v", r.URL.Query())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "managed-access-token",
			"expires_on":   time.Now().UTC().Add(time.Hour).Unix(),
		})
	}))
	defer identity.Close()

	vault, _ := newAzureVaultTestServer(t, "managed-access-token")
	defer vault.Close()
	store, err := NewAzureKeyVaultSecretStore(AzureKeyVaultConfig{
		VaultURL: vault.URL, ClientID: "managed-client", UseManagedIdentity: true,
		ManagedIdentityEndpoint: identity.URL, AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(t.Context(), "org/2/integration/2", []byte(`{"token":"managed"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestConnectorAzureMigrationBecomesAuthoritativeForRuntimeDelivery(t *testing.T) {
	vault, _ := newAzureVaultTestServer(t, "azure-runtime-token")
	defer vault.Close()

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
		Name: "Azure Secret Org", Status: model.OrganizationStatusActive, OwnerUserID: 7,
		CreatedByUserID: 7, MaxMembers: 20, MaxWorkspaces: 20, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	integrations := repository.NewInMemoryIntegrationRepository()
	securityRepo := repository.NewInMemoryConnectorSecurityRepository()
	cipher, err := NewIntegrationCredentialCipher("unit-test-azure-runtime-key")
	if err != nil {
		t.Fatal(err)
	}
	integrationSvc := NewIntegrationService(integrations, orgs, cipher, true)
	connection, err := integrationSvc.CreateConnection(7, org.ID, model.CreateIntegrationConnectionRequest{
		Provider: model.IntegrationProviderGitHub, Name: "Azure-backed GitHub",
		AuthType: model.IntegrationAuthBearerToken, Config: map[string]any{"target_url": target.URL},
		Credentials: map[string]any{"access_token": "token-before-azure"},
	})
	if err != nil {
		t.Fatal(err)
	}

	securitySvc := NewConnectorSecurityService(securityRepo, integrations, orgs, cipher, true)
	azureStore, err := NewAzureKeyVaultSecretStore(AzureKeyVaultConfig{
		VaultURL: vault.URL, AccessToken: "azure-runtime-token", AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	securitySvc.RegisterSecretStore(azureStore)
	integrationSvc.SetCredentialProvider(securitySvc)

	var configured bool
	for _, backend := range securitySvc.SecretBackends() {
		if backend.Key == model.ConnectorSecretBackendAzure {
			configured = backend.Configured && backend.NativeAdapter
		}
	}
	if !configured {
		t.Fatal("Azure backend should be configured and native")
	}

	meta, err := securitySvc.Rotate(7, org.ID, connection.ID, model.RotateConnectorCredentialRequest{
		SecretBackend: model.ConnectorSecretBackendAzure,
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.SecretBackend != model.ConnectorSecretBackendAzure || meta.KeyVersion != 1 {
		t.Fatalf("metadata=%+v", meta)
	}
	dbSecret, err := integrations.GetIntegrationConnectionSecret(connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dbSecret.EncryptedCredentials != "" {
		t.Fatal("database credential copy was not scrubbed after Azure migration")
	}

	if _, err := integrationSvc.QueueDelivery(7, org.ID, model.CreateIntegrationDeliveryRequest{
		ConnectionID: connection.ID, EventType: "task.updated", Payload: map[string]any{"id": 1},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := integrationSvc.ProcessBatch("azure-test-worker", 10); err != nil {
		t.Fatal(err)
	}
	if seenAuthorization != "Bearer token-before-azure" {
		t.Fatalf("authorization=%q", seenAuthorization)
	}

	connection, err = integrationSvc.UpdateConnection(7, org.ID, connection.ID, model.UpdateIntegrationConnectionRequest{
		Name: connection.Name, Status: model.IntegrationConnectionActive,
		Config:      map[string]any{"target_url": target.URL},
		Credentials: map[string]any{"access_token": "token-after-azure"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := integrationSvc.QueueDelivery(7, org.ID, model.CreateIntegrationDeliveryRequest{
		ConnectionID: connection.ID, EventType: "task.completed", Payload: map[string]any{"id": 2},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := integrationSvc.ProcessBatch("azure-test-worker", 10); err != nil {
		t.Fatal(err)
	}
	if seenAuthorization != "Bearer token-after-azure" {
		t.Fatalf("updated authorization=%q", seenAuthorization)
	}
}

func TestAzureKeyVaultRejectsUnsafeConfiguration(t *testing.T) {
	if _, err := NewAzureKeyVaultSecretStore(AzureKeyVaultConfig{
		VaultURL: "http://vault.example.test", AccessToken: "token",
	}); err == nil {
		t.Fatal("expected insecure vault URL to be rejected")
	}
	if _, err := NewAzureKeyVaultSecretStore(AzureKeyVaultConfig{
		VaultURL: "https://vault.example.test", TenantID: "tenant", ClientID: "client",
	}); err == nil {
		t.Fatal("expected incomplete workload identity configuration to be rejected")
	}
}

func TestParseAzureTokenResponse(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"access_token": "token", "expires_in": 1800})
	token, expiry, err := parseAzureTokenResponse(raw)
	if err != nil || token != "token" || !expiry.After(time.Now().UTC().Add(20*time.Minute)) {
		t.Fatalf("token=%q expiry=%v err=%v", token, expiry, err)
	}
}

func TestAzureSecretNameIsStableAndSafe(t *testing.T) {
	vaultURL, _ := url.Parse("https://vault.example.test")
	store := &AzureKeyVaultSecretStore{vaultURL: vaultURL, prefix: "stm-connectors"}
	first, err := store.secretName("org/7/integration/9")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.secretName("org/7/integration/9")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !validAzureSecretName(first) {
		t.Fatalf("first=%q second=%q", first, second)
	}
}
