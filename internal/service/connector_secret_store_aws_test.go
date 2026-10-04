package service

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

type awsSecretsTestState struct {
	mu           sync.Mutex
	secrets      map[string]string
	deleted      map[string]bool
	kmsKeyID     string
	authHeaders  []string
	sessionToken []string
}

func newAWSSecretsTestServer(t *testing.T) (*httptest.Server, *awsSecretsTestState) {
	t.Helper()
	state := &awsSecretsTestState{secrets: map[string]string{}, deleted: map[string]bool{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=") {
			http.Error(w, "missing SigV4 authorization", http.StatusForbidden)
			return
		}
		if r.Header.Get("X-Amz-Date") == "" {
			http.Error(w, "missing X-Amz-Date", http.StatusForbidden)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatal(err)
			}
		}
		target := r.Header.Get("X-Amz-Target")
		secretID := strings.TrimSpace(asString(payload["SecretId"]))
		if secretID == "" {
			secretID = strings.TrimSpace(asString(payload["Name"]))
		}

		state.mu.Lock()
		defer state.mu.Unlock()
		state.authHeaders = append(state.authHeaders, r.Header.Get("Authorization"))
		state.sessionToken = append(state.sessionToken, r.Header.Get("X-Amz-Security-Token"))
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")

		switch target {
		case "secretsmanager.GetSecretValue":
			value, ok := state.secrets[secretID]
			if !ok || state.deleted[secretID] {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"__type":"ResourceNotFoundException","Message":"not found"}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ARN": "arn:aws:secretsmanager:test", "Name": secretID, "SecretString": value})
		case "secretsmanager.CreateSecret":
			if _, exists := state.secrets[secretID]; exists && !state.deleted[secretID] {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"__type":"ResourceExistsException","Message":"already exists"}`)
				return
			}
			state.secrets[secretID] = asString(payload["SecretString"])
			state.deleted[secretID] = false
			state.kmsKeyID = asString(payload["KmsKeyId"])
			_ = json.NewEncoder(w).Encode(map[string]any{"ARN": "arn:aws:secretsmanager:test", "Name": secretID, "VersionId": "v1"})
		case "secretsmanager.PutSecretValue":
			if _, exists := state.secrets[secretID]; !exists || state.deleted[secretID] {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"__type":"ResourceNotFoundException","Message":"not found"}`)
				return
			}
			state.secrets[secretID] = asString(payload["SecretString"])
			_ = json.NewEncoder(w).Encode(map[string]any{"ARN": "arn:aws:secretsmanager:test", "Name": secretID, "VersionId": "v2"})
		case "secretsmanager.DeleteSecret":
			if _, exists := state.secrets[secretID]; !exists {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"__type":"ResourceNotFoundException","Message":"not found"}`)
				return
			}
			state.deleted[secretID] = true
			_ = json.NewEncoder(w).Encode(map[string]any{"ARN": "arn:aws:secretsmanager:test", "Name": secretID})
		default:
			http.Error(w, "unsupported target", http.StatusBadRequest)
		}
	}))
	return server, state
}

func TestAWSSecretsManagerSecretStoreLifecycleAndSigV4(t *testing.T) {
	server, state := newAWSSecretsTestServer(t)
	defer server.Close()

	store, err := NewAWSSecretsManagerSecretStore(AWSSecretsManagerConfig{
		Region: "ap-southeast-1", Prefix: "simple-task-manager/connectors",
		KMSKeyID: "arn:aws:kms:ap-southeast-1:123456789012:key/test",
		Endpoint: server.URL, AccessKeyID: "AKIATESTKEY", SecretAccessKey: "test-secret-key",
		SessionToken: "test-session-token", RecoveryWindowDays: 7, AllowInsecure: true,
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
		t.Fatal("expected deleted AWS secret to be unavailable")
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.kmsKeyID == "" {
		t.Fatal("expected configured KMS key id on CreateSecret")
	}
	if len(state.authHeaders) == 0 || !strings.Contains(state.authHeaders[0], "/ap-southeast-1/secretsmanager/aws4_request") {
		t.Fatalf("unexpected authorization headers: %+v", state.authHeaders)
	}
	for _, token := range state.sessionToken {
		if token != "test-session-token" {
			t.Fatalf("session token = %q", token)
		}
	}
}

func TestAWSWebIdentityCredentialProviderUsesSTSWorkloadIdentity(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("signed-service-account-token"), 0o600); err != nil {
		t.Fatal(err)
	}

	sts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("Action") != "AssumeRoleWithWebIdentity" ||
			r.Form.Get("RoleArn") != "arn:aws:iam::123456789012:role/connectors" ||
			r.Form.Get("WebIdentityToken") != "signed-service-account-token" {
			t.Fatalf("unexpected STS form: %v", r.Form)
		}
		expiration := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
		type credentials struct {
			AccessKeyID     string `xml:"AccessKeyId"`
			SecretAccessKey string `xml:"SecretAccessKey"`
			SessionToken    string `xml:"SessionToken"`
			Expiration      string `xml:"Expiration"`
		}
		type result struct {
			Credentials credentials `xml:"Credentials"`
		}
		type response struct {
			XMLName xml.Name `xml:"AssumeRoleWithWebIdentityResponse"`
			Result  result   `xml:"AssumeRoleWithWebIdentityResult"`
		}
		w.Header().Set("Content-Type", "application/xml")
		_ = xml.NewEncoder(w).Encode(response{Result: result{Credentials: credentials{
			AccessKeyID: "ASIAWORKLOAD", SecretAccessKey: "temporary-secret",
			SessionToken: "temporary-session-token", Expiration: expiration,
		}}})
	}))
	defer sts.Close()

	secrets, state := newAWSSecretsTestServer(t)
	defer secrets.Close()

	store, err := NewAWSSecretsManagerSecretStore(AWSSecretsManagerConfig{
		Region: "ap-southeast-1", Endpoint: secrets.URL, STSEndpoint: sts.URL,
		RoleARN: "arn:aws:iam::123456789012:role/connectors", WebIdentityTokenFile: tokenFile,
		RoleSessionName: "connector-workload", AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(t.Context(), "org/1/integration/1", []byte(`{"token":"workload"}`)); err != nil {
		t.Fatal(err)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.authHeaders) == 0 || !strings.Contains(state.authHeaders[len(state.authHeaders)-1], "Credential=ASIAWORKLOAD/") {
		t.Fatalf("authorization headers=%v", state.authHeaders)
	}
	if state.sessionToken[len(state.sessionToken)-1] != "temporary-session-token" {
		t.Fatalf("session tokens=%v", state.sessionToken)
	}
}

func TestConnectorAWSMigrationBecomesAuthoritativeForRuntimeDelivery(t *testing.T) {
	awsServer, _ := newAWSSecretsTestServer(t)
	defer awsServer.Close()

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
		Name: "AWS Secret Org", Status: model.OrganizationStatusActive, OwnerUserID: 7,
		CreatedByUserID: 7, MaxMembers: 20, MaxWorkspaces: 20, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	integrations := repository.NewInMemoryIntegrationRepository()
	securityRepo := repository.NewInMemoryConnectorSecurityRepository()
	cipher, err := NewIntegrationCredentialCipher("unit-test-aws-runtime-key")
	if err != nil {
		t.Fatal(err)
	}
	integrationSvc := NewIntegrationService(integrations, orgs, cipher, true)
	connection, err := integrationSvc.CreateConnection(7, org.ID, model.CreateIntegrationConnectionRequest{
		Provider: model.IntegrationProviderGitHub, Name: "AWS-backed GitHub",
		AuthType: model.IntegrationAuthBearerToken, Config: map[string]any{"target_url": target.URL},
		Credentials: map[string]any{"access_token": "token-before-aws"},
	})
	if err != nil {
		t.Fatal(err)
	}

	securitySvc := NewConnectorSecurityService(securityRepo, integrations, orgs, cipher, true)
	awsStore, err := NewAWSSecretsManagerSecretStore(AWSSecretsManagerConfig{
		Region: "ap-southeast-1", Endpoint: awsServer.URL, Prefix: "simple-task-manager/connectors",
		AccessKeyID: "AKIATEST", SecretAccessKey: "secret", AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	securitySvc.RegisterSecretStore(awsStore)
	integrationSvc.SetCredentialProvider(securitySvc)

	var awsConfigured bool
	for _, backend := range securitySvc.SecretBackends() {
		if backend.Key == model.ConnectorSecretBackendAWS {
			awsConfigured = backend.Configured && backend.NativeAdapter
		}
	}
	if !awsConfigured {
		t.Fatal("AWS backend should be configured and native")
	}

	meta, err := securitySvc.Rotate(7, org.ID, connection.ID, model.RotateConnectorCredentialRequest{
		SecretBackend: model.ConnectorSecretBackendAWS,
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.SecretBackend != model.ConnectorSecretBackendAWS || meta.KeyVersion != 1 {
		t.Fatalf("metadata=%+v", meta)
	}
	dbSecret, err := integrations.GetIntegrationConnectionSecret(connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dbSecret.EncryptedCredentials != "" {
		t.Fatal("database credential copy was not scrubbed after AWS migration")
	}

	if _, err := integrationSvc.QueueDelivery(7, org.ID, model.CreateIntegrationDeliveryRequest{
		ConnectionID: connection.ID, EventType: "task.updated", Payload: map[string]any{"id": 1},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := integrationSvc.ProcessBatch("aws-test-worker", 10); err != nil {
		t.Fatal(err)
	}
	if seenAuthorization != "Bearer token-before-aws" {
		t.Fatalf("authorization=%q", seenAuthorization)
	}

	connection, err = integrationSvc.UpdateConnection(7, org.ID, connection.ID, model.UpdateIntegrationConnectionRequest{
		Name: connection.Name, Status: model.IntegrationConnectionActive,
		Config:      map[string]any{"target_url": target.URL},
		Credentials: map[string]any{"access_token": "token-after-aws"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := integrationSvc.QueueDelivery(7, org.ID, model.CreateIntegrationDeliveryRequest{
		ConnectionID: connection.ID, EventType: "task.completed", Payload: map[string]any{"id": 2},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := integrationSvc.ProcessBatch("aws-test-worker", 10); err != nil {
		t.Fatal(err)
	}
	if seenAuthorization != "Bearer token-after-aws" {
		t.Fatalf("updated authorization=%q", seenAuthorization)
	}
}

func TestAWSSecretsManagerRejectsUnsafeOrIncompleteConfiguration(t *testing.T) {
	if _, err := NewAWSSecretsManagerSecretStore(AWSSecretsManagerConfig{
		Region: "ap-southeast-1", Endpoint: "http://secretsmanager.example.test",
		AccessKeyID: "key", SecretAccessKey: "secret",
	}); err == nil {
		t.Fatal("expected insecure endpoint to be rejected")
	}
	if _, err := NewAWSSecretsManagerSecretStore(AWSSecretsManagerConfig{
		Region: "ap-southeast-1", Endpoint: "https://secretsmanager.example.test",
		RoleARN: "arn:aws:iam::123456789012:role/connectors",
	}); err == nil {
		t.Fatal("expected incomplete web identity configuration to be rejected")
	}
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	text, _ := value.(string)
	return text
}
