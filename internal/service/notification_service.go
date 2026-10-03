package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrInvalidNotificationPreference  = errors.New("invalid notification preference")
	ErrNotificationForbidden          = errors.New("notification action is forbidden")
	ErrNotificationChannelUnavailable = errors.New("notification channel is unavailable")
)

type NotificationEmitter interface {
	EmitNotificationSignal(model.NotificationSignal) error
	EmitOrganizationAdminsSignal(organizationID int64, eventType, title, body, dedupKey string, data map[string]any) error
}

type NotificationChannelSender interface {
	Channel() string
	Send(context.Context, model.Notification, model.NotificationDelivery) error
}

type LogNotificationSender struct {
	channel string
	logger  *slog.Logger
}

func NewLogNotificationSender(channel string, logger *slog.Logger) *LogNotificationSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogNotificationSender{channel: channel, logger: logger}
}
func (s *LogNotificationSender) Channel() string { return s.channel }
func (s *LogNotificationSender) Send(_ context.Context, n model.Notification, d model.NotificationDelivery) error {
	s.logger.Info("notification_delivery",
		"channel", d.Channel,
		"notification_id", n.ID,
		"user_id", n.UserID,
		"event_type", n.EventType,
		"destination", d.Destination,
	)
	return nil
}

type NotificationService struct {
	repo    repository.NotificationRepository
	users   repository.UserRepository
	orgs    repository.OrganizationRepository
	senders map[string]NotificationChannelSender
}

func NewNotificationService(repo repository.NotificationRepository, users repository.UserRepository, orgs repository.OrganizationRepository, senders ...NotificationChannelSender) *NotificationService {
	s := &NotificationService{
		repo: repo, users: users, orgs: orgs,
		senders: map[string]NotificationChannelSender{},
	}
	for _, sender := range senders {
		if sender == nil {
			continue
		}
		s.senders[sender.Channel()] = sender
	}
	return s
}

func (s *NotificationService) Inbox(userID int64, limit int) ([]model.Notification, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.repo.ListNotifications(userID, limit)
}

func (s *NotificationService) MarkRead(userID, notificationID int64) (model.Notification, error) {
	item, err := s.repo.MarkNotificationRead(userID, notificationID, time.Now().UTC())
	if err == nil {
		_ = s.repo.RecordNotificationAudit(item.OrganizationID, &userID, "notification.read", &item.ID, nil, nil, time.Now().UTC())
	}
	return item, err
}

func (s *NotificationService) Preference(userID int64, organizationID *int64) (model.NotificationPreference, error) {
	if err := s.requireOrganizationMember(userID, organizationID); err != nil {
		return model.NotificationPreference{}, err
	}
	item, err := s.repo.GetPreference(userID, organizationID)
	if errors.Is(err, repository.ErrNotificationPreferenceNotFound) {
		return defaultNotificationPreference(userID, organizationID), nil
	}
	return item, err
}

func (s *NotificationService) UpdatePreference(userID int64, organizationID *int64, req model.UpdateNotificationPreferenceRequest) (model.NotificationPreference, error) {
	if err := s.requireOrganizationMember(userID, organizationID); err != nil {
		return model.NotificationPreference{}, err
	}
	digest := strings.ToLower(strings.TrimSpace(req.Digest))
	if digest == "" {
		digest = model.NotificationDigestImmediate
	}
	if digest != model.NotificationDigestImmediate && digest != model.NotificationDigestHourly && digest != model.NotificationDigestDaily {
		return model.NotificationPreference{}, ErrInvalidNotificationPreference
	}
	timezone := strings.TrimSpace(req.Timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return model.NotificationPreference{}, ErrInvalidNotificationPreference
	}
	locale := strings.TrimSpace(req.Locale)
	if locale == "" {
		locale = "en"
	}
	if len(locale) > 20 || len(req.MutedEventTypes) > 100 {
		return model.NotificationPreference{}, ErrInvalidNotificationPreference
	}
	start, err := normalizeQuietHour(req.QuietHoursStart)
	if err != nil {
		return model.NotificationPreference{}, ErrInvalidNotificationPreference
	}
	end, err := normalizeQuietHour(req.QuietHoursEnd)
	if err != nil {
		return model.NotificationPreference{}, ErrInvalidNotificationPreference
	}
	if (start == "") != (end == "") {
		return model.NotificationPreference{}, ErrInvalidNotificationPreference
	}
	muted := make([]string, 0, len(req.MutedEventTypes))
	seen := map[string]bool{}
	for _, value := range req.MutedEventTypes {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 120 || seen[value] {
			continue
		}
		seen[value] = true
		muted = append(muted, value)
	}
	sort.Strings(muted)
	item := model.NotificationPreference{
		UserID: userID, OrganizationID: organizationID,
		InAppEnabled: req.InAppEnabled, EmailEnabled: req.EmailEnabled,
		PushEnabled: req.PushEnabled, WebhookEnabled: req.WebhookEnabled,
		Digest: digest, Timezone: timezone, Locale: locale,
		QuietHoursStart: start, QuietHoursEnd: end,
		MutedEventTypes: muted, UpdatedAt: time.Now().UTC(),
	}
	updated, err := s.repo.UpsertPreference(item)
	if err == nil {
		_ = s.repo.RecordNotificationAudit(organizationID, &userID, "notification.preference.updated", nil, nil, map[string]any{
			"digest": digest, "timezone": timezone, "locale": locale,
		}, time.Now().UTC())
	}
	return updated, err
}

func (s *NotificationService) Stats(userID int64, organizationID *int64) (model.NotificationStats, error) {
	if err := s.requireOrganizationMember(userID, organizationID); err != nil {
		return model.NotificationStats{}, err
	}
	return s.repo.Stats(userID, organizationID)
}

func (s *NotificationService) EmitNotificationSignal(signal model.NotificationSignal) error {
	if len(signal.UserIDs) == 0 || strings.TrimSpace(signal.EventType) == "" {
		return nil
	}
	userIDs := dedupeInt64(signal.UserIDs)
	for _, userID := range userIDs {
		if userID <= 0 {
			continue
		}
		if err := s.emitForUser(userID, signal); err != nil {
			return err
		}
	}
	return nil
}

func (s *NotificationService) EmitOrganizationAdminsSignal(organizationID int64, eventType, title, body, dedupKey string, data map[string]any) error {
	if s.orgs == nil {
		return ErrNotificationForbidden
	}
	members, err := s.orgs.ListMembers(organizationID)
	if err != nil {
		return err
	}
	users := make([]int64, 0)
	for _, member := range members {
		switch member.Role {
		case model.OrganizationRoleOwner, model.OrganizationRoleAdmin, model.OrganizationRoleDelegatedAdmin:
			users = append(users, member.UserID)
		}
	}
	org := organizationID
	return s.EmitNotificationSignal(model.NotificationSignal{
		UserIDs: users, OrganizationID: &org, EventType: eventType,
		Title: title, Body: body, DedupKey: dedupKey, Data: data,
	})
}

func (s *NotificationService) emitForUser(userID int64, signal model.NotificationSignal) error {
	pref, err := s.repo.GetPreference(userID, signal.OrganizationID)
	if errors.Is(err, repository.ErrNotificationPreferenceNotFound) && signal.OrganizationID != nil {
		pref, err = s.repo.GetPreference(userID, nil)
	}
	if errors.Is(err, repository.ErrNotificationPreferenceNotFound) {
		pref = defaultNotificationPreference(userID, signal.OrganizationID)
		err = nil
	}
	if err != nil {
		return err
	}
	if stringSliceContains(pref.MutedEventTypes, signal.EventType) {
		_ = s.repo.RecordNotificationAudit(signal.OrganizationID, &userID, "notification.suppressed.preference", nil, nil, map[string]any{"event_type": signal.EventType}, time.Now().UTC())
		return nil
	}

	now := time.Now().UTC()
	templateVersion := 0
	title, body := strings.TrimSpace(signal.Title), strings.TrimSpace(signal.Body)
	templateKey := strings.TrimSpace(signal.TemplateKey)
	if templateKey == "" {
		templateKey = signal.EventType
	}
	if templateKey != "" {
		if template, templateErr := s.repo.ActiveTemplate(templateKey, model.NotificationChannelInApp, pref.Locale); templateErr == nil {
			title = renderNotificationTemplate(template.Subject, signal.Data)
			body = renderNotificationTemplate(template.Body, signal.Data)
			templateVersion = template.Version
		} else if pref.Locale != "en" {
			if template, fallbackErr := s.repo.ActiveTemplate(templateKey, model.NotificationChannelInApp, "en"); fallbackErr == nil {
				title = renderNotificationTemplate(template.Subject, signal.Data)
				body = renderNotificationTemplate(template.Body, signal.Data)
				templateVersion = template.Version
			}
		}
	}
	if title == "" {
		title = signal.EventType
	}
	item, created, err := s.repo.CreateNotification(model.Notification{
		UserID: userID, OrganizationID: signal.OrganizationID, WorkspaceID: signal.WorkspaceID,
		EventType: signal.EventType, Title: title, Body: body,
		Data: cloneNotificationMap(signal.Data), DedupKey: notificationDedupKey(signal.DedupKey, userID),
		TemplateKey: templateKey, TemplateVersion: templateVersion, CreatedAt: now,
	})
	if err != nil || !created {
		return err
	}
	_ = s.repo.RecordNotificationAudit(item.OrganizationID, &userID, "notification.created", &item.ID, nil, map[string]any{"event_type": item.EventType}, now)

	user, userErr := s.users.FindByID(userID)
	if userErr != nil {
		return userErr
	}
	channels := []struct {
		name, destination string
		enabled           bool
	}{
		{model.NotificationChannelInApp, "", pref.InAppEnabled},
		{model.NotificationChannelEmail, user.Email, pref.EmailEnabled},
		{model.NotificationChannelPush, "", pref.PushEnabled},
		{model.NotificationChannelWebhook, "", pref.WebhookEnabled},
	}
	for _, channel := range channels {
		if !channel.enabled {
			continue
		}
		suppressed := false
		if item.OrganizationID != nil {
			suppressed, err = s.repo.IsSuppressed(*item.OrganizationID, item.EventType, channel.name)
			if err != nil {
				return err
			}
		}
		status := model.NotificationDeliveryPending
		if suppressed {
			status = model.NotificationDeliverySuppressed
		}
		scheduledAt := scheduleNotificationDelivery(now, pref)
		delivery, err := s.repo.CreateDelivery(model.NotificationDelivery{
			NotificationID: item.ID, UserID: userID, Channel: channel.name, Destination: channel.destination,
			Status: status, Attempt: 0, MaxAttempts: 5, ScheduledAt: scheduledAt,
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
		if delivery.ID != 0 {
			action := "notification.delivery.queued"
			if suppressed {
				action = "notification.delivery.suppressed"
			}
			_ = s.repo.RecordNotificationAudit(item.OrganizationID, &userID, action, &item.ID, &delivery.ID, map[string]any{"channel": channel.name}, now)
		}
	}
	return nil
}

func (s *NotificationService) ProcessReminders(limit int) (int, error) {
	if limit <= 0 {
		limit = 500
	}
	now := time.Now().UTC()
	items, err := s.repo.ListReminderCandidates(now, now.Add(24*time.Hour), limit)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, item := range items {
		workspaceID := item.WorkspaceID
		eventType := model.NotificationEventTaskDueSoon
		title := "Task due soon"
		body := fmt.Sprintf("%s is due at %s", item.Title, item.DueAt.UTC().Format(time.RFC3339))
		kind := "due"
		if item.Kind == "overdue" {
			eventType = model.NotificationEventTaskOverdue
			title = "Task overdue"
			body = fmt.Sprintf("%s was due at %s", item.Title, item.DueAt.UTC().Format(time.RFC3339))
			kind = "overdue"
		}
		dedup := fmt.Sprintf("task-reminder:%s:%d:%d:%s", kind, item.TaskID, item.UserID, item.DueAt.UTC().Format(time.RFC3339))
		if err := s.EmitNotificationSignal(model.NotificationSignal{
			UserIDs: []int64{item.UserID}, WorkspaceID: &workspaceID, EventType: eventType,
			Title: title, Body: body, DedupKey: dedup,
			Data: map[string]any{"task_id": item.TaskID, "due_at": item.DueAt, "kind": item.Kind},
		}); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (s *NotificationService) ProcessDeliveries(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	now := time.Now().UTC()
	items, err := s.repo.ClaimReadyDeliveries(now, limit)
	if err != nil {
		return 0, err
	}
	for _, delivery := range items {
		notification, err := s.repo.GetNotification(delivery.NotificationID)
		if err != nil {
			return 0, err
		}
		sender := s.senders[delivery.Channel]
		delivery.Attempt++
		delivery.UpdatedAt = time.Now().UTC()
		if sender == nil {
			err = ErrNotificationChannelUnavailable
		} else {
			sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			err = sender.Send(sendCtx, notification, delivery)
			cancel()
		}
		if err == nil {
			sentAt := time.Now().UTC()
			delivery.Status = model.NotificationDeliverySent
			delivery.SentAt = &sentAt
			delivery.NextAttemptAt = nil
			delivery.LastError = ""
		} else {
			delivery.LastError = err.Error()
			if delivery.Attempt >= delivery.MaxAttempts {
				delivery.Status = model.NotificationDeliveryDeadLetter
				delivery.NextAttemptAt = nil
			} else {
				delivery.Status = model.NotificationDeliveryFailed
				retryAt := time.Now().UTC().Add(notificationBackoff(delivery.Attempt))
				delivery.NextAttemptAt = &retryAt
			}
		}
		updated, updateErr := s.repo.UpdateDelivery(delivery)
		if updateErr != nil {
			return 0, updateErr
		}
		action := "notification.delivery.sent"
		if updated.Status == model.NotificationDeliveryFailed {
			action = "notification.delivery.retry_scheduled"
		}
		if updated.Status == model.NotificationDeliveryDeadLetter {
			action = "notification.delivery.dead_lettered"
		}
		_ = s.repo.RecordNotificationAudit(notification.OrganizationID, &notification.UserID, action, &notification.ID, &updated.ID, map[string]any{
			"channel": updated.Channel, "attempt": updated.Attempt, "error": updated.LastError,
		}, time.Now().UTC())
	}
	return len(items), nil
}

func (s *NotificationService) requireOrganizationMember(userID int64, organizationID *int64) error {
	if organizationID == nil {
		return nil
	}
	if s.orgs == nil {
		return ErrNotificationForbidden
	}
	if _, err := s.orgs.GetMember(*organizationID, userID); err != nil {
		return ErrNotificationForbidden
	}
	return nil
}

func defaultNotificationPreference(userID int64, organizationID *int64) model.NotificationPreference {
	return model.NotificationPreference{
		UserID: userID, OrganizationID: organizationID,
		InAppEnabled: true, EmailEnabled: true, PushEnabled: false, WebhookEnabled: false,
		Digest: model.NotificationDigestImmediate, Timezone: "UTC", Locale: "en",
		MutedEventTypes: []string{}, UpdatedAt: time.Now().UTC(),
	}
}

func normalizeQuietHour(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	for _, layout := range []string{"15:04", "15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("15:04:05"), nil
		}
	}
	return "", ErrInvalidNotificationPreference
}

func scheduleNotificationDelivery(now time.Time, pref model.NotificationPreference) time.Time {
	loc, err := time.LoadLocation(pref.Timezone)
	if err != nil {
		loc = time.UTC
	}
	local := now.In(loc)
	switch pref.Digest {
	case model.NotificationDigestHourly:
		local = local.Truncate(time.Hour).Add(time.Hour)
	case model.NotificationDigestDaily:
		local = time.Date(local.Year(), local.Month(), local.Day(), 9, 0, 0, 0, loc)
		if !local.After(now.In(loc)) {
			local = local.Add(24 * time.Hour)
		}
	}
	local = moveOutsideQuietHours(local, pref.QuietHoursStart, pref.QuietHoursEnd)
	return local.UTC()
}

func moveOutsideQuietHours(local time.Time, startRaw, endRaw string) time.Time {
	if startRaw == "" || endRaw == "" {
		return local
	}
	start, err1 := time.Parse("15:04:05", startRaw)
	end, err2 := time.Parse("15:04:05", endRaw)
	if err1 != nil || err2 != nil {
		return local
	}
	startAt := time.Date(local.Year(), local.Month(), local.Day(), start.Hour(), start.Minute(), start.Second(), 0, local.Location())
	endAt := time.Date(local.Year(), local.Month(), local.Day(), end.Hour(), end.Minute(), end.Second(), 0, local.Location())
	if endAt.After(startAt) {
		if !local.Before(startAt) && local.Before(endAt) {
			return endAt
		}
		return local
	}
	if !local.Before(startAt) {
		return endAt.Add(24 * time.Hour)
	}
	if local.Before(endAt) {
		return endAt
	}
	return local
}

func notificationBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	return time.Duration(1<<uint(attempt-1)) * time.Minute
}

func notificationDedupKey(base string, userID int64) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	return base + ":user:" + strconv.FormatInt(userID, 10)
}

func stringSliceContains(items []string, value string) bool {
	for _, item := range items {
		if item == value || item == "*" {
			return true
		}
	}
	return false
}

func dedupeInt64(values []int64) []int64 {
	seen := map[int64]bool{}
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func renderNotificationTemplate(template string, data map[string]any) string {
	result := template
	for key, value := range data {
		result = strings.ReplaceAll(result, "{{"+key+"}}", fmt.Sprint(value))
	}
	return result
}

func cloneNotificationMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
