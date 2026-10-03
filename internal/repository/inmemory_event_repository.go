package repository

import (
	"sort"
	"strconv"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

type InMemoryEventRepository struct {
	mu            sync.Mutex
	webhooks      map[int64]model.WebhookSubscription
	idempotency   map[string]model.IdempotencyRecord
	nextWebhookID int64
}

func NewInMemoryEventRepository() *InMemoryEventRepository {
	return &InMemoryEventRepository{
		webhooks:      make(map[int64]model.WebhookSubscription),
		idempotency:   make(map[string]model.IdempotencyRecord),
		nextWebhookID: 1,
	}
}

func (r *InMemoryEventRepository) ClaimReady(string, int, time.Time) ([]model.DomainEvent, error) {
	return []model.DomainEvent{}, nil
}

func (r *InMemoryEventRepository) PendingSubscriptions(model.DomainEvent) ([]model.WebhookSubscription, error) {
	return []model.WebhookSubscription{}, nil
}

func (r *InMemoryEventRepository) RecordDelivery(model.WebhookDelivery) error { return nil }
func (r *InMemoryEventRepository) MarkProcessed(int64, time.Time) error       { return nil }
func (r *InMemoryEventRepository) MarkFailed(int64, string, time.Time) (bool, error) {
	return false, nil
}
func (r *InMemoryEventRepository) ReleaseStaleLocks(time.Time) (int64, error) { return 0, nil }
func (r *InMemoryEventRepository) Replay(string, time.Time) error             { return ErrEventNotFound }

func (r *InMemoryEventRepository) CreateWebhook(workspaceID, actorUserID int64, url, secret string, eventTypes []string, now time.Time) (model.WebhookSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	actor := actorUserID
	item := model.WebhookSubscription{
		ID: r.nextWebhookID, WorkspaceID: workspaceID, CreatedByUserID: &actor,
		URL: url, EventTypes: append([]string(nil), eventTypes...), Active: true,
		SigningSecret: secret, CreatedAt: now, UpdatedAt: now,
	}
	r.nextWebhookID++
	r.webhooks[item.ID] = item
	return item, nil
}

func (r *InMemoryEventRepository) ListWebhooks(workspaceID int64) ([]model.WebhookSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.WebhookSubscription, 0)
	for _, item := range r.webhooks {
		if item.WorkspaceID != workspaceID {
			continue
		}
		item.SigningSecret = ""
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryEventRepository) DeleteWebhook(workspaceID, subscriptionID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.webhooks[subscriptionID]
	if !ok || item.WorkspaceID != workspaceID {
		return ErrWebhookSubscriptionNotFound
	}
	delete(r.webhooks, subscriptionID)
	return nil
}

func idempotencyMapKey(workspaceID int64, key string) string {
	return strconv.FormatInt(workspaceID, 10) + ":" + key
}

func (r *InMemoryEventRepository) BeginIdempotency(workspaceID int64, key, method, path, requestHash string, now, expiresAt time.Time) (model.IdempotencyRecord, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mapKey := idempotencyMapKey(workspaceID, key)
	if existing, ok := r.idempotency[mapKey]; ok {
		if !existing.ExpiresAt.After(now) {
			delete(r.idempotency, mapKey)
		} else {
			if existing.Method != method || existing.Path != path || existing.RequestHash != requestHash {
				return model.IdempotencyRecord{}, false, ErrIdempotencyConflict
			}
			if existing.State == "pending" {
				return model.IdempotencyRecord{}, false, ErrIdempotencyInProgress
			}
			return existing, false, nil
		}
	}
	record := model.IdempotencyRecord{
		WorkspaceID: workspaceID, Key: key, Method: method, Path: path,
		RequestHash: requestHash, State: "pending", ExpiresAt: expiresAt,
	}
	r.idempotency[mapKey] = record
	return record, true, nil
}

func (r *InMemoryEventRepository) CompleteIdempotency(workspaceID int64, key string, status int, contentType string, body []byte, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	mapKey := idempotencyMapKey(workspaceID, key)
	record, ok := r.idempotency[mapKey]
	if !ok || record.State != "pending" {
		return ErrIdempotencyInProgress
	}
	record.State = "completed"
	record.ResponseStatus = status
	record.ResponseContentType = contentType
	record.ResponseBody = append([]byte(nil), body...)
	r.idempotency[mapKey] = record
	return nil
}

func (r *InMemoryEventRepository) AbortIdempotency(workspaceID int64, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	mapKey := idempotencyMapKey(workspaceID, key)
	record, ok := r.idempotency[mapKey]
	if ok && record.State == "pending" {
		delete(r.idempotency, mapKey)
	}
	return nil
}

var _ EventRepository = (*InMemoryEventRepository)(nil)
