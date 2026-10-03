package service

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
)

type fakeEventRepo struct {
	created model.WebhookSubscription
	secret  string
}

func (f *fakeEventRepo) CreateSubscription(s model.WebhookSubscription, secret string) (model.WebhookSubscription, error) {
	s.ID = 1
	f.created = s
	f.secret = secret
	return s, nil
}
func (f *fakeEventRepo) ListSubscriptions(int64) ([]model.WebhookSubscription, error) { return nil, nil }
func (f *fakeEventRepo) DeleteSubscription(int64, int64) error { return nil }
func (f *fakeEventRepo) ClaimOutbox(int, time.Time) ([]model.DomainEvent, error) { return nil, nil }
func (f *fakeEventRepo) FanOutEvent(model.DomainEvent, time.Time) error { return nil }
func (f *fakeEventRepo) MarkOutboxProcessed(string, time.Time) error { return nil }
func (f *fakeEventRepo) RetryOutbox(string, int, time.Time, string, bool) error { return nil }
func (f *fakeEventRepo) ClaimDeliveries(int, time.Time) ([]model.WebhookDelivery, error) { return nil, nil }
func (f *fakeEventRepo) MarkDeliveryDelivered(int64, int, time.Time) error { return nil }
func (f *fakeEventRepo) RetryDelivery(int64, int, time.Time, int, string, bool) error { return nil }
func (f *fakeEventRepo) ReplayDead(int64, time.Time) (int64, error) { return 0, nil }
func (f *fakeEventRepo) Stats(int64) (model.OutboxStats, error) { return model.OutboxStats{}, nil }

func TestEventServiceWebhookPolicy(t *testing.T) {
	repo := &fakeEventRepo{}
	svc := NewEventService(repo)
	owner := model.WorkspaceAccess{Workspace: model.Workspace{ID: 42}, Role: model.WorkspaceRoleOwner}

	sub, err := svc.CreateSubscription(owner, 7, model.CreateWebhookSubscriptionRequest{
		URL:        "https://hooks.example.com/task",
		Secret:     "12345678901234567890123456789012",
		EventTypes: []string{"task.created", "task.created", "task.completed"},
	})
	if err != nil {
		t.Fatalf("CreateSubscription() error = %v", err)
	}
	if sub.ID != 1 || sub.WorkspaceID != 42 || len(sub.EventTypes) != 2 {
		t.Fatalf("unexpected subscription: %+v", sub)
	}
	if repo.secret == "" {
		t.Fatal("secret was not persisted")
	}

	_, err = svc.CreateSubscription(owner, 7, model.CreateWebhookSubscriptionRequest{
		URL:    "http://hooks.example.com/task",
		Secret: "12345678901234567890123456789012",
	})
	if !errors.Is(err, ErrInvalidWebhookURL) {
		t.Fatalf("http URL error = %v, want ErrInvalidWebhookURL", err)
	}

	_, err = svc.CreateSubscription(owner, 7, model.CreateWebhookSubscriptionRequest{
		URL:    "https://hooks.example.com/task",
		Secret: "too-short",
	})
	if !errors.Is(err, ErrInvalidWebhookSecret) {
		t.Fatalf("short secret error = %v, want ErrInvalidWebhookSecret", err)
	}

	member := model.WorkspaceAccess{Workspace: model.Workspace{ID: 42}, Role: model.WorkspaceRoleMember}
	if _, err := svc.ListSubscriptions(member); !errors.Is(err, ErrWebhookForbidden) {
		t.Fatalf("member ListSubscriptions() error = %v, want ErrWebhookForbidden", err)
	}
}
