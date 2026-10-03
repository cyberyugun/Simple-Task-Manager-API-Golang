package repository

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrIntegrationConnectionNotFound = errors.New("integration connection not found")
	ErrIntegrationDeliveryNotFound   = errors.New("integration delivery not found")
	ErrIntegrationInboundDuplicate   = errors.New("integration inbound event already exists")
)

type IntegrationRepository interface {
	CreateIntegrationConnection(connection model.IntegrationConnection, encryptedCredentials string) (model.IntegrationConnection, error)
	UpdateIntegrationConnection(connection model.IntegrationConnection, encryptedCredentials *string) (model.IntegrationConnection, error)
	GetIntegrationConnection(organizationID, connectionID int64) (model.IntegrationConnection, error)
	GetIntegrationConnectionSecret(connectionID int64) (model.IntegrationConnectionSecret, error)
	ListIntegrationConnections(organizationID int64) ([]model.IntegrationConnection, error)
	UpdateIntegrationHealth(connectionID int64, health string, failures int, checkedAt time.Time, rateLimitRemaining *int64, rateLimitResetAt *time.Time) error

	CreateIntegrationDelivery(delivery model.IntegrationDelivery) (model.IntegrationDelivery, error)
	ListIntegrationDeliveries(organizationID int64, limit int) ([]model.IntegrationDelivery, error)
	GetIntegrationDelivery(organizationID, deliveryID int64) (model.IntegrationDelivery, error)
	ClaimIntegrationDeliveries(workerID string, limit int, now time.Time) ([]model.IntegrationDelivery, error)
	MarkIntegrationDelivered(deliveryID int64, httpStatus int, now time.Time) error
	MarkIntegrationFailed(deliveryID int64, httpStatus int, failure string, now time.Time) (bool, error)
	ReplayIntegrationDelivery(organizationID, deliveryID int64, now time.Time) error
	ReleaseIntegrationLocks(before time.Time) (int64, error)

	RecordInboundIntegrationEvent(event model.IntegrationInboundEvent) (model.IntegrationInboundEvent, error)
	ListInboundIntegrationEvents(organizationID int64, limit int) ([]model.IntegrationInboundEvent, error)
}

type inMemoryIntegrationConnection struct {
	item   model.IntegrationConnection
	secret string
}

type InMemoryIntegrationRepository struct {
	mu          sync.Mutex
	connections map[int64]inMemoryIntegrationConnection
	deliveries  map[int64]model.IntegrationDelivery
	inbound     map[int64]model.IntegrationInboundEvent
	inboundKeys map[string]int64
	nextConnID  int64
	nextDelivID int64
	nextInID    int64
}

func NewInMemoryIntegrationRepository() *InMemoryIntegrationRepository {
	return &InMemoryIntegrationRepository{
		connections: make(map[int64]inMemoryIntegrationConnection),
		deliveries:  make(map[int64]model.IntegrationDelivery),
		inbound:     make(map[int64]model.IntegrationInboundEvent),
		inboundKeys: make(map[string]int64),
		nextConnID:  1, nextDelivID: 1, nextInID: 1,
	}
}

func (r *InMemoryIntegrationRepository) CreateIntegrationConnection(connection model.IntegrationConnection, encryptedCredentials string) (model.IntegrationConnection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	connection.ID = r.nextConnID
	r.nextConnID++
	connection.Config = cloneIntegrationMap(connection.Config)
	r.connections[connection.ID] = inMemoryIntegrationConnection{item: connection, secret: encryptedCredentials}
	return cloneIntegrationConnection(connection), nil
}

func (r *InMemoryIntegrationRepository) UpdateIntegrationConnection(connection model.IntegrationConnection, encryptedCredentials *string) (model.IntegrationConnection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.connections[connection.ID]
	if !ok || stored.item.OrganizationID != connection.OrganizationID {
		return model.IntegrationConnection{}, ErrIntegrationConnectionNotFound
	}
	connection.CreatedAt = stored.item.CreatedAt
	connection.CreatedByUserID = stored.item.CreatedByUserID
	if encryptedCredentials != nil {
		stored.secret = *encryptedCredentials
	}
	connection.Config = cloneIntegrationMap(connection.Config)
	stored.item = connection
	r.connections[connection.ID] = stored
	return cloneIntegrationConnection(connection), nil
}

func (r *InMemoryIntegrationRepository) GetIntegrationConnection(organizationID, connectionID int64) (model.IntegrationConnection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.connections[connectionID]
	if !ok || stored.item.OrganizationID != organizationID {
		return model.IntegrationConnection{}, ErrIntegrationConnectionNotFound
	}
	return cloneIntegrationConnection(stored.item), nil
}

func (r *InMemoryIntegrationRepository) GetIntegrationConnectionSecret(connectionID int64) (model.IntegrationConnectionSecret, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.connections[connectionID]
	if !ok {
		return model.IntegrationConnectionSecret{}, ErrIntegrationConnectionNotFound
	}
	return model.IntegrationConnectionSecret{
		ConnectionID: connectionID, OrganizationID: stored.item.OrganizationID, EncryptedCredentials: stored.secret,
	}, nil
}

func (r *InMemoryIntegrationRepository) ListIntegrationConnections(organizationID int64) ([]model.IntegrationConnection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.IntegrationConnection, 0)
	for _, stored := range r.connections {
		if stored.item.OrganizationID == organizationID {
			items = append(items, cloneIntegrationConnection(stored.item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryIntegrationRepository) UpdateIntegrationHealth(connectionID int64, health string, failures int, checkedAt time.Time, rateLimitRemaining *int64, rateLimitResetAt *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, ok := r.connections[connectionID]
	if !ok {
		return ErrIntegrationConnectionNotFound
	}
	stored.item.HealthStatus = health
	stored.item.ConsecutiveFailures = failures
	stored.item.LastHealthCheckedAt = &checkedAt
	stored.item.RateLimitRemaining = rateLimitRemaining
	stored.item.RateLimitResetAt = rateLimitResetAt
	stored.item.UpdatedAt = checkedAt
	r.connections[connectionID] = stored
	return nil
}

func (r *InMemoryIntegrationRepository) CreateIntegrationDelivery(delivery model.IntegrationDelivery) (model.IntegrationDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delivery.ID = r.nextDelivID
	r.nextDelivID++
	delivery.Payload = cloneIntegrationMap(delivery.Payload)
	r.deliveries[delivery.ID] = delivery
	return cloneIntegrationDelivery(delivery), nil
}

func (r *InMemoryIntegrationRepository) ListIntegrationDeliveries(organizationID int64, limit int) ([]model.IntegrationDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.IntegrationDelivery, 0)
	for _, item := range r.deliveries {
		if item.OrganizationID == organizationID {
			items = append(items, cloneIntegrationDelivery(item))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryIntegrationRepository) GetIntegrationDelivery(organizationID, deliveryID int64) (model.IntegrationDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.deliveries[deliveryID]
	if !ok || item.OrganizationID != organizationID {
		return model.IntegrationDelivery{}, ErrIntegrationDeliveryNotFound
	}
	return cloneIntegrationDelivery(item), nil
}

func (r *InMemoryIntegrationRepository) ClaimIntegrationDeliveries(workerID string, limit int, now time.Time) ([]model.IntegrationDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]int64, 0)
	for id, item := range r.deliveries {
		if (item.Status == model.IntegrationDeliveryPending || item.Status == model.IntegrationDeliveryRetry) &&
			item.DeadLetteredAt == nil && item.LockedAt == nil && !item.AvailableAt.After(now) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) > limit {
		ids = ids[:limit]
	}
	items := make([]model.IntegrationDelivery, 0, len(ids))
	for _, id := range ids {
		item := r.deliveries[id]
		t := now
		item.LockedAt = &t
		item.LockedBy = workerID
		r.deliveries[id] = item
		items = append(items, cloneIntegrationDelivery(item))
	}
	return items, nil
}

func (r *InMemoryIntegrationRepository) MarkIntegrationDelivered(deliveryID int64, httpStatus int, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.deliveries[deliveryID]
	if !ok {
		return ErrIntegrationDeliveryNotFound
	}
	item.Status = model.IntegrationDeliveryDelivered
	item.LastHTTPStatus = httpStatus
	item.DeliveredAt = &now
	item.LockedAt = nil
	item.LockedBy = ""
	item.LastError = ""
	item.UpdatedAt = now
	r.deliveries[deliveryID] = item
	return nil
}

func (r *InMemoryIntegrationRepository) MarkIntegrationFailed(deliveryID int64, httpStatus int, failure string, now time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.deliveries[deliveryID]
	if !ok {
		return false, ErrIntegrationDeliveryNotFound
	}
	item.Attempts++
	item.LastHTTPStatus = httpStatus
	item.LastError = failure
	item.LockedAt = nil
	item.LockedBy = ""
	item.UpdatedAt = now
	dead := item.Attempts >= item.MaxAttempts
	if dead {
		item.Status = model.IntegrationDeliveryDeadLetter
		item.DeadLetteredAt = &now
	} else {
		item.Status = model.IntegrationDeliveryRetry
		delay := time.Duration(1<<minIntegrationInt(item.Attempts, 8)) * time.Second
		item.AvailableAt = now.Add(delay)
	}
	r.deliveries[deliveryID] = item
	return dead, nil
}

func (r *InMemoryIntegrationRepository) ReplayIntegrationDelivery(organizationID, deliveryID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.deliveries[deliveryID]
	if !ok || item.OrganizationID != organizationID {
		return ErrIntegrationDeliveryNotFound
	}
	item.Status = model.IntegrationDeliveryPending
	item.Attempts = 0
	item.AvailableAt = now
	item.LockedAt = nil
	item.LockedBy = ""
	item.LastError = ""
	item.LastHTTPStatus = 0
	item.DeliveredAt = nil
	item.DeadLetteredAt = nil
	item.UpdatedAt = now
	r.deliveries[deliveryID] = item
	return nil
}

func (r *InMemoryIntegrationRepository) ReleaseIntegrationLocks(before time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for id, item := range r.deliveries {
		if item.LockedAt != nil && item.LockedAt.Before(before) &&
			item.Status != model.IntegrationDeliveryDelivered && item.Status != model.IntegrationDeliveryDeadLetter {
			item.LockedAt = nil
			item.LockedBy = ""
			r.deliveries[id] = item
			count++
		}
	}
	return count, nil
}

func (r *InMemoryIntegrationRepository) RecordInboundIntegrationEvent(event model.IntegrationInboundEvent) (model.IntegrationInboundEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := integrationInboundKey(event.ConnectionID, event.ProviderEventID)
	if _, ok := r.inboundKeys[key]; ok {
		return model.IntegrationInboundEvent{}, ErrIntegrationInboundDuplicate
	}
	event.ID = r.nextInID
	r.nextInID++
	event.Payload = cloneIntegrationMap(event.Payload)
	r.inbound[event.ID] = event
	r.inboundKeys[key] = event.ID
	return event, nil
}

func (r *InMemoryIntegrationRepository) ListInboundIntegrationEvents(organizationID int64, limit int) ([]model.IntegrationInboundEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.IntegrationInboundEvent, 0)
	for _, item := range r.inbound {
		if item.OrganizationID == organizationID {
			clone := item
			clone.Payload = cloneIntegrationMap(item.Payload)
			items = append(items, clone)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ReceivedAt.Equal(items[j].ReceivedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].ReceivedAt.After(items[j].ReceivedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func cloneIntegrationConnection(item model.IntegrationConnection) model.IntegrationConnection {
	clone := item
	clone.Config = cloneIntegrationMap(item.Config)
	return clone
}

func cloneIntegrationDelivery(item model.IntegrationDelivery) model.IntegrationDelivery {
	clone := item
	clone.Payload = cloneIntegrationMap(item.Payload)
	return clone
}

func cloneIntegrationMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func integrationInboundKey(connectionID int64, eventID string) string {
	return fmt.Sprintf("%d|%s", connectionID, eventID)
}

func minIntegrationInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
