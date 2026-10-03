package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestIntegrationHubSignedDeliveryAndInboundDeduplication(t *testing.T) {
	orgs := repository.NewInMemoryOrganizationRepository()
	repo := repository.NewInMemoryIntegrationRepository()
	cipher, err := NewIntegrationCredentialCipher(strings.Repeat("cipher-test-", 4))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewIntegrationService(repo, orgs, cipher, true)
	now := time.Now().UTC()
	signingSecret := strings.Repeat("integration-test-", 3)
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Integration Org", Status: model.OrganizationStatusActive, OwnerUserID: 11,
		MaxMembers: 100, MaxWorkspaces: 20, CreatedByUserID: 11, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	receiver, err := svc.CreateConnection(11, org.ID, model.CreateIntegrationConnectionRequest{
		Provider: model.IntegrationProviderGenericWebhook, Name: "receiver", AuthType: model.IntegrationAuthWebhookSecret,
		Config: map[string]any{}, Credentials: map[string]any{"signing_secret": signingSecret},
	})
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		item, duplicate, err := svc.AcceptInbound(receiver.ID, r.Header.Get("X-Integration-Signature"), raw)
		if err != nil || duplicate || item.EventType != "task.federated" {
			t.Fatalf("inbound item=%+v duplicate=%v err=%v", item, duplicate, err)
		}
		w.Header().Set("X-RateLimit-Remaining", "99")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	sender, err := svc.CreateConnection(11, org.ID, model.CreateIntegrationConnectionRequest{
		Provider: model.IntegrationProviderGenericWebhook, Name: "sender", AuthType: model.IntegrationAuthWebhookSecret,
		Config: map[string]any{"target_url": server.URL}, Credentials: map[string]any{"signing_secret": signingSecret},
	})
	if err != nil {
		t.Fatal(err)
	}

	queued, err := svc.QueueDelivery(11, org.ID, model.CreateIntegrationDeliveryRequest{
		ConnectionID: sender.ID, EventType: "task.federated", Payload: map[string]any{"task_id": float64(10)},
	})
	if err != nil {
		t.Fatal(err)
	}
	processed, err := svc.ProcessBatch("test-worker", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(processed) != 1 || processed[0].Status != model.IntegrationDeliveryDelivered {
		t.Fatalf("processed=%+v", processed)
	}

	inbound, err := svc.InboundEvents(11, org.ID)
	if err != nil || len(inbound) != 1 || inbound[0].ProviderEventID != queued.EventKey {
		t.Fatalf("inbound=%+v err=%v", inbound, err)
	}

	body, _ := json.Marshal(model.IntegrationInboundEnvelope{EventID: "duplicate-1", EventType: "dup", Payload: map[string]any{"ok": true}})
	sig := integrationSignature(signingSecret, body)
	if _, dup, err := svc.AcceptInbound(receiver.ID, sig, body); err != nil || dup {
		t.Fatalf("first dup test: dup=%v err=%v", dup, err)
	}
	if _, dup, err := svc.AcceptInbound(receiver.ID, sig, body); err != nil || !dup {
		t.Fatalf("second dup test: dup=%v err=%v", dup, err)
	}
}
