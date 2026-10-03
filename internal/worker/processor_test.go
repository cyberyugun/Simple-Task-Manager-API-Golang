package worker

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
)

type fakeEventRepository struct {
	events            []model.DomainEvent
	deliveries        []model.WebhookDelivery
	fanoutCount       int
	processedEventID  string
	deliveredID       int64
	deliveredStatus   int
	retriedDeliveryID int64
	retriedDead       bool
	retriedStatus     int
}

func (f *fakeEventRepository) CreateSubscription(model.WebhookSubscription, string) (model.WebhookSubscription, error) {
	return model.WebhookSubscription{}, nil
}
func (f *fakeEventRepository) ListSubscriptions(int64) ([]model.WebhookSubscription, error) {
	return nil, nil
}
func (f *fakeEventRepository) DeleteSubscription(int64, int64) error { return nil }
func (f *fakeEventRepository) ClaimOutbox(int, time.Time) ([]model.DomainEvent, error) {
	items := f.events
	f.events = nil
	return items, nil
}
func (f *fakeEventRepository) FanOutEvent(model.DomainEvent, time.Time) error {
	f.fanoutCount++
	return nil
}
func (f *fakeEventRepository) MarkOutboxProcessed(eventID string, _ time.Time) error {
	f.processedEventID = eventID
	return nil
}
func (f *fakeEventRepository) RetryOutbox(string, int, time.Time, string, bool) error { return nil }
func (f *fakeEventRepository) ClaimDeliveries(int, time.Time) ([]model.WebhookDelivery, error) {
	items := f.deliveries
	f.deliveries = nil
	return items, nil
}
func (f *fakeEventRepository) MarkDeliveryDelivered(id int64, status int, _ time.Time) error {
	f.deliveredID = id
	f.deliveredStatus = status
	return nil
}
func (f *fakeEventRepository) RetryDelivery(id int64, _ int, _ time.Time, status int, _ string, dead bool) error {
	f.retriedDeliveryID = id
	f.retriedStatus = status
	f.retriedDead = dead
	return nil
}
func (f *fakeEventRepository) ReplayDead(int64, time.Time) (int64, error) { return 0, nil }
func (f *fakeEventRepository) Stats(int64) (model.OutboxStats, error) {
	return model.OutboxStats{}, nil
}

func TestProcessorDeliversSignedWebhook(t *testing.T) {
	secret := "12345678901234567890123456789012"
	event := model.DomainEvent{
		EventID:       "evt_test",
		WorkspaceID:   7,
		AggregateType: "task",
		AggregateID:   "42",
		EventType:     "task.created",
		SchemaVersion: 1,
		Payload:       map[string]any{"id": float64(42)},
		CreatedAt:     time.Now().UTC(),
	}
	var receivedBody []byte
	var receivedTimestamp string
	var receivedSignature string
	var receivedIdempotency string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		receivedBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(receivedBody)
		receivedTimestamp = r.Header.Get("X-Webhook-Timestamp")
		receivedSignature = r.Header.Get("X-Webhook-Signature")
		receivedIdempotency = r.Header.Get("Idempotency-Key")
		if r.Header.Get("X-Webhook-Event-ID") != event.EventID {
			t.Fatalf("event id header = %q", r.Header.Get("X-Webhook-Event-ID"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	repo := &fakeEventRepository{
		events: []model.DomainEvent{event},
		deliveries: []model.WebhookDelivery{{
			ID:            9,
			EventID:       event.EventID,
			URL:           server.URL,
			SigningSecret: secret,
			Event:         event,
			Attempts:      1,
		}},
	}
	processor := New(repo, Config{BatchSize: 10, MaxAttempts: 3}, slog.Default())
	if err := processor.Once(context.Background()); err != nil {
		t.Fatalf("Once() error = %v", err)
	}

	if repo.fanoutCount != 1 || repo.processedEventID != event.EventID {
		t.Fatalf("event processing state = fanout:%d processed:%q", repo.fanoutCount, repo.processedEventID)
	}
	if repo.deliveredID != 9 || repo.deliveredStatus != http.StatusNoContent {
		t.Fatalf("delivery state = id:%d status:%d", repo.deliveredID, repo.deliveredStatus)
	}
	if receivedIdempotency != event.EventID {
		t.Fatalf("Idempotency-Key = %q", receivedIdempotency)
	}
	if receivedTimestamp == "" || receivedSignature == "" {
		t.Fatalf("missing signature headers")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(receivedTimestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(receivedBody)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(receivedSignature)) {
		t.Fatalf("signature = %q, want %q", receivedSignature, expected)
	}

	var decoded model.DomainEvent
	if err := json.Unmarshal(receivedBody, &decoded); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if decoded.EventID != event.EventID || decoded.SchemaVersion != 1 {
		t.Fatalf("unexpected webhook body: %+v", decoded)
	}
}

func TestProcessorMovesExhaustedDeliveryToDeadLetter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary failure", http.StatusInternalServerError)
	}))
	defer server.Close()

	event := model.DomainEvent{
		EventID:       "evt_dead",
		WorkspaceID:   3,
		AggregateType: "task",
		AggregateID:   "1",
		EventType:     "task.updated",
		SchemaVersion: 1,
		Payload:       map[string]any{"id": float64(1)},
		CreatedAt:     time.Now().UTC(),
	}
	repo := &fakeEventRepository{
		deliveries: []model.WebhookDelivery{{
			ID:            11,
			EventID:       event.EventID,
			URL:           server.URL,
			SigningSecret: "12345678901234567890123456789012",
			Event:         event,
			Attempts:      3,
		}},
	}
	processor := New(repo, Config{BatchSize: 10, MaxAttempts: 3, BaseBackoff: time.Millisecond}, slog.Default())
	if err := processor.Once(context.Background()); err != nil {
		t.Fatalf("Once() error = %v", err)
	}
	if repo.retriedDeliveryID != 11 || !repo.retriedDead || repo.retriedStatus != http.StatusInternalServerError {
		t.Fatalf("retry state = id:%d dead:%v status:%d", repo.retriedDeliveryID, repo.retriedDead, repo.retriedStatus)
	}
}
