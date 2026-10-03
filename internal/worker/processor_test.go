package worker

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/webhook"
)

type fakeEventRepository struct {
	deliveries []model.WebhookDelivery
	successID  int64
	deadID     int64
	retriedID  int64
}

func (f *fakeEventRepository) CreateSubscription(int64, int64, string, []string, time.Time) (model.WebhookSubscription, error) {
	return model.WebhookSubscription{}, nil
}
func (f *fakeEventRepository) ListSubscriptions(int64) ([]model.WebhookSubscription, error) {
	return nil, nil
}
func (f *fakeEventRepository) DeleteSubscription(int64, int64) error { return nil }
func (f *fakeEventRepository) ListDeliveries(int64, int64, int) ([]model.WebhookDelivery, error) {
	return nil, nil
}
func (f *fakeEventRepository) ReplayDelivery(int64, int64, int64, time.Time) error { return nil }
func (f *fakeEventRepository) FanoutOutbox(int, time.Time) (int, error) { return 1, nil }
func (f *fakeEventRepository) ClaimDeliveries(string, int, time.Duration, time.Time) ([]model.WebhookDelivery, error) {
	items := f.deliveries
	f.deliveries = nil
	return items, nil
}
func (f *fakeEventRepository) MarkDeliverySuccess(id int64, _ string, _ int, _ time.Time) error {
	f.successID = id
	return nil
}
func (f *fakeEventRepository) MarkDeliveryFailure(id int64, _ string, _ int, _ string, _ time.Time, dead bool, _ time.Time) error {
	if dead {
		f.deadID = id
	} else {
		f.retriedID = id
	}
	return nil
}

func TestProcessorDeliversSignedWebhook(t *testing.T) {
	master := "12345678901234567890123456789012"
	var signatureOK bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		ts, err := strconv.ParseInt(r.Header.Get("X-Webhook-Timestamp"), 10, 64)
		if err != nil {
			t.Fatalf("invalid timestamp header: %v", err)
		}
		secret := webhook.DeriveSigningSecret(master, 5)
		signatureOK = webhook.Verify(secret, ts, body, r.Header.Get("X-Webhook-Signature"))
		if r.Header.Get("X-Webhook-Event-ID") != "event-123" || r.Header.Get("Idempotency-Key") != "event-123" {
			t.Fatalf("unexpected webhook headers: %+v", r.Header)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	repo := &fakeEventRepository{deliveries: []model.WebhookDelivery{{
		ID:             9,
		SubscriptionID: 5,
		EventID:        "event-123",
		EventType:      model.EventTaskCreated,
		AttemptCount:   1,
		MaxAttempts:    8,
		URL:            server.URL,
		WorkspaceID:    10,
		AggregateType:  "task",
		AggregateID:    "77",
		SchemaVersion:  1,
		Payload:        map[string]any{"id": float64(77)},
		OccurredAt:     time.Now().UTC(),
	}}}

	processor := NewProcessor(repo, ProcessorOptions{
		HTTPClient:           server.Client(),
		SigningKey:           master,
		WorkerID:             "test-worker",
		AllowPrivateNetworks: true,
		Logger:               slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	stats, err := processor.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !signatureOK || repo.successID != 9 || stats.Delivered != 1 || stats.FannedOut != 1 {
		t.Fatalf("unexpected result: signature=%v success=%d stats=%+v", signatureOK, repo.successID, stats)
	}
}

func TestProcessorDeadLettersAfterMaxAttempts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer server.Close()

	repo := &fakeEventRepository{deliveries: []model.WebhookDelivery{{
		ID:             11,
		SubscriptionID: 5,
		EventID:        "event-dead",
		EventType:      model.EventTaskDeleted,
		AttemptCount:   8,
		MaxAttempts:    8,
		URL:            server.URL,
		WorkspaceID:    10,
		AggregateType:  "task",
		AggregateID:    "77",
		SchemaVersion:  1,
		Payload:        map[string]any{"id": float64(77)},
		OccurredAt:     time.Now().UTC(),
	}}}

	processor := NewProcessor(repo, ProcessorOptions{
		HTTPClient:           server.Client(),
		SigningKey:           "12345678901234567890123456789012",
		WorkerID:             "test-worker",
		AllowPrivateNetworks: true,
		Logger:               slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	stats, err := processor.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if repo.deadID != 11 || stats.DeadLetter != 1 {
		t.Fatalf("delivery was not dead-lettered: dead=%d stats=%+v", repo.deadID, stats)
	}
}

var _ repository.EventRepository = (*fakeEventRepository)(nil)
