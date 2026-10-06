package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveSecretProviderContract(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_CONTRACTS")), "true") {
		t.Skip("live provider contracts are opt-in")
	}

	target := strings.ToLower(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_CONTRACT_TARGET")))
	store := liveSecretContractStore(t, target)
	liveSecretStoreRoundTrip(t, store, target)
}

func liveSecretContractStore(t *testing.T, target string) ConnectorSecretStore {
	t.Helper()
	allowInsecure := strings.EqualFold(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_ALLOW_INSECURE")), "true")

	var (
		store ConnectorSecretStore
		err   error
	)
	switch target {
	case "secret_vault":
		store, err = NewHashiCorpVaultSecretStoreFromEnv(allowInsecure)
	case "secret_aws":
		store, err = NewAWSSecretsManagerSecretStoreFromEnv(allowInsecure)
	case "secret_azure":
		store, err = NewAzureKeyVaultSecretStoreFromEnv(allowInsecure)
	case "secret_gcp":
		store, err = NewGCPSecretManagerSecretStoreFromEnv(allowInsecure)
	default:
		t.Fatalf("unsupported live secret contract target %q", target)
	}
	if err != nil {
		t.Fatalf("configure %s live secret contract: %v", target, err)
	}
	if store == nil {
		t.Fatalf("%s live secret backend is not configured", target)
	}
	return store
}

func liveSecretStoreRoundTrip(t *testing.T, store ConnectorSecretStore, target string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runID := sanitizeAttachmentName(strings.TrimSpace(os.Getenv("GITHUB_RUN_ID")))
	if runID == "" || runID == "file" {
		runID = fmt.Sprint(time.Now().UTC().UnixNano())
	}
	ref := fmt.Sprintf("provider-contracts/%s-%d", runID, time.Now().UTC().UnixNano())
	first := []byte("simple-task-manager live secret contract version one")
	second := []byte("simple-task-manager live secret contract version two")

	deleted := false
	defer func() {
		if !deleted {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			_ = store.Delete(cleanupCtx, ref)
		}
	}()

	version1, err := store.Put(ctx, ref, first)
	if err != nil {
		t.Fatalf("%s initial put failed: %v", target, err)
	}
	if version1 <= 0 {
		t.Fatalf("%s initial version=%d want positive", target, version1)
	}
	got1, readVersion1, err := store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("%s initial get failed: %v", target, err)
	}
	if !bytes.Equal(got1, first) || readVersion1 != version1 {
		t.Fatalf("%s initial read/version mismatch: read_version=%d put_version=%d", target, readVersion1, version1)
	}

	version2, err := store.Put(ctx, ref, second)
	if err != nil {
		t.Fatalf("%s rotation put failed: %v", target, err)
	}
	if version2 <= version1 {
		t.Fatalf("%s rotation version=%d must be greater than initial=%d", target, version2, version1)
	}
	got2, readVersion2, err := store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("%s rotated get failed: %v", target, err)
	}
	if !bytes.Equal(got2, second) || readVersion2 != version2 {
		t.Fatalf("%s rotated read/version mismatch: read_version=%d put_version=%d", target, readVersion2, version2)
	}

	if err := store.Delete(ctx, ref); err != nil {
		t.Fatalf("%s delete failed: %v", target, err)
	}
	deleted = true

	deadline := time.Now().Add(30 * time.Second)
	for {
		_, _, err = store.Get(ctx, ref)
		if err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s secret remained readable after delete", target)
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Logf("live secret contract passed target=%s backend=%s versions=%d->%d deletion=inaccessible", target, store.Backend(), version1, version2)
}
