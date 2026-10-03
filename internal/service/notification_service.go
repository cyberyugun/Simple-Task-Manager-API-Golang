package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	eventdelivery "go-simple-task-api/internal/events"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrInvalidNotificationPreference = errors.New("invalid notification preference")
	ErrInvalidNotificationEndpoint   = errors.New("invalid notification endpoint")
	ErrInvalidNotificationTemplate   = errors.New("invalid notification template")
	ErrNotificationForbidden         = errors.New("notification action is forbidden")
)

type NotificationConfig struct {
	EmailProviderURL string
	PushProviderURL  string
	ProviderToken    string
	HTTPTimeout      time.Duration
	AllowInsecure    bool
	ReminderHorizon time.Duration
}

type NotificationService struct {
	repo       repository.NotificationRepository
	users      repository.UserRepository
	tasks      repository.TaskRepository
	collab     repository.TaskCollaborationRepository
	workspaces repository.WorkspaceRepository
	orgs       repository.OrganizationRepository
	config     NotificationConfig
}

func NewNotificationService(
	repo repository.NotificationRepository,
	users repository.UserRepository,
	tasks repository.TaskRepository,
	collab repository.TaskCollaborationRepository,
	workspaces repository.WorkspaceRepository,
	orgs repository.OrganizationRepository,
	config NotificationConfig,
) *NotificationService {
	if config.HTTPTimeout <= 0 {
		config.HTTPTimeout = 10 * time.Second
	}
	if config.ReminderHorizon <= 0 {
		config.ReminderHorizon = 24 * time.Hour
	}
	return &NotificationService{
		repo: repo, users: users, tasks: tasks, collab: collab,
		workspaces: workspaces, orgs: orgs, config: config,
	}
}

func (s *NotificationService) Preferences(userID int64) (model.NotificationPreference, error) {
	return s.repo.GetPreference(userID)
}

func (s *NotificationService) UpdatePreferences(userID int64, req model.UpdateNotificationPreferenceRequest) (model.NotificationPreference, error) {
	current, err := s.repo.GetPreference(userID)
	if err != nil {
		return model.NotificationPreference{}, err
	}
	locale := strings.ToLower(strings.TrimSpace(req.Locale))
	if locale == "" {
		locale = current.Locale
	}
	timezone := strings.TrimSpace(req.Timezone)
	if timezone == "" {
		timezone = current.Timezone
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return model.NotificationPreference{}, ErrInvalidNotificationPreference
	}
	quietStart := strings.TrimSpace(req.QuietStart)
	quietEnd := strings.TrimSpace(req.QuietEnd)
	if quietStart == "" {
		quietStart = current.QuietStart
	}
	if quietEnd == "" {
		quietEnd = current.QuietEnd
	}
	if quietStart == "" {
		quietStart = "22:00"
	}
	if quietEnd == "" {
		quietEnd = "07:00"
	}
	if _, _, err := parseClock(quietStart); err != nil {
		return model.NotificationPreference{}, ErrInvalidNotificationPreference
	}
	if _, _, err := parseClock(quietEnd); err != nil {
		return model.NotificationPreference{}, ErrInvalidNotificationPreference
	}
	digest := strings.ToLower(strings.TrimSpace(req.DigestFrequency))
	if digest == "" {
		digest = current.DigestFrequency
	}
	if digest != model.NotificationDigestOff && digest != model.NotificationDigestDaily && digest != model.NotificationDigestWeekly {
		return model.NotificationPreference{}, ErrInvalidNotificationPreference
	}
	digestHour := req.DigestHour
	if digestHour < 0 || digestHour > 23 {
		return model.NotificationPreference{}, ErrInvalidNotificationPreference
	}
	channels := mergeNotificationBoolMap(current.Channels, req.Channels)
	events := mergeNotificationBoolMap(current.Events, req.Events)
	for key := range channels {
		if !validNotificationChannel(key) {
			return model.NotificationPreference{}, ErrInvalidNotificationPreference
		}
	}
	now := time.Now().UTC()
	return s.repo.UpsertPreference(model.NotificationPreference{
		UserID: userID, Locale: locale, Timezone: timezone,
		QuietHoursEnabled: req.QuietHoursEnabled, QuietStart: quietStart, QuietEnd: quietEnd,
		DigestFrequency: digest, DigestHour: digestHour,
		Channels: channels, Events: events, CreatedAt: current.CreatedAt, UpdatedAt: now,
	})
}

func (s *NotificationService) Endpoints(userID int64) ([]model.NotificationEndpoint, error) {
	return s.repo.ListEndpoints(userID)
}

func (s *NotificationService) CreateEndpoint(userID int64, req model.CreateNotificationEndpointRequest) (model.NotificationEndpoint, error) {
	channel := strings.ToLower(strings.TrimSpace(req.Channel))
	address := strings.TrimSpace(req.Address)
	secret := strings.TrimSpace(req.Secret)
	switch channel {
	case model.NotificationChannelEmail:
		parsed, err := mail.ParseAddress(address)
		if err != nil || !strings.Contains(parsed.Address, "@") {
			return model.NotificationEndpoint{}, ErrInvalidNotificationEndpoint
		}
		address = parsed.Address
		secret = ""
	case model.NotificationChannelPush:
		if address == "" || len(address) > 4096 {
			return model.NotificationEndpoint{}, ErrInvalidNotificationEndpoint
		}
		secret = ""
	case model.NotificationChannelWebhook:
		endpoint, err := url.Parse(address)
		if err != nil || eventdelivery.ValidateWebhookURL(endpoint, s.config.AllowInsecure) != nil {
			return model.NotificationEndpoint{}, ErrInvalidNotificationEndpoint
		}
		if secret == "" {
			raw := make([]byte, 32)
			if _, err := rand.Read(raw); err != nil {
				return model.NotificationEndpoint{}, err
			}
			secret = hex.EncodeToString(raw)
		}
		if len(secret) < 32 {
			return model.NotificationEndpoint{}, ErrInvalidNotificationEndpoint
		}
	default:
		return model.NotificationEndpoint{}, ErrInvalidNotificationEndpoint
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateEndpoint(model.NotificationEndpoint{
		UserID: userID, Channel: channel, Address: address, Secret: secret,
		Active: true, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return model.NotificationEndpoint{}, err
	}
	if channel != model.NotificationChannelWebhook {
		item.Secret = ""
	}
	return item, nil
}

func (s *NotificationService) DeleteEndpoint(userID, endpointID int64) error {
	return s.repo.DeleteEndpoint(userID, endpointID, time.Now().UTC())
}

func (s *NotificationService) Notifications(userID int64, unreadOnly bool, limit int) (model.NotificationList, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	items, unread, err := s.repo.ListNotifications(userID, unreadOnly, limit)
	if err != nil {
		return model.NotificationList{}, err
	}
	return model.NotificationList{Items: items, Unread: unread}, nil
}

func (s *NotificationService) MarkRead(userID, notificationID int64) (model.Notification, error) {
	return s.repo.MarkNotificationRead(userID, notificationID, time.Now().UTC())
}

func (s *NotificationService) MarkAllRead(userID int64) (int64, error) {
	return s.repo.MarkAllNotificationsRead(userID, time.Now().UTC())
}

func (s *NotificationService) Deliveries(userID int64, limit int) ([]model.NotificationDelivery, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.ListDeliveries(userID, limit)
}

func (s *NotificationService) RetryDelivery(userID, deliveryID int64) (model.NotificationDelivery, error) {
	return s.repo.RetryDelivery(userID, deliveryID, time.Now().UTC())
}

func (s *NotificationService) Templates(actorUserID, organizationID int64) ([]model.NotificationTemplate, error) {
	if _, err := s.requireNotificationAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListTemplates(organizationID)
}

func (s *NotificationService) CreateTemplate(actorUserID, organizationID int64, req model.CreateNotificationTemplateRequest) (model.NotificationTemplate, error) {
	if _, err := s.requireNotificationAdmin(actorUserID, organizationID); err != nil {
		return model.NotificationTemplate{}, err
	}
	key := strings.ToLower(strings.TrimSpace(req.Key))
	locale := strings.ToLower(strings.TrimSpace(req.Locale))
	channel := strings.ToLower(strings.TrimSpace(req.Channel))
	subject := strings.TrimSpace(req.Subject)
	body := strings.TrimSpace(req.Body)
	if !validNotificationTemplateKey(key) || locale == "" || len(locale) > 20 ||
		!validNotificationChannel(channel) || body == "" || len(body) > 50000 || len(subject) > 1000 {
		return model.NotificationTemplate{}, ErrInvalidNotificationTemplate
	}
	orgID := organizationID
	version, err := s.repo.NextTemplateVersion(&orgID, key, locale, channel)
	if err != nil {
		return model.NotificationTemplate{}, err
	}
	now := time.Now().UTC()
	actor := actorUserID
	return s.repo.CreateTemplate(model.NotificationTemplate{
		OrganizationID: &orgID, Key: key, Locale: locale, Channel: channel, Version: version,
		Status: model.NotificationTemplateDraft, Subject: subject, Body: body,
		CreatedByUserID: &actor, CreatedAt: now, UpdatedAt: now,
	})
}

func (s *NotificationService) PublishTemplate(actorUserID, organizationID, templateID int64) (model.NotificationTemplate, error) {
	if _, err := s.requireNotificationAdmin(actorUserID, organizationID); err != nil {
		return model.NotificationTemplate{}, err
	}
	orgID := organizationID
	item, err := s.repo.GetTemplate(&orgID, templateID)
	if err != nil {
		return model.NotificationTemplate{}, err
	}
	if item.Status != model.NotificationTemplateDraft {
		return model.NotificationTemplate{}, ErrInvalidNotificationTemplate
	}
	all, err := s.repo.ListTemplates(organizationID)
	if err != nil {
		return model.NotificationTemplate{}, err
	}
	now := time.Now().UTC()
	for _, existing := range all {
		if existing.ID == item.ID || existing.Status != model.NotificationTemplatePublished ||
			existing.Key != item.Key || existing.Locale != item.Locale || existing.Channel != item.Channel {
			continue
		}
		existing.Status = model.NotificationTemplateArchived
		existing.UpdatedAt = now
		if _, err := s.repo.UpdateTemplate(existing); err != nil {
			return model.NotificationTemplate{}, err
		}
	}
	actor := actorUserID
	item.Status = model.NotificationTemplatePublished
	item.PublishedByID = &actor
	item.PublishedAt = &now
	item.UpdatedAt = now
	return s.repo.UpdateTemplate(item)
}

func (s *NotificationService) Consume(_ context.Context, event model.DomainEvent) error {
	switch event.EventType {
	case model.EventTaskAssigned:
		payload := map[string]any{}
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		userID, ok := notificationInt64(payload["user_id"])
		if !ok {
			return nil
		}
		taskID, _ := notificationInt64(payload["task_id"])
		return s.notifyTaskRelation(event, userID, taskID, model.NotificationEventTaskAssigned, "task.assigned")
	case model.EventTaskWatcherAdded:
		payload := map[string]any{}
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		userID, ok := notificationInt64(payload["user_id"])
		if !ok {
			return nil
		}
		taskID, _ := notificationInt64(payload["task_id"])
		return s.notifyTaskRelation(event, userID, taskID, model.NotificationEventTaskWatcherAdded, "task.watcher_added")
	case model.EventTaskCommentCreated, model.EventTaskCommentUpdated:
		var comment model.TaskComment
		if err := json.Unmarshal(event.Data, &comment); err != nil {
			return err
		}
		return s.notifyCommentMentions(event, comment)
	default:
		return nil
	}
}

func (s *NotificationService) notifyTaskRelation(event model.DomainEvent, userID, taskID int64, eventType, templateKey string) error {
	taskTitle := "Task"
	if taskID > 0 {
		if task, err := s.tasks.FindByID(event.WorkspaceID, taskID); err == nil {
			taskTitle = task.Title
		}
	}
	return s.createNotification(model.NotificationCreate{
		WorkspaceID: notificationInt64Ptr(event.WorkspaceID),
		UserID: userID, EventType: eventType, TemplateKey: templateKey,
		Data: map[string]any{"task_id": taskID, "task_title": taskTitle},
		DedupeKey: fmt.Sprintf("event:%s:%s:%d", event.EventKey, eventType, userID),
	})
}

var notificationMentionPattern = regexp.MustCompile(`(?:<@|@\{|@)([0-9]{1,18})(?:>|\})?`)

func (s *NotificationService) notifyCommentMentions(event model.DomainEvent, comment model.TaskComment) error {
	matches := notificationMentionPattern.FindAllStringSubmatch(comment.Body, -1)
	seen := map[int64]bool{}
	taskTitle := "Task"
	if task, err := s.tasks.FindByID(event.WorkspaceID, comment.TaskID); err == nil {
		taskTitle = task.Title
	}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		userID, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || userID <= 0 || userID == comment.UserID || seen[userID] {
			continue
		}
		if _, err := s.workspaces.ResolveAccess(userID, event.WorkspaceID, time.Now().UTC()); err != nil {
			continue
		}
		seen[userID] = true
		if err := s.createNotification(model.NotificationCreate{
			WorkspaceID: notificationInt64Ptr(event.WorkspaceID),
			UserID: userID, EventType: model.NotificationEventTaskMention, TemplateKey: "task.mention",
			Data: map[string]any{
				"task_id": comment.TaskID, "task_title": taskTitle,
				"comment_id": comment.ID, "comment_body": comment.Body,
			},
			DedupeKey: fmt.Sprintf("event:%s:mention:%d", event.EventKey, userID),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *NotificationService) GenerateReminders(now time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 500
	}
	horizon := now.Add(s.config.ReminderHorizon)
	candidates, err := s.repo.ListReminderCandidates(now, horizon, limit)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, candidate := range candidates {
		pref, err := s.repo.GetPreference(candidate.UserID)
		if err != nil {
			return created, err
		}
		localDate := notificationLocalDate(now, pref.Timezone)
		eventType := model.NotificationEventTaskDueSoon
		templateKey := "task.due_soon"
		dedupe := fmt.Sprintf("task-due:%d:%d:%d", candidate.TaskID, candidate.UserID, candidate.DueAt.Unix())
		if !candidate.DueAt.After(now) {
			eventType = model.NotificationEventTaskOverdue
			templateKey = "task.overdue"
			dedupe = fmt.Sprintf("task-overdue:%d:%d:%s", candidate.TaskID, candidate.UserID, localDate)
		}
		wasCreated, err := s.createNotificationWithResult(model.NotificationCreate{
			WorkspaceID: notificationInt64Ptr(candidate.WorkspaceID),
			UserID: candidate.UserID, EventType: eventType, TemplateKey: templateKey,
			Data: map[string]any{
				"task_id": candidate.TaskID, "task_title": candidate.TaskTitle,
				"due_at": candidate.DueAt.UTC().Format(time.RFC3339),
			},
			DedupeKey: dedupe,
		})
		if err != nil {
			return created, err
		}
		if wasCreated {
			created++
		}
	}
	return created, nil
}

func (s *NotificationService) GenerateApprovalNotifications(limit int) (int, error) {
	if limit <= 0 {
		limit = 500
	}
	candidates, err := s.repo.ListApprovalCandidates(limit)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, candidate := range candidates {
		orgID := candidate.OrganizationID
		wasCreated, err := s.createNotificationWithResult(model.NotificationCreate{
			OrganizationID: &orgID,
			UserID: candidate.UserID, EventType: model.NotificationEventWorkflowApproval,
			TemplateKey: "workflow.approval",
			Data: map[string]any{
				"workflow_id": candidate.WorkflowID, "workflow_name": candidate.WorkflowName,
				"execution_id": candidate.ExecutionID, "approval_id": candidate.ApprovalID,
				"node_id": candidate.NodeID,
			},
			DedupeKey: fmt.Sprintf("workflow-approval:%d:%d", candidate.ApprovalID, candidate.UserID),
		})
		if err != nil {
			return created, err
		}
		if wasCreated {
			created++
		}
	}
	return created, nil
}

func (s *NotificationService) GenerateDigests(now time.Time) (int, error) {
	preferences, err := s.repo.ListDigestPreferences()
	if err != nil {
		return 0, err
	}
	created := 0
	for _, pref := range preferences {
		location, err := time.LoadLocation(pref.Timezone)
		if err != nil {
			continue
		}
		local := now.In(location)
		if local.Hour() != pref.DigestHour {
			continue
		}
		var since time.Time
		var dedupe string
		switch pref.DigestFrequency {
		case model.NotificationDigestDaily:
			start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
			since = start.Add(-24 * time.Hour).UTC()
			dedupe = fmt.Sprintf("digest:daily:%d:%s", pref.UserID, local.Format("2006-01-02"))
		case model.NotificationDigestWeekly:
			if local.Weekday() != time.Monday {
				continue
			}
			start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
			since = start.Add(-7 * 24 * time.Hour).UTC()
			year, week := local.ISOWeek()
			dedupe = fmt.Sprintf("digest:weekly:%d:%04d-%02d", pref.UserID, year, week)
		default:
			continue
		}
		count, err := s.repo.UnreadCountSince(pref.UserID, since)
		if err != nil {
			return created, err
		}
		if count == 0 {
			continue
		}
		wasCreated, err := s.createNotificationWithResult(model.NotificationCreate{
			UserID: pref.UserID, EventType: model.NotificationEventDigestSummary,
			TemplateKey: "digest.summary",
			Data: map[string]any{"count": count, "frequency": pref.DigestFrequency},
			DedupeKey: dedupe,
		})
		if err != nil {
			return created, err
		}
		if wasCreated {
			created++
		}
	}
	return created, nil
}

func (s *NotificationService) createNotification(input model.NotificationCreate) error {
	_, err := s.createNotificationWithResult(input)
	return err
}

func (s *NotificationService) createNotificationWithResult(input model.NotificationCreate) (bool, error) {
	preference, err := s.repo.GetPreference(input.UserID)
	if err != nil {
		return false, err
	}
	if enabled, exists := preference.Events[input.EventType]; exists && !enabled {
		return false, nil
	}
	if input.OrganizationID == nil && input.WorkspaceID != nil {
		input.OrganizationID, err = s.repo.WorkspaceOrganization(*input.WorkspaceID)
		if err != nil {
			return false, err
		}
	}
	subject, body, err := s.render(input.OrganizationID, input.TemplateKey, preference.Locale, model.NotificationChannelInApp, input.Data)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	item := model.Notification{
		OrganizationID: input.OrganizationID, WorkspaceID: input.WorkspaceID,
		UserID: input.UserID, EventType: input.EventType, TemplateKey: input.TemplateKey,
		Title: subject, Body: body, Data: cloneNotificationData(input.Data),
		DedupeKey: input.DedupeKey, CreatedAt: now, UpdatedAt: now,
	}
	if enabled, ok := preference.Channels[model.NotificationChannelInApp]; ok && !enabled {
		item.ReadAt = &now
	}
	notification, created, err := s.repo.CreateNotification(item)
	if err != nil || !created {
		return created, err
	}
	if err := s.enqueueExternalDeliveries(notification, preference, now); err != nil {
		return true, err
	}
	return true, nil
}

func (s *NotificationService) enqueueExternalDeliveries(notification model.Notification, preference model.NotificationPreference, now time.Time) error {
	availableAt := nextNotificationAllowedTime(now, preference)
	user, err := s.users.FindByID(notification.UserID)
	if err != nil {
		return err
	}
	if preference.Channels[model.NotificationChannelEmail] && strings.TrimSpace(s.config.EmailProviderURL) != "" {
		destinations := []model.NotificationEndpoint{}
		endpoints, err := s.repo.ListEndpoints(notification.UserID)
		if err != nil {
			return err
		}
		for _, endpoint := range endpoints {
			if endpoint.Channel == model.NotificationChannelEmail && endpoint.Active {
				destinations = append(destinations, endpoint)
			}
		}
		if len(destinations) == 0 && user.Email != "" {
			destinations = append(destinations, model.NotificationEndpoint{Address: user.Email, Channel: model.NotificationChannelEmail, Active: true})
		}
		for _, endpoint := range destinations {
			if err := s.createDelivery(notification, endpoint, availableAt); err != nil {
				return err
			}
		}
	}
	endpoints, err := s.repo.ListEndpoints(notification.UserID)
	if err != nil {
		return err
	}
	for _, endpoint := range endpoints {
		if !endpoint.Active || !preference.Channels[endpoint.Channel] {
			continue
		}
		if endpoint.Channel == model.NotificationChannelPush && strings.TrimSpace(s.config.PushProviderURL) == "" {
			continue
		}
		if endpoint.Channel != model.NotificationChannelPush && endpoint.Channel != model.NotificationChannelWebhook {
			continue
		}
		if err := s.createDelivery(notification, endpoint, availableAt); err != nil {
			return err
		}
	}
	return nil
}

func (s *NotificationService) createDelivery(notification model.Notification, endpoint model.NotificationEndpoint, availableAt time.Time) error {
	now := time.Now().UTC()
	var endpointID *int64
	if endpoint.ID > 0 {
		id := endpoint.ID
		endpointID = &id
	}
	_, err := s.repo.CreateDelivery(model.NotificationDelivery{
		NotificationID: notification.ID, UserID: notification.UserID,
		Channel: endpoint.Channel, Destination: endpoint.Address, EndpointID: endpointID,
		Status: model.NotificationDeliveryPending, MaxAttempts: 8,
		AvailableAt: availableAt, CreatedAt: now, UpdatedAt: now,
	})
	return err
}

func (s *NotificationService) render(organizationID *int64, key, locale, channel string, data map[string]any) (string, string, error) {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if locale == "" {
		locale = "en"
	}
	template, err := s.repo.GetPublishedTemplate(organizationID, key, locale, channel)
	if err != nil && locale != "en" {
		template, err = s.repo.GetPublishedTemplate(organizationID, key, "en", channel)
	}
	if err == nil {
		return renderNotificationText(template.Subject, data), renderNotificationText(template.Body, data), nil
	}
	if !errors.Is(err, repository.ErrNotificationTemplateNotFound) {
		return "", "", err
	}
	subject, body := defaultNotificationTemplate(key, locale, channel)
	if body == "" && locale != "en" {
		subject, body = defaultNotificationTemplate(key, "en", channel)
	}
	if body == "" {
		return "", "", repository.ErrNotificationTemplateNotFound
	}
	return renderNotificationText(subject, data), renderNotificationText(body, data), nil
}

func (s *NotificationService) requireNotificationAdmin(userID, organizationID int64) (model.OrganizationMember, error) {
	member, err := s.orgs.GetMember(organizationID, userID)
	if err != nil {
		return model.OrganizationMember{}, ErrNotificationForbidden
	}
	switch member.Role {
	case model.OrganizationRoleOwner, model.OrganizationRoleAdmin, model.OrganizationRoleDelegatedAdmin:
		return member, nil
	default:
		return model.OrganizationMember{}, ErrNotificationForbidden
	}
}

func validNotificationChannel(channel string) bool {
	switch channel {
	case model.NotificationChannelInApp, model.NotificationChannelEmail,
		model.NotificationChannelPush, model.NotificationChannelWebhook:
		return true
	default:
		return false
	}
}

func validNotificationTemplateKey(key string) bool {
	if key == "" || len(key) > 120 {
		return false
	}
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func mergeNotificationBoolMap(current, updates map[string]bool) map[string]bool {
	result := map[string]bool{}
	for key, value := range current {
		result[key] = value
	}
	for key, value := range updates {
		result[strings.TrimSpace(key)] = value
	}
	return result
}

func parseClock(value string) (int, int, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, ErrInvalidNotificationPreference
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, ErrInvalidNotificationPreference
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, ErrInvalidNotificationPreference
	}
	return hour, minute, nil
}

func nextNotificationAllowedTime(now time.Time, preference model.NotificationPreference) time.Time {
	if !preference.QuietHoursEnabled {
		return now
	}
	location, err := time.LoadLocation(preference.Timezone)
	if err != nil {
		return now
	}
	startHour, startMinute, err := parseClock(preference.QuietStart)
	if err != nil {
		return now
	}
	endHour, endMinute, err := parseClock(preference.QuietEnd)
	if err != nil {
		return now
	}
	local := now.In(location)
	start := time.Date(local.Year(), local.Month(), local.Day(), startHour, startMinute, 0, 0, location)
	end := time.Date(local.Year(), local.Month(), local.Day(), endHour, endMinute, 0, 0, location)
	if start.Before(end) {
		if !local.Before(start) && local.Before(end) {
			return end.UTC()
		}
		return now
	}
	if !local.Before(start) {
		end = end.Add(24 * time.Hour)
		return end.UTC()
	}
	if local.Before(end) {
		return end.UTC()
	}
	return now
}

func notificationLocalDate(now time.Time, timezone string) string {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return now.UTC().Format("2006-01-02")
	}
	return now.In(location).Format("2006-01-02")
}

func renderNotificationText(template string, data map[string]any) string {
	result := template
	for key, value := range data {
		result = strings.ReplaceAll(result, "{{"+key+"}}", fmt.Sprint(value))
	}
	return result
}

func defaultNotificationTemplate(key, locale, channel string) (string, string) {
	indonesian := strings.HasPrefix(locale, "id")
	switch key {
	case "task.assigned":
		if indonesian {
			return "Tugas baru untuk Anda", "Anda ditugaskan ke {{task_title}}."
		}
		return "Task assigned", "You were assigned to {{task_title}}."
	case "task.watcher_added":
		if indonesian {
			return "Anda mengikuti tugas", "Anda sekarang mengikuti {{task_title}}."
		}
		return "Watching task", "You are now watching {{task_title}}."
	case "task.mention":
		if indonesian {
			return "Anda disebut di komentar", "Anda disebut pada {{task_title}}: {{comment_body}}"
		}
		return "You were mentioned", "You were mentioned on {{task_title}}: {{comment_body}}"
	case "task.due_soon":
		if indonesian {
			return "Tugas segera jatuh tempo", "{{task_title}} jatuh tempo pada {{due_at}}."
		}
		return "Task due soon", "{{task_title}} is due at {{due_at}}."
	case "task.overdue":
		if indonesian {
			return "Tugas terlambat", "{{task_title}} sudah melewati jatuh tempo {{due_at}}."
		}
		return "Task overdue", "{{task_title}} is overdue since {{due_at}}."
	case "workflow.approval":
		if indonesian {
			return "Persetujuan workflow diperlukan", "{{workflow_name}} menunggu persetujuan pada node {{node_id}}."
		}
		return "Workflow approval required", "{{workflow_name}} is waiting for approval at node {{node_id}}."
	case "digest.summary":
		if indonesian {
			return "Ringkasan notifikasi", "Anda memiliki {{count}} notifikasi belum dibaca pada ringkasan {{frequency}}."
		}
		return "Notification digest", "You have {{count}} unread notifications in your {{frequency}} digest."
	default:
		if channel == model.NotificationChannelWebhook {
			return key, key
		}
		return "", ""
	}
}

func cloneNotificationData(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	raw, _ := json.Marshal(input)
	output := map[string]any{}
	_ = json.Unmarshal(raw, &output)
	return output
}

func notificationInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		value := int64(typed)
		return value, float64(value) == typed
	case json.Number:
		value, err := typed.Int64()
		return value, err == nil
	case string:
		value, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return value, err == nil
	default:
		return 0, false
	}
}

func notificationInt64Ptr(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}
