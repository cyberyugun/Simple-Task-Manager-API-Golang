package service

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type gcsObjectTestState struct {
	mu          sync.Mutex
	token       string
	exists      bool
	size        int64
	sha256      string
	headCalls   int
	deleteCalls int
	signCalls   int
	lastPayload string
}

func newGCSObjectTestServers(t *testing.T, token string, size int64, sha256 string) (*httptest.Server, *httptest.Server, *gcsObjectTestState) {
	t.Helper()
	state := &gcsObjectTestState{token: token, exists: true, size: size, sha256: sha256}

	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+state.token {
			http.Error(w, "bad authorization", http.StatusUnauthorized)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/test-bucket/workspaces/") {
			http.NotFound(w, r)
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
			w.Header().Set("x-goog-meta-sha256", state.sha256)
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			state.deleteCalls++
			state.exists = false
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unsupported", http.StatusMethodNotAllowed)
		}
	}))

	iam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+state.token {
			http.Error(w, "bad authorization", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost ||
			!strings.Contains(r.URL.Path, "/v1/projects/-/serviceAccounts/") ||
			!strings.HasSuffix(r.URL.Path, ":signBlob") {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Payload string `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		raw, err := base64.StdEncoding.DecodeString(body.Payload)
		if err != nil {
			t.Fatal(err)
		}
		state.mu.Lock()
		state.signCalls++
		state.lastPayload = string(raw)
		state.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keyId":      "test-key",
			"signedBlob": base64.StdEncoding.EncodeToString([]byte("test-rsa-signature")),
		})
	}))

	return storage, iam, state
}

func TestGCSObjectStorePresignVerifyDeleteAndCMEK(t *testing.T) {
	hash := strings.Repeat("a", 64)
	storage, iam, state := newGCSObjectTestServers(t, "gcs-test-token", 1234, hash)
	defer storage.Close()
	defer iam.Close()

	store, err := NewGCSObjectStore(GCSObjectStoreConfig{
		Bucket:                 "test-bucket",
		Endpoint:               storage.URL,
		ServiceAccountEmail:    "signer@test-project.iam.gserviceaccount.com",
		IAMCredentialsEndpoint: iam.URL,
		AccessToken:            "gcs-test-token",
		AllowInsecure:          true,
		Encryption:             "gcp_cmek",
		EncryptionKeyID:        "projects/kms-project/locations/global/keyRings/files/cryptoKeys/attachments",
	})
	if err != nil {
		t.Fatal(err)
	}
	key := "workspaces/7/aaaaaaaaaaaa/123-report.pdf"
	upload, headers, err := store.PresignUpload(
		key,
		"application/pdf",
		hash,
		1234,
		time.Now().UTC().Add(10*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(upload)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("X-Goog-Algorithm") != gcsV4Algorithm ||
		query.Get("X-Goog-Signature") == "" ||
		query.Get("X-Goog-Date") == "" ||
		query.Get("X-Goog-Expires") == "" ||
		!strings.Contains(query.Get("X-Goog-Credential"), "/auto/storage/goog4_request") {
		t.Fatalf("unexpected GCS signed URL query: %v", query)
	}
	signedHeaders := query.Get("X-Goog-SignedHeaders")
	for _, want := range []string{"content-type", "host", "x-goog-encryption-kms-key-name", "x-goog-meta-sha256"} {
		if !strings.Contains(signedHeaders, want) {
			t.Fatalf("signed headers %q missing %q", signedHeaders, want)
		}
	}
	if headers["Content-Type"] != "application/pdf" ||
		headers["x-goog-meta-sha256"] != hash ||
		headers["x-goog-encryption-kms-key-name"] == "" {
		t.Fatalf("unexpected upload headers: %v", headers)
	}

	download, err := store.PresignDownload(key, time.Now().UTC().Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	downloadURL, err := url.Parse(download)
	if err != nil {
		t.Fatal(err)
	}
	if downloadURL.Query().Get("X-Goog-SignedHeaders") != "host" ||
		downloadURL.Query().Get("X-Goog-Signature") == "" {
		t.Fatalf("unexpected download signed URL: %s", download)
	}

	if err := store.VerifyUpload(t.Context(), key, 1234, hash); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.exists || state.headCalls < 2 || state.deleteCalls != 1 || state.signCalls != 2 {
		t.Fatalf("exists=%v head=%d delete=%d sign=%d", state.exists, state.headCalls, state.deleteCalls, state.signCalls)
	}
	if !strings.Contains(state.lastPayload, gcsV4Algorithm) ||
		!strings.Contains(state.lastPayload, "/auto/storage/goog4_request") {
		t.Fatalf("unexpected signBlob payload: %q", state.lastPayload)
	}
}

func TestGCSWorkloadIdentityMetadataTokenIsCached(t *testing.T) {
	hash := strings.Repeat("b", 64)
	metadataCalls := 0
	metadata := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadataCalls++
		if r.Header.Get("Metadata-Flavor") != "Google" {
			t.Fatal("Metadata-Flavor header missing")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "metadata-gcs-token",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer metadata.Close()

	storage, iam, _ := newGCSObjectTestServers(t, "metadata-gcs-token", 1234, hash)
	defer storage.Close()
	defer iam.Close()

	store, err := NewGCSObjectStore(GCSObjectStoreConfig{
		Bucket:                 "test-bucket",
		Endpoint:               storage.URL,
		ServiceAccountEmail:    "signer@test-project.iam.gserviceaccount.com",
		IAMCredentialsEndpoint: iam.URL,
		UseMetadata:            true,
		MetadataEndpoint:       metadata.URL,
		AllowInsecure:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	key := "workspaces/8/bbbbbbbbbbbb/124-report.pdf"
	if _, _, err := store.PresignUpload(key, "application/pdf", hash, 1234, time.Now().UTC().Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyUpload(t.Context(), key, 1234, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PresignDownload(key, time.Now().UTC().Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if metadataCalls != 1 {
		t.Fatalf("metadata calls=%d want=1", metadataCalls)
	}
}

func TestGCSObjectStoreRejectsUnsafeConfiguration(t *testing.T) {
	if _, err := NewGCSObjectStore(GCSObjectStoreConfig{
		Bucket:              "Bad_Bucket",
		ServiceAccountEmail: "signer@test-project.iam.gserviceaccount.com",
		AccessToken:         "token",
	}); err == nil {
		t.Fatal("expected invalid bucket rejection")
	}
	if _, err := NewGCSObjectStore(GCSObjectStoreConfig{
		Bucket:              "test-bucket",
		ServiceAccountEmail: "invalid-email",
		AccessToken:         "token",
	}); err == nil {
		t.Fatal("expected invalid service account email rejection")
	}
	if _, err := NewGCSObjectStore(GCSObjectStoreConfig{
		Bucket:              "test-bucket",
		ServiceAccountEmail: "signer@test-project.iam.gserviceaccount.com",
		Endpoint:            "http://storage.example.test",
		AccessToken:         "token",
	}); err == nil {
		t.Fatal("expected insecure GCS endpoint rejection")
	}
	if _, err := NewGCSObjectStore(GCSObjectStoreConfig{
		Bucket:              "test-bucket",
		ServiceAccountEmail: "signer@test-project.iam.gserviceaccount.com",
		AccessToken:         "token",
		Encryption:          "gcp_cmek",
		EncryptionKeyID:     "../unsafe",
	}); err == nil {
		t.Fatal("expected invalid CMEK rejection")
	}
}

func TestGCSObjectStoreVerifyRejectsMetadataMismatch(t *testing.T) {
	hash := strings.Repeat("c", 64)
	storage, iam, _ := newGCSObjectTestServers(t, "gcs-test-token", 1234, strings.Repeat("d", 64))
	defer storage.Close()
	defer iam.Close()

	store, err := NewGCSObjectStore(GCSObjectStoreConfig{
		Bucket:                 "test-bucket",
		Endpoint:               storage.URL,
		ServiceAccountEmail:    "signer@test-project.iam.gserviceaccount.com",
		IAMCredentialsEndpoint: iam.URL,
		AccessToken:            "gcs-test-token",
		AllowInsecure:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = store.VerifyUpload(t.Context(), "workspaces/1/abc/file.txt", 1234, hash)
	if err == nil {
		t.Fatal("expected digest metadata mismatch")
	}
}

func TestGCSObjectStoreObjectKeyValidation(t *testing.T) {
	store := &GCSObjectStore{}
	for _, key := range []string{"", "../escape", "a/../b", "a//b", "a\\b"} {
		if _, err := store.objectURL(key); err == nil {
			t.Fatalf("expected invalid object key %q", key)
		}
	}
}

func TestGCSObjectStoreSignBlobPayloadIsBase64(t *testing.T) {
	hash := strings.Repeat("e", 64)
	storage, iam, _ := newGCSObjectTestServers(t, "gcs-test-token", 1234, hash)
	defer storage.Close()
	defer iam.Close()

	store, err := NewGCSObjectStore(GCSObjectStoreConfig{
		Bucket:                 "test-bucket",
		Endpoint:               storage.URL,
		ServiceAccountEmail:    "signer@test-project.iam.gserviceaccount.com",
		IAMCredentialsEndpoint: iam.URL,
		AccessToken:            "gcs-test-token",
		AllowInsecure:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sig, err := store.signBlob(t.Context(), []byte("payload-to-sign"))
	if err != nil {
		t.Fatal(err)
	}
	if string(sig) != "test-rsa-signature" {
		t.Fatalf("signature=%q", sig)
	}
}

