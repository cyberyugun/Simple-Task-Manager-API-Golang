package service

import (
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type azureBlobTestState struct {
	mu              sync.Mutex
	exists          bool
	size            int64
	sha256          string
	bearerToken     string
	delegationCalls int
	headCalls       int
	deleteCalls     int
}

func newAzureBlobTestServer(t *testing.T, size int64, sha256, bearerToken string) (*httptest.Server, *azureBlobTestState) {
	t.Helper()
	state := &azureBlobTestState{exists: true, size: size, sha256: sha256, bearerToken: bearerToken}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()

		if r.URL.Query().Get("restype") == "service" && r.URL.Query().Get("comp") == "userdelegationkey" {
			if r.Method != http.MethodPost {
				http.Error(w, "method", http.StatusMethodNotAllowed)
				return
			}
			if r.Header.Get("Authorization") != "Bearer "+state.bearerToken {
				http.Error(w, "auth", http.StatusUnauthorized)
				return
			}
			state.delegationCalls++
			start := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Second)
			expiry := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
			response := azureUserDelegationKey{
				SignedOID:     "object-id",
				SignedTID:     "tenant-id",
				SignedStart:   start.Format(time.RFC3339),
				SignedExpiry:  expiry.Format(time.RFC3339),
				SignedService: "b",
				SignedVersion: azureBlobAPIVersion,
				Value:         base64.StdEncoding.EncodeToString([]byte(strings.Repeat("d", 32))),
			}
			w.Header().Set("Content-Type", "application/xml")
			if err := xml.NewEncoder(w).Encode(response); err != nil {
				t.Fatal(err)
			}
			return
		}

		if !strings.HasPrefix(r.URL.Path, "/files/workspaces/") {
			http.NotFound(w, r)
			return
		}
		if state.bearerToken != "" && r.URL.Query().Get("sig") == "" &&
			r.Header.Get("Authorization") != "Bearer "+state.bearerToken {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodHead:
			state.headCalls++
			if !state.exists {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Length", "1234")
			w.Header().Set("x-ms-meta-sha256", state.sha256)
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			state.deleteCalls++
			state.exists = false
			w.WriteHeader(http.StatusAccepted)
		default:
			http.Error(w, "unsupported", http.StatusMethodNotAllowed)
		}
	}))
	return server, state
}

func TestAzureBlobObjectStoreAccountKeyPresignVerifyAndDelete(t *testing.T) {
	hash := strings.Repeat("a", 64)
	server, state := newAzureBlobTestServer(t, 1234, hash, "")
	defer server.Close()

	accountKey := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	store, err := NewAzureBlobObjectStore(AzureBlobObjectStoreConfig{
		AccountName:     "testaccount",
		AccountKey:      accountKey,
		Container:       "files",
		Endpoint:        server.URL,
		AllowInsecure:   true,
		Encryption:      "azure_cmk",
		EncryptionKeyID: "attachment-scope",
	})
	if err != nil {
		t.Fatal(err)
	}
	key := "workspaces/7/aaaaaaaaaaaa/123-report.pdf"
	upload, headers, err := store.PresignUpload(key, "application/pdf", hash, 1234, time.Now().UTC().Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(upload)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("sig") == "" || parsed.Query().Get("sp") != "cw" ||
		parsed.Query().Get("sr") != "b" || parsed.Query().Get("sv") != azureBlobAPIVersion ||
		parsed.Query().Get("ses") != "attachment-scope" {
		t.Fatalf("unexpected Azure SAS query: %v", parsed.Query())
	}
	if headers["x-ms-blob-type"] != "BlockBlob" ||
		headers["x-ms-meta-sha256"] != hash ||
		headers["x-ms-encryption-scope"] != "attachment-scope" {
		t.Fatalf("unexpected upload headers: %v", headers)
	}
	download, err := store.PresignDownload(key, time.Now().UTC().Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	downloadURL, _ := url.Parse(download)
	if downloadURL.Query().Get("sp") != "r" || downloadURL.Query().Get("sig") == "" {
		t.Fatalf("unexpected download SAS: %s", download)
	}
	if err := store.VerifyUpload(t.Context(), key, 1234, hash); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.exists || state.headCalls < 2 || state.deleteCalls != 1 {
		t.Fatalf("exists=%v head=%d delete=%d", state.exists, state.headCalls, state.deleteCalls)
	}
}

func TestAzureBlobWorkloadIdentityUserDelegationSAS(t *testing.T) {
	hash := strings.Repeat("b", 64)
	identityCalls := 0
	identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identityCalls++
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("scope") != "https://storage.azure.com/.default" ||
			r.Form.Get("client_id") != "storage-client" ||
			r.Form.Get("client_assertion") != "federated-token" {
			t.Fatalf("unexpected identity form: %v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "storage-bearer",
			"expires_in":   3600,
		})
	}))
	defer identity.Close()

	tokenFile := t.TempDir() + "/azure-token"
	if err := os.WriteFile(tokenFile, []byte("federated-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	blob, state := newAzureBlobTestServer(t, 1234, hash, "storage-bearer")
	defer blob.Close()

	store, err := NewAzureBlobObjectStore(AzureBlobObjectStoreConfig{
		AccountName:        "testaccount",
		Container:          "files",
		Endpoint:           blob.URL,
		TenantID:           "tenant",
		ClientID:           "storage-client",
		FederatedTokenFile: tokenFile,
		AuthorityHost:      identity.URL,
		AllowInsecure:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	key := "workspaces/8/bbbbbbbbbbbb/124-report.pdf"
	upload, _, err := store.PresignUpload(key, "application/pdf", hash, 1234, time.Now().UTC().Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(upload)
	if parsed.Query().Get("skoid") != "object-id" ||
		parsed.Query().Get("sktid") != "tenant-id" ||
		parsed.Query().Get("sig") == "" {
		t.Fatalf("unexpected delegation SAS: %v", parsed.Query())
	}
	if err := store.VerifyUpload(t.Context(), key, 1234, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PresignDownload(key, time.Now().UTC().Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if identityCalls != 1 {
		t.Fatalf("identity token calls=%d want=1", identityCalls)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.delegationCalls != 1 {
		t.Fatalf("delegation calls=%d want=1", state.delegationCalls)
	}
}

func TestAzureBlobManagedIdentityTokenProvider(t *testing.T) {
	hash := strings.Repeat("c", 64)
	metadata := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata") != "true" ||
			r.URL.Query().Get("resource") != "https://storage.azure.com/" ||
			r.URL.Query().Get("client_id") != "managed-client" {
			t.Fatalf("unexpected managed identity request: headers=%v query=%v", r.Header, r.URL.Query())
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "managed-storage-token",
			"expires_in":   3600,
		})
	}))
	defer metadata.Close()

	blob, _ := newAzureBlobTestServer(t, 1234, hash, "managed-storage-token")
	defer blob.Close()

	store, err := NewAzureBlobObjectStore(AzureBlobObjectStoreConfig{
		AccountName:             "testaccount",
		Container:               "files",
		Endpoint:                blob.URL,
		ClientID:                "managed-client",
		UseManagedIdentity:      true,
		ManagedIdentityEndpoint: metadata.URL,
		AllowInsecure:           true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PresignDownload("workspaces/9/cccccccccccc/125-report.pdf", time.Now().UTC().Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func TestAzureBlobRejectsUnsafeConfiguration(t *testing.T) {
	accountKey := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	if _, err := NewAzureBlobObjectStore(AzureBlobObjectStoreConfig{
		AccountName: "Bad_Account", AccountKey: accountKey, Container: "files",
	}); err == nil {
		t.Fatal("expected invalid account name rejection")
	}
	if _, err := NewAzureBlobObjectStore(AzureBlobObjectStoreConfig{
		AccountName: "testaccount", AccountKey: accountKey, Container: "Bad_Container",
	}); err == nil {
		t.Fatal("expected invalid container rejection")
	}
	if _, err := NewAzureBlobObjectStore(AzureBlobObjectStoreConfig{
		AccountName: "testaccount", AccountKey: accountKey, Container: "files",
		Endpoint: "http://blob.example.test",
	}); err == nil {
		t.Fatal("expected insecure endpoint rejection")
	}
}
