package repository

import (
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrWebhookSubscriptionNotFound = errors.New("webhook subscription not found")
	ErrWebhookDeliveryNotFound     = errors.New("webhook delivery not found")
)

type EventRepository interface {
	CreateSubscription(workspaceID, actorID int64, url string, eventTypes []string, now time.Time) (model.WebhookSubscription, error)
	ListSubscriptions(workspaceID int64) ([]model.WebhookSubscription, error)
	DeleteSubscription(workspaceID, subscriptionID int64) error
	ListDeliveries(workspaceID, subscriptionID int64, limit int) ([]model.WebhookDelivery, error)
	ReplayDelivery(workspaceID, subscriptionID, deliveryID int64, now time.Time) error

	FanoutOutbox(limit int, now time.Time) (int, error)
	ClaimDeliveries(workerID string, limit int, lockTTL time.Duration, now time.Time) ([]model.WebhookDelivery, error)
	MarkDeliverySuccess(deliveryID int64, workerID string, status int, deliveredAt time.Time) error
	MarkDeliveryFailure(deliveryID int64, workerID string, status int, lastError string, nextAttemptAt time.Time, deadLetter bool, now time.Time) error
}

type InMemoryEventRepository struct {
	mu            sync.Mutex
	subscriptions map[int64]model.WebhookSubscription
	deliveries    map[int64]model.WebhookDelivery
	nextSubID     int64
	nextDelivery  int64
}

func NewInMemoryEventRepository() *InMemoryEventRepository {
	return &InMemoryEventRepository{
		subscriptions: make(map[int64]model.WebhookSubscription),
		deliveries:    make(map[int64]model.WebhookDelivery),
		nextSubID:     1,
		nextDelivery:  1,
	}
}

func (r *InMemoryEventRepository) CreateSubscription(workspaceID, actorID int64, url string, eventTypes []string, now time.Time) (model.WebhookSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sub := model.WebhookSubscription{
		ID:              r.nextSubID,
		WorkspaceID:     workspaceID,
		URL:             url,
		EventTypes:      append([]string(nil), eventTypes...),
		Active:          true,
		CreatedByUserID: actorID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	r.nextSubID++
	r.subscriptions[sub.ID] = sub
	return sub, nil
}

func (r *InMemoryEventRepository) ListSubscriptions(workspaceID int64) ([]model.WebhookSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.WebhookSubscription, 0)
	for _, sub := range r.subscriptions {
		if sub.WorkspaceID == workspaceID {
			items = append(items, sub)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryEventRepository) DeleteSubscription(workspaceID, subscriptionID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	sub, ok := r.subscriptions[subscriptionID]
	if !ok || sub.WorkspaceID != workspaceID {
		return ErrWebhookSubscriptionNotFound
	}
	delete(r.subscriptions, subscriptionID)
	return nil
}

func (r *InMemoryEventRepository) ListDeliveries(workspaceID, subscriptionID int64, limit int) ([]model.WebhookDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sub, ok := r.subscriptions[subscriptionID]
	if !ok || sub.WorkspaceID != workspaceID {
		return nil, ErrWebhookSubscriptionNotFound
	}
	items := make([]model.WebhookDelivery, 0)
	for _, delivery := range r.deliveries {
		if delivery.SubscriptionID == subscriptionID {
			items = append(items, delivery)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryEventRepository) ReplayDelivery(workspaceID, subscriptionID, deliveryID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	sub, ok := r.subscriptions[subscriptionID]
	if !ok || sub.WorkspaceID != workspaceID {
		return ErrWebhookSubscriptionNotFound
	}
	delivery, ok := r.deliveries[deliveryID]
	if !ok || delivery.SubscriptionID != subscriptionID {
		return ErrWebhookDeliveryNotFound
	}
	delivery.AttemptCount = 0
	delivery.NextAttemptAt = now
	delivery.DeliveredAt = nil
	delivery.DeadLetteredAt = nil
	delivery.LastStatus = nil
	delivery.LastError = ""
	delivery.UpdatedAt = now
	r.deliveries[deliveryID] = delivery
	return nil
}

func (r *InMemoryEventRepository) FanoutOutbox(int, time.Time) (int, error) {
	return 0, nil
}

func (r *InMemoryEventRepository) ClaimDeliveries(_ string, limit int, _ time.Duration, now time.Time) ([]model.WebhookDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.WebhookDelivery, 0)
	for id, delivery := range r.deliveries {
		if delivery.DeliveredAt != nil || delivery.DeadLetteredAt != nil || delivery.NextAttemptAt.After(now) {
			continue
		}
		delivery.AttemptCount++
		delivery.UpdatedAt = now
		r.deliveries[id] = delivery
		items = append(items, delivery)
		if len(items) >= limit {
			break
		}
	}
	return items, nil
}

func (r *InMemoryEventRepository) MarkDeliverySuccess(deliveryID int64, _ string, status int, deliveredAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delivery, ok := r.deliveries[deliveryID]
	if !ok {
		return ErrWebhookDeliveryNotFound
	}
	delivery.LastStatus = &status
	delivery.DeliveredAt = &deliveredAt
	delivery.LastError = ""
	delivery.UpdatedAt = deliveredAt
	r.deliveries[deliveryID] = delivery
	return nil
}

func (r *InMemoryEventRepository) MarkDeliveryFailure(deliveryID int64, _ string, status int, lastError string, nextAttemptAt time.Time, deadLetter bool, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delivery, ok := r.deliveries[deliveryID]
	if !ok {
		return ErrWebhookDeliveryNotFound
	}
	if status > 0 {
		delivery.LastStatus = &status
	}
	delivery.LastError = lastError
	delivery.NextAttemptAt = nextAttemptAt
	delivery.UpdatedAt = now
	if deadLetter {
		deadAt := now
		delivery.DeadLetteredAt = &deadAt
	}
	r.deliveries[deliveryID] = delivery
	return nil
}

func marshalEventTypes(values []string) ([]byte, error) {
	if values == nil {
		values = []string{}
	}
	return json.Marshal(values)
}
