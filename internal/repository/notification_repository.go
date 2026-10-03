package repository

import (
	"errors"
	"sort"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrNotificationNotFound           = errors.New("notification not found")
	ErrNotificationPreferenceNotFound = errors.New("notification preference not found")
	ErrNotificationTemplateNotFound   = errors.New("notification template not found")
)

type NotificationRepository interface {
	GetPreference(userID int64, organizationID *int64) (model.NotificationPreference, error)
	UpsertPreference(model.NotificationPreference) (model.NotificationPreference, error)
	CreateNotification(model.Notification) (model.Notification, bool, error)
	GetNotification(notificationID int64) (model.Notification, error)
	ListNotifications(userID int64, limit int) ([]model.Notification, error)
	MarkNotificationRead(userID, notificationID int64, at time.Time) (model.Notification, error)

	CreateDelivery(model.NotificationDelivery) (model.NotificationDelivery, error)
	ClaimReadyDeliveries(now time.Time, limit int) ([]model.NotificationDelivery, error)
	UpdateDelivery(model.NotificationDelivery) (model.NotificationDelivery, error)

	ActiveTemplate(key, channel, locale string) (model.NotificationTemplate, error)
	IsSuppressed(organizationID int64, eventType, channel string) (bool, error)
	ListReminderCandidates(now, dueBefore time.Time, limit int) ([]model.ReminderCandidate, error)
	ListOrganizationAdmins(organizationID int64) ([]int64, error)
	Stats(userID int64, organizationID *int64) (model.NotificationStats, error)
	RecordNotificationAudit(organizationID *int64, userID *int64, action string, notificationID, deliveryID *int64, metadata map[string]any, at time.Time) error
}

type InMemoryNotificationRepository struct {
	mu                 sync.Mutex
	preferences        map[string]model.NotificationPreference
	notifications      map[int64]model.Notification
	deliveries         map[int64]model.NotificationDelivery
	templates          []model.NotificationTemplate
	suppressions       []model.NotificationSuppressionRule
	admins             map[int64][]int64
	reminders          []model.ReminderCandidate
	nextNotificationID int64
	nextDeliveryID     int64
}

func NewInMemoryNotificationRepository() *InMemoryNotificationRepository {
	return &InMemoryNotificationRepository{
		preferences:        map[string]model.NotificationPreference{},
		notifications:      map[int64]model.Notification{},
		deliveries:         map[int64]model.NotificationDelivery{},
		admins:             map[int64][]int64{},
		nextNotificationID: 1, nextDeliveryID: 1,
	}
}

func notificationPreferenceKey(userID int64, organizationID *int64) string {
	if organizationID == nil {
		return fmtInt(userID) + ":0"
	}
	return fmtInt(userID) + ":" + fmtInt(*organizationID)
}

func fmtInt(value int64) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var b [32]byte
	i := len(b)
	for value > 0 {
		i--
		b[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func (r *InMemoryNotificationRepository) GetPreference(userID int64, organizationID *int64) (model.NotificationPreference, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.preferences[notificationPreferenceKey(userID, organizationID)]
	if !ok {
		return model.NotificationPreference{}, ErrNotificationPreferenceNotFound
	}
	return item, nil
}
func (r *InMemoryNotificationRepository) UpsertPreference(item model.NotificationPreference) (model.NotificationPreference, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.preferences[notificationPreferenceKey(item.UserID, item.OrganizationID)] = item
	return item, nil
}
func (r *InMemoryNotificationRepository) CreateNotification(item model.Notification) (model.Notification, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if item.DedupKey != "" {
		for _, existing := range r.notifications {
			if existing.UserID == item.UserID && existing.DedupKey == item.DedupKey {
				return existing, false, nil
			}
		}
	}
	item.ID = r.nextNotificationID
	r.nextNotificationID++
	r.notifications[item.ID] = item
	return item, true, nil
}
func (r *InMemoryNotificationRepository) GetNotification(notificationID int64) (model.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.notifications[notificationID]
	if !ok {
		return model.Notification{}, ErrNotificationNotFound
	}
	return v, nil
}
func (r *InMemoryNotificationRepository) ListNotifications(userID int64, limit int) ([]model.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []model.Notification{}
	for _, v := range r.notifications {
		if v.UserID != userID {
			continue
		}
		visible := false
		for _, d := range r.deliveries {
			if d.NotificationID == v.ID && d.Channel == model.NotificationChannelInApp && d.Status != model.NotificationDeliverySuppressed {
				visible = true
				break
			}
		}
		if visible {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (r *InMemoryNotificationRepository) MarkNotificationRead(userID, notificationID int64, at time.Time) (model.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.notifications[notificationID]
	if !ok || v.UserID != userID {
		return model.Notification{}, ErrNotificationNotFound
	}
	v.ReadAt = &at
	r.notifications[v.ID] = v
	return v, nil
}
func (r *InMemoryNotificationRepository) CreateDelivery(item model.NotificationDelivery) (model.NotificationDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextDeliveryID
	r.nextDeliveryID++
	r.deliveries[item.ID] = item
	return item, nil
}
func (r *InMemoryNotificationRepository) ClaimReadyDeliveries(now time.Time, limit int) ([]model.NotificationDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []model.NotificationDelivery{}
	for _, v := range r.deliveries {
		ready := v.ScheduledAt
		if v.NextAttemptAt != nil {
			ready = *v.NextAttemptAt
		}
		if (v.Status == model.NotificationDeliveryPending || v.Status == model.NotificationDeliveryFailed) && !ready.After(now) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (r *InMemoryNotificationRepository) UpdateDelivery(item model.NotificationDelivery) (model.NotificationDelivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.deliveries[item.ID]; !ok {
		return model.NotificationDelivery{}, ErrNotificationNotFound
	}
	r.deliveries[item.ID] = item
	return item, nil
}
func (r *InMemoryNotificationRepository) ActiveTemplate(key, channel, locale string) (model.NotificationTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var best model.NotificationTemplate
	found := false
	for _, v := range r.templates {
		if v.Key == key && v.Channel == channel && v.Locale == locale && v.Active && (!found || v.Version > best.Version) {
			best = v
			found = true
		}
	}
	if !found {
		return model.NotificationTemplate{}, ErrNotificationTemplateNotFound
	}
	return best, nil
}
func (r *InMemoryNotificationRepository) IsSuppressed(orgID int64, eventType, channel string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.suppressions {
		if v.OrganizationID == orgID && v.Active && (v.EventType == "*" || v.EventType == eventType) && (v.Channel == "*" || v.Channel == channel) {
			return true, nil
		}
	}
	return false, nil
}
func (r *InMemoryNotificationRepository) ListReminderCandidates(now, dueBefore time.Time, limit int) ([]model.ReminderCandidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []model.ReminderCandidate{}
	for _, v := range r.reminders {
		if !v.DueAt.After(dueBefore) {
			out = append(out, v)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (r *InMemoryNotificationRepository) ListOrganizationAdmins(orgID int64) ([]int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int64(nil), r.admins[orgID]...), nil
}
func (r *InMemoryNotificationRepository) Stats(userID int64, orgID *int64) (model.NotificationStats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := model.NotificationStats{ByChannel: map[string]int64{}, ByStatus: map[string]int64{}}
	for _, n := range r.notifications {
		if n.UserID != userID {
			continue
		}
		if orgID != nil && (n.OrganizationID == nil || *n.OrganizationID != *orgID) {
			continue
		}
		s.Total++
		if n.ReadAt == nil {
			s.Unread++
		}
	}
	for _, d := range r.deliveries {
		if d.UserID != userID {
			continue
		}
		s.ByChannel[d.Channel]++
		s.ByStatus[d.Status]++
		if d.Status == model.NotificationDeliveryDeadLetter {
			s.DeadLettered++
		}
		if d.Status == model.NotificationDeliverySuppressed {
			s.Suppressed++
		}
	}
	return s, nil
}
func (r *InMemoryNotificationRepository) RecordNotificationAudit(_ *int64, _ *int64, _ string, _ *int64, _ *int64, _ map[string]any, _ time.Time) error {
	return nil
}
