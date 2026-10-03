package events

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
)

func TestWebhookDeliverySignsEnvelope(t *testing.T) {
	secret := "whsec_test-secret-at-least-thirty-two-characters"
	var received map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		timestamp := r.Header.Get("X-Webhook-Timestamp")
		if got, want := r.Header.Get("X-Webhook-Signature"), "v1="+Sign(secret, timestamp, body); got != want {
			t.Fatalf("signature = %q, want %q", got, want)
		}
		if r.Header.Get("X-Webhook-Id") != "evt_test" || r.Header.Get("X-Webhook-Event") != model.EventTaskCreated {
			t.Fatalf("unexpected event headers: %+v", r.Header)
		}
		if err := json.Unmarshal(body, &received); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	worker := NewWorker(nil, WorkerOptions{
		AllowInsecure: true,
		HTTPTimeout:   time.Second,
	})
	event := model.DomainEvent{
		ID: 1, EventKey: "evt_test", WorkspaceID: 10,
		EventType: model.EventTaskCreated, AggregateType: "task",
		AggregateID: "42", SchemaVersion: 1,
		Data:       json.RawMessage(`{"id":42,"title":"signed"}`),
		OccurredAt: time.Now().UTC(),
	}
	subscription := model.WebhookSubscription{
		ID: 1, WorkspaceID: 10, URL: server.URL,
		SigningSecret: secret, EventTypes: []string{model.EventTaskCreated}, Active: true,
	}

	status, body, err := worker.deliver(context.Background(), event, subscription)
	if err != nil {
		t.Fatalf("deliver() error = %v", err)
	}
	if status != http.StatusNoContent || body != "" {
		t.Fatalf("deliver() status=%d body=%q", status, body)
	}
	if received["id"] != "evt_test" || received["type"] != model.EventTaskCreated {
		t.Fatalf("unexpected webhook envelope: %+v", received)
	}
}

func TestValidateWebhookURLSecurity(t *testing.T) {
	for _, raw := range []string{
		"http://example.com/hook",
		"https://127.0.0.1/hook",
		"https://localhost/hook",
		"https://169.254.169.254/latest/meta-data",
	} {
		endpoint, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateWebhookURL(endpoint, false); err == nil {
			t.Fatalf("ValidateWebhookURL(%q) error = nil, want rejection", raw)
		}
	}

	local, _ := url.Parse("http://127.0.0.1:8081/hook")
	if err := ValidateWebhookURL(local, true); err != nil {
		t.Fatalf("development insecure URL rejected: %v", err)
	}
	public, _ := url.Parse("https://hooks.example.com/task")
	if err := ValidateWebhookURL(public, false); err != nil && !strings.Contains(err.Error(), "private") {
		t.Fatalf("public HTTPS URL rejected: %v", err)
	}
}
