package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func newNotificationTestService(t *testing.T) (*NotificationService, repository.UserRepository, repository.TaskRepository, repository.WorkspaceRepository, repository.NotificationRepository, repository.OrganizationRepository) {
	t.Helper()
	users := repository.NewInMemoryUserRepository()
	tasks := repository.NewInMemoryTaskRepository()
	collab := repository.NewInMemoryTaskCollaborationRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	notifications := repository.NewInMemoryNotificationRepository()
	organizations := repository.NewInMemoryOrganizationRepository()
	return NewNotificationService(
		notifications, users, tasks, collab, workspaces, organizations,
		NotificationConfig{ReminderHorizon: 24 * time.Hour},
	), users, tasks, workspaces, notifications, organizations
}

func TestNotificationEventFanoutAndMentionDeduplication(t *testing.T) {
	svc, users, tasks, workspaces, _, _ := newNotificationTestService(t)
	now := time.Now().UTC()
	owner, err := users.Create(model.User{Name: "Owner", Email: "owner@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	member, err := users.Create(model.User{Name: "Member", Email: "member@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	access, err := workspaces.Create(owner.ID, "Team", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.AddMember(access.ID, member.ID, model.WorkspaceRoleMember, now); err != nil {
		t.Fatal(err)
	}
	task, err := tasks.Create(model.Task{
		WorkspaceID: access.ID, UserID: owner.ID, Title: "Ship Phase 35", Status: model.TaskStatusTodo,
		Priority: model.TaskPriorityHigh, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	assignedData, _ := json.Marshal(map[string]any{"task_id": task.ID, "user_id": member.ID})
	event := model.DomainEvent{
		EventKey: "evt-assigned-1", WorkspaceID: access.ID, EventType: model.EventTaskAssigned,
		AggregateType: "task", AggregateID: "1", SchemaVersion: 1, Data: assignedData, OccurredAt: now,
	}
	if err := svc.Consume(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := svc.Consume(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	list, err := svc.Notifications(member.ID, false, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].EventType != model.NotificationEventTaskAssigned {
		t.Fatalf("assigned notifications=%+v", list.Items)
	}

	comment := model.TaskComment{
		ID: 10, TaskID: task.ID, WorkspaceID: access.ID, UserID: owner.ID,
		Body:      "please review <@" + notificationTestInt64(member.ID) + "> and @{" + notificationTestInt64(member.ID) + "}",
		CreatedAt: now, UpdatedAt: now,
	}
	commentData, _ := json.Marshal(comment)
	if err := svc.Consume(context.Background(), model.DomainEvent{
		EventKey: "evt-comment-1", WorkspaceID: access.ID, EventType: model.EventTaskCommentCreated,
		AggregateType: "task", AggregateID: "1", SchemaVersion: 1, Data: commentData, OccurredAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	list, err = svc.Notifications(member.ID, false, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("expected assigned+single mention, got %d", len(list.Items))
	}
	if list.Items[0].EventType != model.NotificationEventTaskMention {
		t.Fatalf("latest event=%s", list.Items[0].EventType)
	}
}

func TestNotificationPreferenceQuietHoursAndChannelEnforcement(t *testing.T) {
	svc, users, _, _, repo, _ := newNotificationTestService(t)
	now := time.Now().UTC()
	user, err := users.Create(model.User{Name: "User", Email: "user@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	pref, err := svc.UpdatePreferences(user.ID, model.UpdateNotificationPreferenceRequest{
		Locale: "id", Timezone: "Asia/Jakarta", QuietHoursEnabled: true,
		QuietStart: "22:00", QuietEnd: "07:00", DigestFrequency: model.NotificationDigestDaily,
		DigestHour: 8, Channels: map[string]bool{
			model.NotificationChannelInApp:   true,
			model.NotificationChannelEmail:   true,
			model.NotificationChannelPush:    false,
			model.NotificationChannelWebhook: false,
		},
		Events: map[string]bool{model.NotificationEventTaskAssigned: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pref.Locale != "id" || pref.Timezone != "Asia/Jakarta" || !pref.Channels[model.NotificationChannelEmail] {
		t.Fatalf("preference=%+v", pref)
	}
	jakarta, _ := time.LoadLocation("Asia/Jakarta")
	quietNow := time.Date(2026, 10, 4, 23, 30, 0, 0, jakarta).UTC()
	allowed := nextNotificationAllowedTime(quietNow, pref).In(jakarta)
	if allowed.Hour() != 7 || allowed.Day() != 5 {
		t.Fatalf("quiet-hours resume=%s", allowed)
	}

	created, err := svc.createNotificationWithResult(model.NotificationCreate{
		UserID: user.ID, EventType: model.NotificationEventTaskAssigned, TemplateKey: "task.assigned",
		Data: map[string]any{"task_title": "Suppressed"}, DedupeKey: "suppressed-event",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("disabled event preference should suppress notification")
	}
	items, _, err := repo.ListNotifications(user.ID, false, 10)
	if err != nil || len(items) != 0 {
		t.Fatalf("notifications=%+v err=%v", items, err)
	}
}

func TestNotificationWebhookSigningSecretIsReturnedOnce(t *testing.T) {
	svc, users, _, _, _, _ := newNotificationTestService(t)
	now := time.Now().UTC()
	user, err := users.Create(model.User{Name: "Webhook User", Email: "webhook@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreateEndpoint(user.ID, model.CreateNotificationEndpointRequest{
		Channel: model.NotificationChannelWebhook,
		Address: "https://example.com/notifications",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.SigningSecret) != 64 || created.Secret != "" {
		t.Fatalf("created endpoint should expose one-time signing secret: %+v", created)
	}
	items, err := svc.Endpoints(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].SigningSecret != "" || items[0].Secret != "" {
		t.Fatalf("listed endpoints must not expose signing secrets: %+v", items)
	}
}

func TestNotificationTemplateVersioningPublish(t *testing.T) {
	svc, users, _, _, _, orgs := newNotificationTestService(t)
	now := time.Now().UTC()
	owner, err := users.Create(model.User{Name: "Owner", Email: "owner@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Org", Status: model.OrganizationStatusActive, OwnerUserID: owner.ID,
		MaxMembers: 100, MaxWorkspaces: 20, CreatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.CreateTemplate(owner.ID, org.ID, model.CreateNotificationTemplateRequest{
		Key: "task.assigned", Locale: "en", Channel: model.NotificationChannelEmail,
		Subject: "Assigned {{task_title}}", Body: "First {{task_title}}",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err = svc.PublishTemplate(owner.ID, org.ID, first.ID)
	if err != nil || first.Status != model.NotificationTemplatePublished {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := svc.CreateTemplate(owner.ID, org.ID, model.CreateNotificationTemplateRequest{
		Key: "task.assigned", Locale: "en", Channel: model.NotificationChannelEmail,
		Subject: "Assigned v2", Body: "Second {{task_title}}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Version != 2 {
		t.Fatalf("version=%d", second.Version)
	}
	if _, err := svc.PublishTemplate(owner.ID, org.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	items, err := svc.Templates(owner.ID, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	published := 0
	archived := 0
	for _, item := range items {
		if item.Status == model.NotificationTemplatePublished {
			published++
		}
		if item.Status == model.NotificationTemplateArchived {
			archived++
		}
	}
	if published != 1 || archived != 1 {
		t.Fatalf("published=%d archived=%d items=%+v", published, archived, items)
	}
}

func notificationTestInt64(value int64) string {
	return fmt.Sprint(value)
}
