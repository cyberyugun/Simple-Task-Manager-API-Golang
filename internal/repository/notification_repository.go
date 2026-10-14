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
	ErrNotificationSuppressionRuleNotFound = errors.New("notification suppression rule not found")
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

	CreateSuppressionRule(item model.NotificationSuppressionRule) (model.NotificationSuppressionRule, error)
	ListSuppressionRules(organizationID int64) ([]model.NotificationSuppressionRule, error)
	DeleteSuppressionRule(organizationID, ruleID int64, at time.Time) error
	ListActiveSuppressionRules(organizationID int64, eventType string, now time.Time) ([]model.NotificationSuppressionRule, error)
	RecordSuppression(item model.NotificationSuppressionEvent) error
	NotificationAnalytics(organizationID int64, since, now time.Time) (model.NotificationAnalyticsDashboard, error)

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
	templates          map[int64]model.NotificationTemplate
	suppressionRules   map[int64]model.NotificationSuppressionRule
	suppressionEvents  map[int64]model.NotificationSuppressionEvent
	notifications      map[int64]model.Notification
	deliveries         map[int64]model.NotificationDelivery

	nextEndpointID     int64
	nextTemplateID      int64
	nextSuppressionRuleID int64
	nextSuppressionEventID int64
	nextNotificationID  int64
	nextDeliveryID      int64
}

func NewInMemoryNotificationRepository() *InMemoryNotificationRepository {
	return &InMemoryNotificationRepository{
		preferences:        map[int64]model.NotificationPreference{},
		endpoints:          map[int64]model.NotificationEndpoint{},
		templates:            map[int64]model.NotificationTemplate{},
		suppressionRules:     map[int64]model.NotificationSuppressionRule{},
		suppressionEvents:    map[int64]model.NotificationSuppressionEvent{},
		notifications:        map[int64]model.Notification{},
		deliveries:           map[int64]model.NotificationDelivery{},
		nextEndpointID:       1,
		nextTemplateID:       1,
		nextSuppressionRuleID: 1,
		nextSuppressionEventID: 1,
		nextNotificationID:   1,
		nextDeliveryID:       1,
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

func (r *InMemoryNotificationRepository) CreateSuppressionRule(item model.NotificationSuppressionRule) (model.NotificationSuppressionRule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextSuppressionRuleID
	r.nextSuppressionRuleID++
	r.suppressionRules[item.ID] = item
	return item, nil
}

func (r *InMemoryNotificationRepository) ListSuppressionRules(organizationID int64) ([]model.NotificationSuppressionRule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.NotificationSuppressionRule, 0)
	for _, item := range r.suppressionRules {
		if item.OrganizationID == organizationID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryNotificationRepository) DeleteSuppressionRule(organizationID, ruleID int64, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.suppressionRules[ruleID]
	if !ok || item.OrganizationID != organizationID || !item.Active {
		return ErrNotificationSuppressionRuleNotFound
	}
	item.Active = false
	item.UpdatedAt = at
	r.suppressionRules[ruleID] = item
	return nil
}

func (r *InMemoryNotificationRepository) ListActiveSuppressionRules(organizationID int64, eventType string, now time.Time) ([]model.NotificationSuppressionRule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.NotificationSuppressionRule, 0)
	for _, item := range r.suppressionRules {
		if item.OrganizationID != organizationID || !item.Active {
			continue
		}
		if item.EventType != "*" && item.EventType != eventType {
			continue
		}
		if item.StartsAt != nil && now.Before(*item.StartsAt) {
			continue
		}
		if item.EndsAt != nil && !now.Before(*item.EndsAt) {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].EventType != items[j].EventType {
			return items[i].EventType == eventType
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (r *InMemoryNotificationRepository) RecordSuppression(item model.NotificationSuppressionEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.suppressionEvents {
		if existing.OrganizationID == item.OrganizationID && existing.DedupeKey == item.DedupeKey {
			return nil
		}
	}
	item.ID = r.nextSuppressionEventID
	r.nextSuppressionEventID++
	r.suppressionEvents[item.ID] = item
	return nil
}

func (r *InMemoryNotificationRepository) NotificationAnalytics(organizationID int64, since, now time.Time) (model.NotificationAnalyticsDashboard, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := model.NotificationAnalyticsDashboard{
		OrganizationID: organizationID,
		GeneratedAt: now,
		ByEvent: []model.NotificationEventAnalytics{},
		ByChannel: []model.NotificationChannelAnalytics{},
		Daily: []model.NotificationDailyAnalytics{},
	}
	eventMetrics := map[string]*model.NotificationEventAnalytics{}
	channelMetrics := map[string]*model.NotificationChannelAnalytics{}
	dailyMetrics := map[string]*model.NotificationDailyAnalytics{}
	start := time.Date(since.UTC().Year(), since.UTC().Month(), since.UTC().Day(), 0, 0, 0, 0, time.UTC)
	end := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		dailyMetrics[key] = &model.NotificationDailyAnalytics{Date: key}
	}
	for _, item := range r.notifications {
		if item.OrganizationID == nil || *item.OrganizationID != organizationID || item.CreatedAt.Before(since) || item.CreatedAt.After(now) {
			continue
		}
		result.Summary.Generated++
		metric := eventMetrics[item.EventType]
		if metric == nil {
			metric = &model.NotificationEventAnalytics{EventType: item.EventType}
			eventMetrics[item.EventType] = metric
		}
		metric.Generated++
		day := dailyMetrics[item.CreatedAt.UTC().Format("2006-01-02")]
		if day != nil {
			day.Generated++
		}
		if item.ReadAt != nil {
			result.Summary.Read++
			metric.Read++
			if day != nil {
				day.Read++
			}
		} else {
			result.Summary.Unread++
		}
	}
	for _, delivery := range r.deliveries {
		notification, ok := r.notifications[delivery.NotificationID]
		if !ok || notification.OrganizationID == nil || *notification.OrganizationID != organizationID ||
			delivery.CreatedAt.Before(since) || delivery.CreatedAt.After(now) {
			continue
		}
		result.Summary.Deliveries++
		result.Summary.DeliveryAttempts += int64(delivery.Attempts)
		metric := channelMetrics[delivery.Channel]
		if metric == nil {
			metric = &model.NotificationChannelAnalytics{Channel: delivery.Channel}
			channelMetrics[delivery.Channel] = metric
		}
		metric.Deliveries++
		metric.Attempts += int64(delivery.Attempts)
		switch delivery.Status {
		case model.NotificationDeliverySent:
			result.Summary.Sent++
			metric.Sent++
			if delivery.SentAt != nil {
				if day := dailyMetrics[delivery.SentAt.UTC().Format("2006-01-02")]; day != nil {
					day.Sent++
				}
			}
		case model.NotificationDeliveryRetry:
			result.Summary.Retrying++
			metric.Retrying++
		case model.NotificationDeliveryDeadLetter:
			result.Summary.DeadLetter++
			metric.DeadLetter++
			if day := dailyMetrics[delivery.UpdatedAt.UTC().Format("2006-01-02")]; day != nil {
				day.DeadLetter++
			}
		}
	}
	for _, event := range r.suppressionEvents {
		if event.OrganizationID != organizationID || event.CreatedAt.Before(since) || event.CreatedAt.After(now) {
			continue
		}
		result.Summary.Suppressed++
		eventMetric := eventMetrics[event.EventType]
		if eventMetric == nil {
			eventMetric = &model.NotificationEventAnalytics{EventType: event.EventType}
			eventMetrics[event.EventType] = eventMetric
		}
		eventMetric.Suppressed++
		channelMetric := channelMetrics[event.Channel]
		if channelMetric == nil {
			channelMetric = &model.NotificationChannelAnalytics{Channel: event.Channel}
			channelMetrics[event.Channel] = channelMetric
		}
		channelMetric.Suppressed++
		if day := dailyMetrics[event.CreatedAt.UTC().Format("2006-01-02")]; day != nil {
			day.Suppressed++
		}
	}
	if result.Summary.Deliveries > 0 {
		result.Summary.DeliverySuccessRate = float64(result.Summary.Sent) / float64(result.Summary.Deliveries)
	}
	for _, metric := range eventMetrics {
		result.ByEvent = append(result.ByEvent, *metric)
	}
	sort.Slice(result.ByEvent, func(i, j int) bool { return result.ByEvent[i].EventType < result.ByEvent[j].EventType })
	for _, metric := range channelMetrics {
		result.ByChannel = append(result.ByChannel, *metric)
	}
	sort.Slice(result.ByChannel, func(i, j int) bool { return result.ByChannel[i].Channel < result.ByChannel[j].Channel })
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		result.Daily = append(result.Daily, *dailyMetrics[day.Format("2006-01-02")])
	}
	return result, nil
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
	if !ok || item.UserID != userID || item.Status != model.NotificationDeliveryDeadLetter {
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
