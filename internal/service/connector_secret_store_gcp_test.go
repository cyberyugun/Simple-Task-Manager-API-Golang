package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
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

type gcpSecretTestState struct {
	mu           sync.Mutex
	token        string
	secrets      map[string][][]byte
	deleted      map[string]bool
	cmekKey      string
	createCalls  int
	versionCalls int
}

func newGCPSecretManagerTestServer(t *testing.T, token string) (*httptest.Server, *gcpSecretTestState) {
	t.Helper()
	state := &gcpSecretTestState{
		token: token, secrets: map[string][][]byte{}, deleted: map[string]bool{},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+state.token {
			http.Error(w, "bad authorization", http.StatusUnauthorized)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/projects/test-project/secrets") {
			http.NotFound(w, r)
			return
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		base := "/v1/projects/test-project/secrets"
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base:
			secretID := r.URL.Query().Get("secretId")
			if secretID == "" {
				http.Error(w, "missing secretId", http.StatusBadRequest)
				return
			}
			if _, exists := state.secrets[secretID]; exists && !state.deleted[secretID] {
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"error":{"code":409,"message":"exists","status":"ALREADY_EXISTS"}}`)
				return
			}
			var body struct {
				Replication struct {
					Automatic struct {
						CustomerManagedEncryption struct {
							KMSKeyName string `json:"kmsKeyName"`
						} `json:"customerManagedEncryption"`
					} `json:"automatic"`
				} `json:"replication"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			state.secrets[secretID] = nil
			state.deleted[secretID] = false
			state.cmekKey = body.Replication.Automatic.CustomerManagedEncryption.KMSKeyName
			state.createCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "projects/test-project/secrets/" + secretID,
			})

		case strings.HasSuffix(r.URL.Path, ":addVersion") && r.Method == http.MethodPost:
			secretID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, base+"/"), ":addVersion")
			if _, exists := state.secrets[secretID]; !exists || state.deleted[secretID] {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"error":{"code":404,"message":"not found","status":"NOT_FOUND"}}`)
				return
			}
			var body struct {
				Payload struct {
					Data string `json:"data"`
				} `json:"payload"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			raw, err := base64.StdEncoding.DecodeString(body.Payload.Data)
			if err != nil {
				t.Fatal(err)
			}
			state.secrets[secretID] = append(state.secrets[secretID], raw)
			state.versionCalls++
			version := len(state.secrets[secretID])
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":  "projects/test-project/secrets/" + secretID + "/versions/" + strconvItoa(version),
				"state": "ENABLED",
			})

		case strings.HasSuffix(r.URL.Path, "/versions/latest:access") && r.Method == http.MethodGet:
			secretID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, base+"/"), "/versions/latest:access")
			versions, exists := state.secrets[secretID]
			if !exists || state.deleted[secretID] || len(versions) == 0 {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"error":{"code":404,"message":"not found","status":"NOT_FOUND"}}`)
				return
			}
			version := len(versions)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "projects/test-project/secrets/" + secretID + "/versions/" + strconvItoa(version),
				"payload": map[string]any{
					"data": base64.StdEncoding.EncodeToString(versions[version-1]),
				},
			})

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, base+"/"):
			secretID := strings.TrimPrefix(r.URL.Path, base+"/")
			if _, exists := state.secrets[secretID]; !exists || state.deleted[secretID] {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"error":{"code":404,"message":"not found","status":"NOT_FOUND"}}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "projects/test-project/secrets/" + secretID,
			})

		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, base+"/"):
			secretID := strings.TrimPrefix(r.URL.Path, base+"/")
			if _, exists := state.secrets[secretID]; !exists {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"error":{"code":404,"message":"not found","status":"NOT_FOUND"}}`)
				return
			}
			state.deleted[secretID] = true
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "unsupported", http.StatusBadRequest)
		}
	}))
	return server, state
}

func TestGCPSecretManagerLifecycleAndCMEK(t *testing.T) {
	server, state := newGCPSecretManagerTestServer(t, "gcp-test-token")
	defer server.Close()

	cmek := "projects/kms-project/locations/global/keyRings/connectors/cryptoKeys/secret-manager"
	store, err := NewGCPSecretManagerSecretStore(GCPSecretManagerConfig{
		ProjectID: "test-project", Prefix: "stm-connectors", CMEKKeyName: cmek,
		Endpoint: server.URL, AccessToken: "gcp-test-token", AllowInsecure: true,
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
		t.Fatal("expected deleted GCP secret to be unavailable")
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.cmekKey != cmek || state.createCalls != 1 || state.versionCalls != 2 {
		t.Fatalf("cmek=%q create=%d versions=%d", state.cmekKey, state.createCalls, state.versionCalls)
	}
}

func TestGCPMetadataWorkloadIdentityTokenProvider(t *testing.T) {
	metadataCalls := 0
	metadata := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadataCalls++
		if r.Header.Get("Metadata-Flavor") != "Google" {
			t.Fatal("Metadata-Flavor header missing")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "workload-gcp-token",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer metadata.Close()

	secrets, _ := newGCPSecretManagerTestServer(t, "workload-gcp-token")
	defer secrets.Close()

	store, err := NewGCPSecretManagerSecretStore(GCPSecretManagerConfig{
		ProjectID: "test-project", Endpoint: secrets.URL, UseMetadata: true,
		MetadataEndpoint: metadata.URL, AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(t.Context(), "org/1/integration/1", []byte(`{"token":"workload"}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Get(t.Context(), "org/1/integration/1"); err != nil {
		t.Fatal(err)
	}
	if metadataCalls != 1 {
		t.Fatalf("metadata calls=%d want=1 due to token cache", metadataCalls)
	}
}

func TestGCPSecretManagerRejectsUnsafeConfiguration(t *testing.T) {
	if _, err := NewGCPSecretManagerSecretStore(GCPSecretManagerConfig{
		ProjectID: "test-project", Endpoint: "http://secretmanager.example.test",
		AccessToken: "token",
	}); err == nil {
		t.Fatal("expected insecure Secret Manager endpoint to be rejected")
	}
	if _, err := NewGCPSecretManagerSecretStore(GCPSecretManagerConfig{
		ProjectID: "test-project", Endpoint: "https://secretmanager.googleapis.com",
		UseMetadata: false,
	}); err == nil {
		t.Fatal("expected missing identity configuration to be rejected")
	}
	if _, err := NewGCPSecretManagerSecretStore(GCPSecretManagerConfig{
		ProjectID: "test-project", Endpoint: "https://secretmanager.googleapis.com",
		AccessToken: "token", CMEKKeyName: "../unsafe",
	}); err == nil {
		t.Fatal("expected invalid CMEK name to be rejected")
	}
}

func strconvItoa(value int) string {
	return fmt.Sprintf("%d", value)
}

func TestConnectorGCPMigrationBecomesAuthoritativeForRuntimeDelivery(t *testing.T) {
	secrets, _ := newGCPSecretManagerTestServer(t, "gcp-runtime-token")
	defer secrets.Close()

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
		Name: "GCP Secret Org", Status: model.OrganizationStatusActive, OwnerUserID: 7,
		CreatedByUserID: 7, MaxMembers: 20, MaxWorkspaces: 20, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	integrations := repository.NewInMemoryIntegrationRepository()
	securityRepo := repository.NewInMemoryConnectorSecurityRepository()
	cipher, err := NewIntegrationCredentialCipher("unit-test-gcp-runtime-key")
	if err != nil {
		t.Fatal(err)
	}
	integrationSvc := NewIntegrationService(integrations, orgs, cipher, true)
	connection, err := integrationSvc.CreateConnection(7, org.ID, model.CreateIntegrationConnectionRequest{
		Provider: model.IntegrationProviderGitHub, Name: "GCP-backed GitHub",
		AuthType: model.IntegrationAuthBearerToken, Config: map[string]any{"target_url": target.URL},
		Credentials: map[string]any{"access_token": "token-before-gcp"},
	})
	if err != nil {
		t.Fatal(err)
	}

	securitySvc := NewConnectorSecurityService(securityRepo, integrations, orgs, cipher, true)
	gcpStore, err := NewGCPSecretManagerSecretStore(GCPSecretManagerConfig{
		ProjectID: "test-project", Endpoint: secrets.URL,
		AccessToken: "gcp-runtime-token", AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	securitySvc.RegisterSecretStore(gcpStore)
	integrationSvc.SetCredentialProvider(securitySvc)

	var configured bool
	for _, backend := range securitySvc.SecretBackends() {
		if backend.Key == model.ConnectorSecretBackendGCP {
			configured = backend.Configured && backend.NativeAdapter
		}
	}
	if !configured {
		t.Fatal("GCP backend should be configured and native")
	}

	meta, err := securitySvc.Rotate(7, org.ID, connection.ID, model.RotateConnectorCredentialRequest{
		SecretBackend: model.ConnectorSecretBackendGCP,
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.SecretBackend != model.ConnectorSecretBackendGCP || meta.KeyVersion != 1 {
		t.Fatalf("metadata=%+v", meta)
	}
	dbSecret, err := integrations.GetIntegrationConnectionSecret(connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dbSecret.EncryptedCredentials != "" {
		t.Fatal("database credential copy was not scrubbed after GCP migration")
	}

	if _, err := integrationSvc.QueueDelivery(7, org.ID, model.CreateIntegrationDeliveryRequest{
		ConnectionID: connection.ID, EventType: "task.updated", Payload: map[string]any{"id": 1},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := integrationSvc.ProcessBatch("gcp-test-worker", 10); err != nil {
		t.Fatal(err)
	}
	if seenAuthorization != "Bearer token-before-gcp" {
		t.Fatalf("authorization=%q", seenAuthorization)
	}

	connection, err = integrationSvc.UpdateConnection(7, org.ID, connection.ID, model.UpdateIntegrationConnectionRequest{
		Name: connection.Name, Status: model.IntegrationConnectionActive,
		Config:      map[string]any{"target_url": target.URL},
		Credentials: map[string]any{"access_token": "token-after-gcp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := integrationSvc.QueueDelivery(7, org.ID, model.CreateIntegrationDeliveryRequest{
		ConnectionID: connection.ID, EventType: "task.completed", Payload: map[string]any{"id": 2},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := integrationSvc.ProcessBatch("gcp-test-worker", 10); err != nil {
		t.Fatal(err)
	}
	if seenAuthorization != "Bearer token-after-gcp" {
		t.Fatalf("updated authorization=%q", seenAuthorization)
	}
}
