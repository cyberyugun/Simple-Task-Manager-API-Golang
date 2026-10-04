package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type s3ObjectTestState struct {
	mu      sync.Mutex
	exists  bool
	size    int64
	sha256  string
	auth    []string
	methods []string
}

func newS3ObjectTestServer(t *testing.T, size int64, sha256 string) (*httptest.Server, *s3ObjectTestState) {
	t.Helper()
	state := &s3ObjectTestState{exists: true, size: size, sha256: sha256}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()
		state.auth = append(state.auth, r.Header.Get("Authorization"))
		state.methods = append(state.methods, r.Method)
		if !strings.HasPrefix(r.URL.Path, "/test-bucket/workspaces/") {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodHead:
			if !state.exists {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Length", strconv.FormatInt(state.size, 10))
			w.Header().Set("x-amz-meta-sha256", state.sha256)
			w.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			state.exists = false
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unsupported", http.StatusMethodNotAllowed)
		}
	}))
	return server, state
}

func TestS3ObjectStorePresignVerifyAndDelete(t *testing.T) {
	hash := strings.Repeat("a", 64)
	server, state := newS3ObjectTestServer(t, 1234, hash)
	defer server.Close()

	store, err := NewS3ObjectStore(S3ObjectStoreConfig{
		Region: "ap-southeast-1", Bucket: "test-bucket", Endpoint: server.URL,
		AccessKeyID: "AKIATEST", SecretAccessKey: "test-secret", SessionToken: "session-token",
		ForcePathStyle: true, AllowInsecure: true, Encryption: "aws:kms",
		EncryptionKeyID: "arn:aws:kms:ap-southeast-1:123456789012:key/test",
	})
	if err != nil {
		t.Fatal(err)
	}

	upload, headers, err := store.PresignUpload(
		"workspaces/7/aaaaaaaaaaaa/123-report.pdf",
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
	if query.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" ||
		query.Get("X-Amz-Signature") == "" ||
		query.Get("X-Amz-Security-Token") != "session-token" ||
		!strings.Contains(query.Get("X-Amz-Credential"), "/ap-southeast-1/s3/aws4_request") {
		t.Fatalf("unexpected presign query: %v", query)
	}
	if headers["x-amz-meta-sha256"] != hash ||
		headers["x-amz-server-side-encryption"] != "aws:kms" ||
		headers["x-amz-server-side-encryption-aws-kms-key-id"] == "" {
		t.Fatalf("unexpected upload headers: %v", headers)
	}

	download, err := store.PresignDownload(
		"workspaces/7/aaaaaaaaaaaa/123-report.pdf",
		time.Now().UTC().Add(5*time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(download, "X-Amz-Signature=") {
		t.Fatalf("download url missing signature: %s", download)
	}

	key := "workspaces/7/aaaaaaaaaaaa/123-report.pdf"
	if err := store.VerifyUpload(t.Context(), key, 1234, hash); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.exists {
		t.Fatal("object still exists after delete")
	}
	if len(state.auth) < 3 {
		t.Fatalf("expected signed HEAD/DELETE/HEAD calls, got %d", len(state.auth))
	}
	for _, auth := range state.auth {
		if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKIATEST/") {
			t.Fatalf("unexpected Authorization header: %q", auth)
		}
	}
}

func TestS3ObjectStoreVerifyRejectsMetadataMismatch(t *testing.T) {
	hash := strings.Repeat("b", 64)
	server, _ := newS3ObjectTestServer(t, 500, strings.Repeat("c", 64))
	defer server.Close()

	store, err := NewS3ObjectStore(S3ObjectStoreConfig{
		Region: "us-east-1", Bucket: "test-bucket", Endpoint: server.URL,
		AccessKeyID: "AKIATEST", SecretAccessKey: "test-secret",
		ForcePathStyle: true, AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = store.VerifyUpload(context.Background(), "workspaces/1/abc/file.txt", 500, hash)
	if err == nil {
		t.Fatal("expected digest metadata mismatch")
	}
}

func TestS3ObjectStoreRejectsUnsafeConfigAndKeys(t *testing.T) {
	if _, err := NewS3ObjectStore(S3ObjectStoreConfig{
		Region: "ap-southeast-1", Bucket: "Invalid_Bucket",
		AccessKeyID: "key", SecretAccessKey: "secret",
	}); err == nil {
		t.Fatal("expected invalid bucket rejection")
	}
	store, err := NewS3ObjectStore(S3ObjectStoreConfig{
		Region: "ap-southeast-1", Bucket: "valid-bucket",
		Endpoint: "http://127.0.0.1:9000", AccessKeyID: "key", SecretAccessKey: "secret",
		ForcePathStyle: true, AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.objectURL("workspaces/1/../escape"); err == nil {
		t.Fatal("expected traversal key rejection")
	}
}

func TestAttachmentObjectStoreFactoryRequiresImplementedNativeProvider(t *testing.T) {
	t.Setenv("ATTACHMENT_STORAGE_NATIVE", "true")
	_, err := NewAttachmentObjectStore(AttachmentConfig{
		Provider: "azure_blob", Bucket: "files",
	})
	if err == nil {
		t.Fatal("expected unsupported native provider to fail closed")
	}
}
