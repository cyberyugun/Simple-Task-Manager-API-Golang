package repository

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrEventSchemaNotFound              = errors.New("event schema not found")
	ErrEventFabricSubscriptionNotFound  = errors.New("event fabric subscription not found")
	ErrEventRouteNotFound               = errors.New("event route not found")
	ErrEventFabricDeliveryNotFound      = errors.New("event fabric delivery not found")
)

type EventFabricRepository interface {
	CreateSchema(schema model.EventSchemaVersion) (model.EventSchemaVersion, error)
	ListSchemas(workspaceID int64, eventType string) ([]model.EventSchemaVersion, error)
	LatestSchema(workspaceID int64, eventType string) (model.EventSchemaVersion, error)
	DeprecateSchema(workspaceID, schemaID int64, now time.Time) error

	CreateSubscription(subscription model.EventFabricSubscription) (model.EventFabricSubscription, error)
	ListSubscriptions(workspaceID int64) ([]model.EventFabricSubscription, error)
	GetSubscription(workspaceID, subscriptionID int64) (model.EventFabricSubscription, error)
	UpdateSubscriptionStatus(workspaceID, subscriptionID int64, status string, now time.Time) (model.EventFabricSubscription, error)

	CreateRoute(route model.EventRoute) (model.EventRoute, error)
	ListRoutes(workspaceID int64) ([]model.EventRoute, error)
	MatchingRoutes(workspaceID int64, eventType string) ([]model.EventRoute, error)

	CreateDelivery(delivery model.EventFabricDelivery) (model.EventFabricDelivery, bool, error)
	ClaimDeliveries(workerID string, limit int, now time.Time) ([]model.EventFabricDelivery, error)
	MarkDeliverySucceeded(deliveryID int64, now time.Time) error
	MarkDeliveryFailed(deliveryID int64, failure string, now time.Time) (bool, error)
	ReleaseDeliveryLocks(before time.Time) (int64, error)
	GetDelivery(workspaceID, deliveryID int64) (model.EventFabricDelivery, error)
	ListDeliveries(workspaceID, subscriptionID int64, status string, limit int) ([]model.EventFabricDelivery, error)
	ReplayDelivery(workspaceID, deliveryID int64, now time.Time) error
	ReplayRange(workspaceID, subscriptionID, fromEventID, toEventID int64, now time.Time) (int64, error)
	PurgeDelivered(before time.Time, limit int) (int64, error)

	UpsertOffset(offset model.EventConsumerOffset) error
	GetOffset(workspaceID, subscriptionID int64) (model.EventConsumerOffset, error)
}

type InMemoryEventFabricRepository struct {
	mu            sync.Mutex
	schemas       map[int64]model.EventSchemaVersion
	subscriptions map[int64]model.EventFabricSubscription
	routes        map[int64]model.EventRoute
	deliveries    map[int64]model.EventFabricDelivery
	deliveryKeys  map[string]int64
	offsets       map[int64]model.EventConsumerOffset
	nextSchema    int64
	nextSub       int64
	nextRoute     int64
	nextDelivery  int64
}

func NewInMemoryEventFabricRepository() *InMemoryEventFabricRepository {
	return &InMemoryEventFabricRepository{
		schemas: make(map[int64]model.EventSchemaVersion),
		subscriptions: make(map[int64]model.EventFabricSubscription),
		routes: make(map[int64]model.EventRoute),
		deliveries: make(map[int64]model.EventFabricDelivery),
		deliveryKeys: make(map[string]int64),
		offsets: make(map[int64]model.EventConsumerOffset),
		nextSchema: 1, nextSub: 1, nextRoute: 1, nextDelivery: 1,
	}
}

func (r *InMemoryEventFabricRepository) CreateSchema(item model.EventSchemaVersion) (model.EventSchemaVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextSchema
	r.nextSchema++
	item.Schema = cloneEventFabricMap(item.Schema)
	r.schemas[item.ID] = item
	return cloneEventSchema(item), nil
}

func (r *InMemoryEventFabricRepository) ListSchemas(workspaceID int64, eventType string) ([]model.EventSchemaVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.EventSchemaVersion, 0)
	for _, item := range r.schemas {
		if item.WorkspaceID != workspaceID || (eventType != "" && item.EventType != eventType) {
			continue
		}
		items = append(items, cloneEventSchema(item))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].EventType == items[j].EventType {
			return items[i].Version > items[j].Version
		}
		return items[i].EventType < items[j].EventType
	})
	return items, nil
}

func (r *InMemoryEventFabricRepository) LatestSchema(workspaceID int64, eventType string) (model.EventSchemaVersion, error) {
	items, _ := r.ListSchemas(workspaceID, eventType)
	for _, item := range items {
		if item.Status == model.EventSchemaStatusActive {
			return item, nil
		}
	}
	return model.EventSchemaVersion{}, ErrEventSchemaNotFound
}

func (r *InMemoryEventFabricRepository) DeprecateSchema(workspaceID, schemaID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.schemas[schemaID]
	if !ok || item.WorkspaceID != workspaceID {
		return ErrEventSchemaNotFound
	}
	item.Status = model.EventSchemaStatusDeprecated
	item.DeprecatedAt = &now
	r.schemas[schemaID] = item
	return nil
}

func (r *InMemoryEventFabricRepository) CreateSubscription(item model.EventFabricSubscription) (model.EventFabricSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextSub
	r.nextSub++
	item.EventTypes = append([]string(nil), item.EventTypes...)
	r.subscriptions[item.ID] = item
	return cloneEventFabricSubscription(item), nil
}

func (r *InMemoryEventFabricRepository) ListSubscriptions(workspaceID int64) ([]model.EventFabricSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.EventFabricSubscription, 0)
	for _, item := range r.subscriptions {
		if item.WorkspaceID == workspaceID {
			items = append(items, cloneEventFabricSubscription(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryEventFabricRepository) GetSubscription(workspaceID, subscriptionID int64) (model.EventFabricSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.subscriptions[subscriptionID]
	if !ok || item.WorkspaceID != workspaceID {
		return model.EventFabricSubscription{}, ErrEventFabricSubscriptionNotFound
	}
	return cloneEventFabricSubscription(item), nil
}

func (r *InMemoryEventFabricRepository) UpdateSubscriptionStatus(workspaceID, subscriptionID int64, status string, now time.Time) (model.EventFabricSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.subscriptions[subscriptionID]
	if !ok || item.WorkspaceID != workspaceID {
		return model.EventFabricSubscription{}, ErrEventFabricSubscriptionNotFound
	}
	item.Status = status
	item.UpdatedAt = now
	r.subscriptions[subscriptionID] = item
	return cloneEventFabricSubscription(item), nil
}

func (r *InMemoryEventFabricRepository) CreateRoute(item model.EventRoute) (model.EventRoute, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sub, ok := r.subscriptions[item.SubscriptionID]
	if !ok || sub.WorkspaceID != item.WorkspaceID {
		return model.EventRoute{}, ErrEventFabricSubscriptionNotFound
	}
	item.ID = r.nextRoute
	r.nextRoute++
	r.routes[item.ID] = item
	return item, nil
}

func (r *InMemoryEventFabricRepository) ListRoutes(workspaceID int64) ([]model.EventRoute, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.EventRoute, 0)
	for _, item := range r.routes {
		if item.WorkspaceID == workspaceID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryEventFabricRepository) MatchingRoutes(workspaceID int64, eventType string) ([]model.EventRoute, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.EventRoute, 0)
	for _, item := range r.routes {
		if item.WorkspaceID == workspaceID && item.Active && eventPatternMatches(item.EventPattern, eventType) {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryEventFabricRepository) CreateDelivery(item model.EventFabricDelivery) (model.EventFabricDelivery, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := eventFabricDeliveryKey(item.SubscriptionID, item.OutboxEventID)
	if id, ok := r.deliveryKeys[key]; ok {
		return cloneEventFabricDelivery(r.deliveries[id]), false, nil
	}
	item.ID = r.nextDelivery
	r.nextDelivery++
	item.Payload = cloneEventFabricMap(item.Payload)
	r.deliveries[item.ID] = item
	r.deliveryKeys[key] = item.ID
	return cloneEventFabricDelivery(item), true, nil
}

func (r *InMemoryEventFabricRepository) ClaimDeliveries(workerID string, limit int, now time.Time) ([]model.EventFabricDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]int64, 0)
	for id, item := range r.deliveries {
		if (item.Status == model.EventFabricDeliveryPending || item.Status == model.EventFabricDeliveryRetry) &&
			item.DeadLetteredAt == nil && item.LockedAt == nil && !item.AvailableAt.After(now) {
			sub, ok := r.subscriptions[item.SubscriptionID]
			if ok && sub.Status == model.EventFabricSubscriptionActive {
				ids = append(ids, id)
			}
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if limit > 0 && len(ids) > limit {
		ids = ids[:limit]
	}
	items := make([]model.EventFabricDelivery, 0, len(ids))
	for _, id := range ids {
		item := r.deliveries[id]
		t := now
		item.LockedAt = &t
		item.LockedBy = workerID
		r.deliveries[id] = item
		items = append(items, cloneEventFabricDelivery(item))
	}
	return items, nil
}

func (r *InMemoryEventFabricRepository) MarkDeliverySucceeded(deliveryID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.deliveries[deliveryID]
	if !ok {
		return ErrEventFabricDeliveryNotFound
	}
	item.Status = model.EventFabricDeliveryDelivered
	item.LockedAt = nil
	item.LockedBy = ""
	item.LastError = ""
	item.DeliveredAt = &now
	item.UpdatedAt = now
	r.deliveries[deliveryID] = item
	return nil
}

func (r *InMemoryEventFabricRepository) MarkDeliveryFailed(deliveryID int64, failure string, now time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.deliveries[deliveryID]
	if !ok {
		return false, ErrEventFabricDeliveryNotFound
	}
	item.Attempts++
	item.LastError = failure
	item.LockedAt = nil
	item.LockedBy = ""
	item.UpdatedAt = now
	dead := item.Attempts >= item.MaxAttempts
	if dead {
		item.Status = model.EventFabricDeliveryDeadLetter
		item.DeadLetteredAt = &now
	} else {
		item.Status = model.EventFabricDeliveryRetry
		delay := time.Duration(1<<minEventFabricInt(item.Attempts, 8)) * time.Second
		item.AvailableAt = now.Add(delay)
	}
	r.deliveries[deliveryID] = item
	return dead, nil
}

func (r *InMemoryEventFabricRepository) ReleaseDeliveryLocks(before time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for id, item := range r.deliveries {
		if item.LockedAt != nil && item.LockedAt.Before(before) &&
			item.Status != model.EventFabricDeliveryDelivered && item.Status != model.EventFabricDeliveryDeadLetter {
			item.LockedAt = nil
			item.LockedBy = ""
			r.deliveries[id] = item
			count++
		}
	}
	return count, nil
}

func (r *InMemoryEventFabricRepository) GetDelivery(workspaceID, deliveryID int64) (model.EventFabricDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.deliveries[deliveryID]
	if !ok || item.WorkspaceID != workspaceID {
		return model.EventFabricDelivery{}, ErrEventFabricDeliveryNotFound
	}
	return cloneEventFabricDelivery(item), nil
}

func (r *InMemoryEventFabricRepository) ListDeliveries(workspaceID, subscriptionID int64, status string, limit int) ([]model.EventFabricDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.EventFabricDelivery, 0)
	for _, item := range r.deliveries {
		if item.WorkspaceID != workspaceID || (subscriptionID > 0 && item.SubscriptionID != subscriptionID) ||
			(status != "" && item.Status != status) {
			continue
		}
		items = append(items, cloneEventFabricDelivery(item))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryEventFabricRepository) ReplayDelivery(workspaceID, deliveryID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.deliveries[deliveryID]
	if !ok || item.WorkspaceID != workspaceID {
		return ErrEventFabricDeliveryNotFound
	}
	item.Status = model.EventFabricDeliveryPending
	item.Attempts = 0
	item.AvailableAt = now
	item.LockedAt = nil
	item.LockedBy = ""
	item.LastError = ""
	item.DeliveredAt = nil
	item.DeadLetteredAt = nil
	item.UpdatedAt = now
	r.deliveries[deliveryID] = item
	return nil
}

func (r *InMemoryEventFabricRepository) ReplayRange(workspaceID, subscriptionID, fromEventID, toEventID int64, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for id, item := range r.deliveries {
		if item.WorkspaceID != workspaceID || item.SubscriptionID != subscriptionID {
			continue
		}
		if fromEventID > 0 && item.OutboxEventID < fromEventID {
			continue
		}
		if toEventID > 0 && item.OutboxEventID > toEventID {
			continue
		}
		item.Status = model.EventFabricDeliveryPending
		item.Attempts = 0
		item.AvailableAt = now
		item.LockedAt = nil
		item.LockedBy = ""
		item.LastError = ""
		item.DeliveredAt = nil
		item.DeadLetteredAt = nil
		item.UpdatedAt = now
		r.deliveries[id] = item
		count++
	}
	return count, nil
}

func (r *InMemoryEventFabricRepository) PurgeDelivered(before time.Time, limit int) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for id, item := range r.deliveries {
		if limit > 0 && count >= int64(limit) {
			break
		}
		if item.Status == model.EventFabricDeliveryDelivered && item.DeliveredAt != nil && item.DeliveredAt.Before(before) {
			delete(r.deliveryKeys, eventFabricDeliveryKey(item.SubscriptionID, item.OutboxEventID))
			delete(r.deliveries, id)
			count++
		}
	}
	return count, nil
}

func (r *InMemoryEventFabricRepository) UpsertOffset(item model.EventConsumerOffset) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.offsets[item.SubscriptionID]
	if ok && current.LastEventID > item.LastEventID {
		return nil
	}
	r.offsets[item.SubscriptionID] = item
	return nil
}

func (r *InMemoryEventFabricRepository) GetOffset(workspaceID, subscriptionID int64) (model.EventConsumerOffset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.offsets[subscriptionID]
	if !ok {
		return model.EventConsumerOffset{SubscriptionID: subscriptionID, WorkspaceID: workspaceID}, nil
	}
	if item.WorkspaceID != workspaceID {
		return model.EventConsumerOffset{}, ErrEventFabricSubscriptionNotFound
	}
	return item, nil
}

func cloneEventSchema(item model.EventSchemaVersion) model.EventSchemaVersion {
	item.Schema = cloneEventFabricMap(item.Schema)
	return item
}

func cloneEventFabricSubscription(item model.EventFabricSubscription) model.EventFabricSubscription {
	item.EventTypes = append([]string(nil), item.EventTypes...)
	return item
}

func cloneEventFabricDelivery(item model.EventFabricDelivery) model.EventFabricDelivery {
	item.Payload = cloneEventFabricMap(item.Payload)
	return item
}

func cloneEventFabricMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func eventPatternMatches(pattern, eventType string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		return strings.HasPrefix(eventType, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == eventType
}

func eventFabricDeliveryKey(subscriptionID, eventID int64) string {
	return strings.Join([]string{formatEventFabricInt(subscriptionID), formatEventFabricInt(eventID)}, "|")
}

func formatEventFabricInt(value int64) string {
	if value == 0 {
		return "0"
	}
	const digits = "0123456789"
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = digits[value%10]
		value /= 10
	}
	return string(buf[i:])
}

func minEventFabricInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
