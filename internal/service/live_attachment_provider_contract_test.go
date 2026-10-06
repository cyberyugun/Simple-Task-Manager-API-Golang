package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
)

func TestLiveAttachmentProviderContract(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_CONTRACTS")), "true") {
		t.Skip("live provider contracts are opt-in")
	}

	target := strings.ToLower(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_CONTRACT_TARGET")))
	switch target {
	case "storage_s3", "storage_azure", "storage_gcs":
		store, provider, cfg := liveAttachmentContractStore(t, target)
		liveAttachmentStorageRoundTrip(t, store, provider, cfg)
	case "scanner":
		storageTarget := strings.ToLower(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_STORAGE_TARGET")))
		if storageTarget == "" {
			t.Fatal("LIVE_PROVIDER_STORAGE_TARGET is required for scanner contract")
		}
		store, provider, cfg := liveAttachmentContractStore(t, storageTarget)
		liveAttachmentScannerRoundTrip(t, store, provider, cfg)
	default:
		t.Fatalf("unsupported LIVE_PROVIDER_CONTRACT_TARGET %q", target)
	}
}

func liveAttachmentContractStore(t *testing.T, target string) (ObjectStore, string, AttachmentConfig) {
	t.Helper()
	bucket := strings.TrimSpace(os.Getenv("ATTACHMENT_STORAGE_BUCKET"))
	if bucket == "" {
		t.Fatal("ATTACHMENT_STORAGE_BUCKET is required for live storage contracts")
	}
	cfg := AttachmentConfig{
		Bucket:          bucket,
		Encryption:      strings.TrimSpace(os.Getenv("ATTACHMENT_ENCRYPTION")),
		EncryptionKeyID: strings.TrimSpace(os.Getenv("ATTACHMENT_ENCRYPTION_KEY_ID")),
		AllowInsecure:   strings.EqualFold(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_ALLOW_INSECURE")), "true"),
	}

	var (
		store    ObjectStore
		provider string
		err      error
	)
	switch target {
	case "storage_s3":
		provider = strings.TrimSpace(os.Getenv("ATTACHMENT_STORAGE_PROVIDER"))
		if provider != model.AttachmentProviderS3 && provider != model.AttachmentProviderS3Compatible {
			provider = model.AttachmentProviderS3
		}
		cfg.Provider = provider
		store, err = NewS3ObjectStoreFromEnv(cfg)
	case "storage_azure":
		provider = model.AttachmentProviderAzureBlob
		cfg.Provider = provider
		store, err = NewAzureBlobObjectStoreFromEnv(cfg)
	case "storage_gcs":
		provider = model.AttachmentProviderGCS
		cfg.Provider = provider
		store, err = NewGCSObjectStoreFromEnv(cfg)
	default:
		t.Fatalf("unsupported live storage target %q", target)
	}
	if err != nil {
		t.Fatalf("configure %s live storage contract: %v", target, err)
	}
	return store, provider, cfg
}

func liveAttachmentStorageRoundTrip(t *testing.T, store ObjectStore, provider string, cfg AttachmentConfig) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	payload := []byte("simple-task-manager live storage provider contract\n")
	objectKey, digest := liveAttachmentUpload(t, ctx, store, payload)
	deleted := false
	defer func() {
		if !deleted {
			_ = store.Delete(context.Background(), objectKey)
		}
	}()

	if err := store.VerifyUpload(ctx, objectKey, int64(len(payload)), digest); err != nil {
		t.Fatalf("%s verify upload: %v", provider, err)
	}
	liveAttachmentDownloadAndVerify(t, ctx, store, objectKey, payload)
	if err := store.Delete(ctx, objectKey); err != nil {
		t.Fatalf("%s delete: %v", provider, err)
	}
	deleted = true

	t.Logf("live storage contract passed provider=%s bucket=%s object_key=%s", provider, cfg.Bucket, objectKey)
}

func liveAttachmentScannerRoundTrip(t *testing.T, store ObjectStore, provider string, cfg AttachmentConfig) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	payload := []byte("simple-task-manager live clean scanner contract\n")
	objectKey, digest := liveAttachmentUpload(t, ctx, store, payload)
	defer func() {
		_ = store.Delete(context.Background(), objectKey)
	}()
	if err := store.VerifyUpload(ctx, objectKey, int64(len(payload)), digest); err != nil {
		t.Fatalf("%s verify scanner fixture upload: %v", provider, err)
	}

	scannerCfg := cfg
	scannerCfg.ScannerURL = strings.TrimSpace(os.Getenv("ATTACHMENT_SCANNER_URL"))
	scannerCfg.ScannerBearerToken = strings.TrimSpace(os.Getenv("ATTACHMENT_SCANNER_BEARER_TOKEN"))
	scannerCfg.ScannerSigningSecret = strings.TrimSpace(os.Getenv("ATTACHMENT_SCANNER_SIGNING_SECRET"))
	scannerCfg.ScannerRequired = true
	scanner, err := NewAttachmentScanner(scannerCfg)
	if err != nil {
		t.Fatalf("configure live attachment scanner: %v", err)
	}

	now := time.Now().UTC()
	result, err := scanner.Scan(ctx, model.Attachment{
		ID: 1, WorkspaceID: 1, UploadedByUserID: 1,
		Provider: provider, Bucket: cfg.Bucket, ObjectKey: objectKey,
		FileName: "provider-contract.txt", ContentType: "text/plain",
		SizeBytes: int64(len(payload)), SHA256: digest, Status: model.AttachmentStatusUploaded,
		Encryption: cfg.Encryption, EncryptionKeyID: cfg.EncryptionKeyID,
		Version: 1, CreatedAt: now, UpdatedAt: now, UploadedAt: &now,
	})
	if err != nil {
		t.Fatalf("live scanner request failed: %v", err)
	}
	if !result.Clean || strings.TrimSpace(result.Engine) == "" {
		t.Fatalf("live scanner returned unexpected result: %+v", result)
	}
	liveAttachmentDownloadAndVerify(t, ctx, store, objectKey, payload)
	t.Logf("live scanner contract passed engine=%s provider=%s bucket=%s", result.Engine, provider, cfg.Bucket)
}

func liveAttachmentUpload(t *testing.T, ctx context.Context, store ObjectStore, payload []byte) (string, string) {
	t.Helper()
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	runID := strings.TrimSpace(os.Getenv("GITHUB_RUN_ID"))
	if runID == "" {
		runID = fmt.Sprint(time.Now().UTC().UnixNano())
	}
	objectKey := fmt.Sprintf("provider-contracts/%s-%d.txt", sanitizeAttachmentName(runID), time.Now().UTC().UnixNano())
	expiresAt := time.Now().UTC().Add(10 * time.Minute)
	uploadURL, headers, err := store.PresignUpload(objectKey, "text/plain", digest, int64(len(payload)), expiresAt)
	if err != nil {
		t.Fatalf("presign live upload: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	client := &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("live upload request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("live upload HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return objectKey, digest
}

func liveAttachmentDownloadAndVerify(t *testing.T, ctx context.Context, store ObjectStore, objectKey string, expected []byte) {
	t.Helper()
	downloadURL, err := store.PresignDownload(objectKey, time.Now().UTC().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("presign live download: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("live download request failed: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(len(expected))+1024))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("live download HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if !bytes.Equal(body, expected) {
		t.Fatalf("live download payload mismatch: got=%d bytes want=%d", len(body), len(expected))
	}
}
