package repository

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrNotificationNotFound         = errors.New("notification not found")
	ErrNotificationEndpointNotFound = errors.New("notification endpoint not found")
	ErrNotificationTemplateNotFound = errors.New("notification template not found")
	ErrNotificationDeliveryNotFound = errors.New("notification delivery not found")
)

type NotificationRepository interface {
	GetPreference(userID int64) (model.NotificationPreference, error)
	UpsertPreference(item model.NotificationPreference) (model.NotificationPreference, error)
	ListDigestPreferences() ([]model.NotificationPreference, error)

	CreateEndpoint(item model.NotificationEndpoint) (model.NotificationEndpoint, error)
	ListEndpoints(userID int64) ([]model.NotificationEndpoint, error)
	GetEndpoint(userID, endpointID int64) (model.NotificationEndpoint, error)
	DeleteEndpoint(userID, endpointID int64, at time.Time) error

	CreateTemplate(item model.NotificationTemplate) (model.NotificationTemplate, error)
	ListTemplates(organizationID int64) ([]model.NotificationTemplate, error)
	GetTemplate(organizationID *int64, templateID int64) (model.NotificationTemplate, error)
	UpdateTemplate(item model.NotificationTemplate) (model.NotificationTemplate, error)
	NextTemplateVersion(organizationID *int64, key, locale, channel string) (int, error)
	GetPublishedTemplate(organizationID *int64, key, locale, channel string) (model.NotificationTemplate, error)

	CreateNotification(item model.Notification) (model.Notification, bool, error)
	GetNotification(userID, notificationID int64) (model.Notification, error)
	ListNotifications(userID int64, unreadOnly bool, limit int) ([]model.Notification, int64, error)
	MarkNotificationRead(userID, notificationID int64, at time.Time) (model.Notification, error)
	MarkAllNotificationsRead(userID int64, at time.Time) (int64, error)
	UnreadCountSince(userID int64, since time.Time) (int64, error)

	CreateDelivery(item model.NotificationDelivery) (model.NotificationDelivery, error)
	ClaimDeliveries(workerID string, limit int, now time.Time) ([]model.NotificationDeliveryDetail, error)
	MarkDeliverySent(deliveryID int64, at time.Time) (model.NotificationDelivery, error)
	MarkDeliveryFailed(deliveryID int64, failure string, at time.Time) (model.NotificationDelivery, error)
	ListDeliveries(userID int64, limit int) ([]model.NotificationDelivery, error)
	RetryDelivery(userID, deliveryID int64, at time.Time) (model.NotificationDelivery, error)
	ReleaseStaleDeliveryLocks(before time.Time) (int64, error)

	ListReminderCandidates(now, horizon time.Time, limit int) ([]model.NotificationReminderCandidate, error)
	ListApprovalCandidates(limit int) ([]model.NotificationApprovalCandidate, error)
	WorkspaceOrganization(workspaceID int64) (*int64, error)
}

type InMemoryNotificationRepository struct {
	mu sync.Mutex

	preferences   map[int64]model.NotificationPreference
	endpoints     map[int64]model.NotificationEndpoint
	templates     map[int64]model.NotificationTemplate
	notifications map[int64]model.Notification
	deliveries    map[int64]model.NotificationDelivery

	nextEndpointID     int64
	nextTemplateID     int64
	nextNotificationID int64
	nextDeliveryID     int64
}

func NewInMemoryNotificationRepository() *InMemoryNotificationRepository {
	return &InMemoryNotificationRepository{
		preferences:        map[int64]model.NotificationPreference{},
		endpoints:          map[int64]model.NotificationEndpoint{},
		templates:          map[int64]model.NotificationTemplate{},
		notifications:      map[int64]model.Notification{},
		deliveries:         map[int64]model.NotificationDelivery{},
		nextEndpointID:     1,
		nextTemplateID:     1,
		nextNotificationID: 1,
		nextDeliveryID:     1,
	}
}

func defaultNotificationPreference(userID int64) model.NotificationPreference {
	now := time.Now().UTC()
	return model.NotificationPreference{
		UserID: userID, Locale: "en", Timezone: "UTC",
		DigestFrequency: model.NotificationDigestOff, DigestHour: 8,
		Channels: map[string]bool{
			model.NotificationChannelInApp:   true,
			model.NotificationChannelEmail:   false,
			model.NotificationChannelPush:    false,
			model.NotificationChannelWebhook: false,
		},
		Events: map[string]bool{}, CreatedAt: now, UpdatedAt: now,
	}
}

func (r *InMemoryNotificationRepository) GetPreference(userID int64) (model.NotificationPreference, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.preferences[userID]
	if !ok {
		item = defaultNotificationPreference(userID)
		r.preferences[userID] = item
	}
	return cloneNotificationPreference(item), nil
}

func (r *InMemoryNotificationRepository) UpsertPreference(item model.NotificationPreference) (model.NotificationPreference, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.preferences[item.UserID]; ok {
		item.CreatedAt = old.CreatedAt
	}
	item = cloneNotificationPreference(item)
	r.preferences[item.UserID] = item
	return cloneNotificationPreference(item), nil
}

func (r *InMemoryNotificationRepository) ListDigestPreferences() ([]model.NotificationPreference, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.NotificationPreference{}
	for _, item := range r.preferences {
		if item.DigestFrequency != model.NotificationDigestOff {
			items = append(items, cloneNotificationPreference(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UserID < items[j].UserID })
	return items, nil
}

func (r *InMemoryNotificationRepository) CreateEndpoint(item model.NotificationEndpoint) (model.NotificationEndpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextEndpointID
	r.nextEndpointID++
	r.endpoints[item.ID] = item
	return item, nil
}

func (r *InMemoryNotificationRepository) ListEndpoints(userID int64) ([]model.NotificationEndpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.NotificationEndpoint{}
	for _, item := range r.endpoints {
		if item.UserID == userID && item.DeletedAt == nil {
			item.Secret = ""
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryNotificationRepository) GetEndpoint(userID, endpointID int64) (model.NotificationEndpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.endpoints[endpointID]
	if !ok || item.UserID != userID || item.DeletedAt != nil {
		return model.NotificationEndpoint{}, ErrNotificationEndpointNotFound
	}
	return item, nil
}

func (r *InMemoryNotificationRepository) DeleteEndpoint(userID, endpointID int64, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.endpoints[endpointID]
	if !ok || item.UserID != userID || item.DeletedAt != nil {
		return ErrNotificationEndpointNotFound
	}
	item.Active = false
	item.DeletedAt = &at
	item.UpdatedAt = at
	r.endpoints[endpointID] = item
	return nil
}

func (r *InMemoryNotificationRepository) CreateTemplate(item model.NotificationTemplate) (model.NotificationTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextTemplateID
	r.nextTemplateID++
	r.templates[item.ID] = item
	return item, nil
}

func (r *InMemoryNotificationRepository) ListTemplates(organizationID int64) ([]model.NotificationTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.NotificationTemplate{}
	for _, item := range r.templates {
		if item.OrganizationID != nil && *item.OrganizationID == organizationID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Key != items[j].Key {
			return items[i].Key < items[j].Key
		}
		if items[i].Locale != items[j].Locale {
			return items[i].Locale < items[j].Locale
		}
		if items[i].Channel != items[j].Channel {
			return items[i].Channel < items[j].Channel
		}
		return items[i].Version < items[j].Version
	})
	return items, nil
}

func (r *InMemoryNotificationRepository) GetTemplate(organizationID *int64, templateID int64) (model.NotificationTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.templates[templateID]
	if !ok || !sameOptionalInt64(item.OrganizationID, organizationID) {
		return model.NotificationTemplate{}, ErrNotificationTemplateNotFound
	}
	return item, nil
}

func (r *InMemoryNotificationRepository) UpdateTemplate(item model.NotificationTemplate) (model.NotificationTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.templates[item.ID]
	if !ok || !sameOptionalInt64(old.OrganizationID, item.OrganizationID) {
		return model.NotificationTemplate{}, ErrNotificationTemplateNotFound
	}
	item.CreatedAt = old.CreatedAt
	r.templates[item.ID] = item
	return item, nil
}

func (r *InMemoryNotificationRepository) NextTemplateVersion(organizationID *int64, key, locale, channel string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := 1
	for _, item := range r.templates {
		if sameOptionalInt64(item.OrganizationID, organizationID) && item.Key == key && item.Locale == locale && item.Channel == channel && item.Version >= next {
			next = item.Version + 1
		}
	}
	return next, nil
}

func (r *InMemoryNotificationRepository) GetPublishedTemplate(organizationID *int64, key, locale, channel string) (model.NotificationTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var best model.NotificationTemplate
	found := false
	for _, item := range r.templates {
		if sameOptionalInt64(item.OrganizationID, organizationID) && item.Key == key && item.Locale == locale && item.Channel == channel && item.Status == model.NotificationTemplatePublished {
			if !found || item.Version > best.Version {
				best, found = item, true
			}
		}
	}
	if !found && organizationID != nil {
		for _, item := range r.templates {
			if item.OrganizationID == nil && item.Key == key && item.Locale == locale && item.Channel == channel && item.Status == model.NotificationTemplatePublished {
				if !found || item.Version > best.Version {
					best, found = item, true
				}
			}
		}
	}
	if !found {
		return model.NotificationTemplate{}, ErrNotificationTemplateNotFound
	}
	return best, nil
}

func (r *InMemoryNotificationRepository) CreateNotification(item model.Notification) (model.Notification, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.notifications {
		if existing.UserID == item.UserID && existing.DedupeKey == item.DedupeKey {
			return cloneNotification(existing), false, nil
		}
	}
	item.ID = r.nextNotificationID
	r.nextNotificationID++
	item = cloneNotification(item)
	r.notifications[item.ID] = item
	return cloneNotification(item), true, nil
}

func (r *InMemoryNotificationRepository) GetNotification(userID, notificationID int64) (model.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.notifications[notificationID]
	if !ok || item.UserID != userID {
		return model.Notification{}, ErrNotificationNotFound
	}
	return cloneNotification(item), nil
}

func (r *InMemoryNotificationRepository) ListNotifications(userID int64, unreadOnly bool, limit int) ([]model.Notification, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.Notification{}
	var unread int64
	for _, item := range r.notifications {
		if item.UserID != userID {
			continue
		}
		if item.ReadAt == nil {
			unread++
		}
		if unreadOnly && item.ReadAt != nil {
			continue
		}
		items = append(items, cloneNotification(item))
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
	return items, unread, nil
}

func (r *InMemoryNotificationRepository) MarkNotificationRead(userID, notificationID int64, at time.Time) (model.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.notifications[notificationID]
	if !ok || item.UserID != userID {
		return model.Notification{}, ErrNotificationNotFound
	}
	if item.ReadAt == nil {
		item.ReadAt = &at
		item.UpdatedAt = at
		r.notifications[item.ID] = item
	}
	return cloneNotification(item), nil
}

func (r *InMemoryNotificationRepository) MarkAllNotificationsRead(userID int64, at time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for id, item := range r.notifications {
		if item.UserID == userID && item.ReadAt == nil {
			item.ReadAt = &at
			item.UpdatedAt = at
			r.notifications[id] = item
			count++
		}
	}
	return count, nil
}

func (r *InMemoryNotificationRepository) UnreadCountSince(userID int64, since time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for _, item := range r.notifications {
		if item.UserID == userID && item.ReadAt == nil && !item.CreatedAt.Before(since) && item.EventType != model.NotificationEventDigestSummary {
			count++
		}
	}
	return count, nil
}

func (r *InMemoryNotificationRepository) CreateDelivery(item model.NotificationDelivery) (model.NotificationDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.deliveries {
		if existing.NotificationID == item.NotificationID && existing.Channel == item.Channel && existing.Destination == item.Destination {
			return existing, nil
		}
	}
	item.ID = r.nextDeliveryID
	r.nextDeliveryID++
	r.deliveries[item.ID] = item
	return item, nil
}

func (r *InMemoryNotificationRepository) ClaimDeliveries(workerID string, limit int, now time.Time) ([]model.NotificationDeliveryDetail, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.NotificationDeliveryDetail{}
	ids := make([]int64, 0)
	for id, item := range r.deliveries {
		if (item.Status == model.NotificationDeliveryPending || item.Status == model.NotificationDeliveryRetry) && !item.AvailableAt.After(now) {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := r.deliveries[ids[i]], r.deliveries[ids[j]]
		if a.AvailableAt.Equal(b.AvailableAt) {
			return a.ID < b.ID
		}
		return a.AvailableAt.Before(b.AvailableAt)
	})
	if limit > 0 && len(ids) > limit {
		ids = ids[:limit]
	}
	for _, id := range ids {
		d := r.deliveries[id]
		n := r.notifications[d.NotificationID]
		items = append(items, model.NotificationDeliveryDetail{Delivery: d, Notification: cloneNotification(n)})
	}
	return items, nil
}

func (r *InMemoryNotificationRepository) MarkDeliverySent(deliveryID int64, at time.Time) (model.NotificationDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.deliveries[deliveryID]
	if !ok {
		return model.NotificationDelivery{}, ErrNotificationDeliveryNotFound
	}
	item.Status = model.NotificationDeliverySent
	item.Attempts++
	item.LastError = ""
	item.SentAt = &at
	item.UpdatedAt = at
	r.deliveries[item.ID] = item
	return item, nil
}

func (r *InMemoryNotificationRepository) MarkDeliveryFailed(deliveryID int64, failure string, at time.Time) (model.NotificationDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.deliveries[deliveryID]
	if !ok {
		return model.NotificationDelivery{}, ErrNotificationDeliveryNotFound
	}
	item.Attempts++
	item.LastError = failure
	item.UpdatedAt = at
	if item.Attempts >= item.MaxAttempts {
		item.Status = model.NotificationDeliveryDeadLetter
	} else {
		item.Status = model.NotificationDeliveryRetry
		backoff := time.Duration(1<<minNotificationInt(item.Attempts, 8)) * time.Second
		item.AvailableAt = at.Add(backoff)
	}
	r.deliveries[item.ID] = item
	return item, nil
}

func (r *InMemoryNotificationRepository) ListDeliveries(userID int64, limit int) ([]model.NotificationDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.NotificationDelivery{}
	for _, item := range r.deliveries {
		if item.UserID == userID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryNotificationRepository) RetryDelivery(userID, deliveryID int64, at time.Time) (model.NotificationDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.deliveries[deliveryID]
	if !ok || item.UserID != userID {
		return model.NotificationDelivery{}, ErrNotificationDeliveryNotFound
	}
	item.Status = model.NotificationDeliveryPending
	item.Attempts = 0
	item.LastError = ""
	item.AvailableAt = at
	item.SentAt = nil
	item.UpdatedAt = at
	r.deliveries[item.ID] = item
	return item, nil
}

func (r *InMemoryNotificationRepository) ReleaseStaleDeliveryLocks(time.Time) (int64, error) {
	return 0, nil
}

func (r *InMemoryNotificationRepository) ListReminderCandidates(time.Time, time.Time, int) ([]model.NotificationReminderCandidate, error) {
	return []model.NotificationReminderCandidate{}, nil
}

func (r *InMemoryNotificationRepository) ListApprovalCandidates(int) ([]model.NotificationApprovalCandidate, error) {
	return []model.NotificationApprovalCandidate{}, nil
}

func (r *InMemoryNotificationRepository) WorkspaceOrganization(int64) (*int64, error) {
	return nil, nil
}

func sameOptionalInt64(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func cloneNotificationPreference(item model.NotificationPreference) model.NotificationPreference {
	item.Channels = cloneBoolMap(item.Channels)
	item.Events = cloneBoolMap(item.Events)
	return item
}

func cloneNotification(item model.Notification) model.Notification {
	raw, _ := json.Marshal(item.Data)
	item.Data = map[string]any{}
	_ = json.Unmarshal(raw, &item.Data)
	return item
}

func cloneBoolMap(input map[string]bool) map[string]bool {
	output := make(map[string]bool, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func minNotificationInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func normalizeNotificationAddress(value string) string {
	return strings.TrimSpace(value)
}
