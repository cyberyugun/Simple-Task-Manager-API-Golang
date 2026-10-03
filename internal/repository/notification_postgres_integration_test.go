//go:build integration

package repository_test

import (
	"testing"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestIntegrationPostgresNotificationRepository(t *testing.T) {
	databaseURL := getenvRequired(t, "TEST_DATABASE_URL")
	db, err := appdb.OpenPostgres(databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgres() error = %v", err)
	}
	if err := resetDatabase(db); err != nil {
		_ = db.Close()
		t.Fatalf("reset database: %v", err)
	}
	t.Cleanup(func() {
		if err := resetDatabase(db); err != nil {
			t.Errorf("cleanup database: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	applyMigrations(t, db)

	users := repository.NewPostgresUserRepository(db)
	workspaces := repository.NewPostgresWorkspaceRepository(db)
	orgs := repository.NewPostgresOrganizationRepository(db)
	tasks := repository.NewPostgresTaskRepository(db)
	collab := repository.NewPostgresTaskCollaborationRepository(db)
	notifications := repository.NewPostgresNotificationRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	owner, err := users.Create(model.User{
		Name: "Notification Owner", Email: "notification-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	member, err := users.Create(model.User{
		Name: "Notification Member", Email: "notification-member@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaces.Create(owner.ID, "Notification Workspace", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.AddMember(workspace.ID, member.ID, model.WorkspaceRoleMember, now); err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Notification Org", Status: model.OrganizationStatusActive, OwnerUserID: owner.ID,
		MaxMembers: 100, MaxWorkspaces: 20, CreatedByUserID: owner.ID,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orgs.AttachWorkspace(model.OrganizationWorkspace{
		OrganizationID: org.ID, WorkspaceID: workspace.ID, AttachedByID: owner.ID, AttachedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	pref, err := notifications.GetPreference(member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pref.Locale != "en" || pref.Timezone != "UTC" || !pref.Channels[model.NotificationChannelInApp] {
		t.Fatalf("unexpected default preference: %+v", pref)
	}
	pref.Locale = "id"
	pref.Timezone = "Asia/Jakarta"
	pref.QuietHoursEnabled = true
	pref.QuietStart = "22:00"
	pref.QuietEnd = "07:00"
	pref.DigestFrequency = model.NotificationDigestDaily
	pref.DigestHour = 8
	pref.Channels[model.NotificationChannelEmail] = true
	pref.UpdatedAt = now.Add(time.Second)
	updatedPref, err := notifications.UpsertPreference(pref)
	if err != nil {
		t.Fatal(err)
	}
	if updatedPref.Locale != "id" || !updatedPref.Channels[model.NotificationChannelEmail] {
		t.Fatalf("updated preference=%+v", updatedPref)
	}

	endpoint, err := notifications.CreateEndpoint(model.NotificationEndpoint{
		UserID: member.ID, Channel: model.NotificationChannelEmail, Address: member.Email,
		Active: true, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || endpoint.ID == 0 {
		t.Fatalf("endpoint=%+v err=%v", endpoint, err)
	}
	endpoints, err := notifications.ListEndpoints(member.ID)
	if err != nil || len(endpoints) != 1 || endpoints[0].Secret != "" {
		t.Fatalf("endpoints=%+v err=%v", endpoints, err)
	}

	orgID := org.ID
	version, err := notifications.NextTemplateVersion(&orgID, "task.assigned", "id", model.NotificationChannelEmail)
	if err != nil || version != 1 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	template, err := notifications.CreateTemplate(model.NotificationTemplate{
		OrganizationID: &orgID, Key: "task.assigned", Locale: "id",
		Channel: model.NotificationChannelEmail, Version: version,
		Status: model.NotificationTemplatePublished, Subject: "Tugas {{task_title}}",
		Body: "Anda mendapat tugas {{task_title}}", CreatedByUserID: &owner.ID,
		PublishedByID: &owner.ID, PublishedAt: &now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || template.ID == 0 {
		t.Fatalf("template=%+v err=%v", template, err)
	}
	published, err := notifications.GetPublishedTemplate(&orgID, "task.assigned", "id", model.NotificationChannelEmail)
	if err != nil || published.ID != template.ID {
		t.Fatalf("published=%+v err=%v", published, err)
	}

	notification, created, err := notifications.CreateNotification(model.Notification{
		OrganizationID: &orgID, WorkspaceID: &workspace.ID, UserID: member.ID,
		EventType: model.NotificationEventTaskAssigned, TemplateKey: "task.assigned",
		Title: "Tugas baru", Body: "Anda mendapat tugas", Data: map[string]any{"task_title": "Phase 35"},
		DedupeKey: "event:phase35:member", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || !created || notification.ID == 0 {
		t.Fatalf("notification=%+v created=%v err=%v", notification, created, err)
	}
	duplicate, created, err := notifications.CreateNotification(model.Notification{
		OrganizationID: &orgID, WorkspaceID: &workspace.ID, UserID: member.ID,
		EventType: model.NotificationEventTaskAssigned, TemplateKey: "task.assigned",
		Title: "duplicate", Body: "duplicate", Data: map[string]any{},
		DedupeKey: "event:phase35:member", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || created || duplicate.ID != notification.ID {
		t.Fatalf("duplicate=%+v created=%v err=%v", duplicate, created, err)
	}

	delivery, err := notifications.CreateDelivery(model.NotificationDelivery{
		NotificationID: notification.ID, UserID: member.ID, Channel: model.NotificationChannelEmail,
		Destination: member.Email, EndpointID: &endpoint.ID, Status: model.NotificationDeliveryPending,
		MaxAttempts: 2, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || delivery.ID == 0 {
		t.Fatalf("delivery=%+v err=%v", delivery, err)
	}
	claimed, err := notifications.ClaimDeliveries("integration-worker", 10, now.Add(time.Second))
	if err != nil || len(claimed) != 1 || claimed[0].Delivery.ID != delivery.ID ||
		claimed[0].Notification.ID != notification.ID {
		t.Fatalf("claimed=%+v err=%v", claimed, err)
	}
	failed, err := notifications.MarkDeliveryFailed(delivery.ID, "provider unavailable", now.Add(2*time.Second))
	if err != nil || failed.Status != model.NotificationDeliveryRetry || failed.Attempts != 1 {
		t.Fatalf("failed=%+v err=%v", failed, err)
	}
	deadLetter, err := notifications.MarkDeliveryFailed(delivery.ID, "provider unavailable again", now.Add(3*time.Second))
	if err != nil || deadLetter.Status != model.NotificationDeliveryDeadLetter || deadLetter.Attempts != 2 {
		t.Fatalf("deadLetter=%+v err=%v", deadLetter, err)
	}
	replayed, err := notifications.RetryDelivery(member.ID, delivery.ID, now.Add(4*time.Second))
	if err != nil || replayed.Status != model.NotificationDeliveryPending || replayed.Attempts != 0 {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}
	sent, err := notifications.MarkDeliverySent(delivery.ID, now.Add(5*time.Second))
	if err != nil || sent.Status != model.NotificationDeliverySent || sent.SentAt == nil {
		t.Fatalf("sent=%+v err=%v", sent, err)
	}

	read, err := notifications.MarkNotificationRead(member.ID, notification.ID, now.Add(6*time.Second))
	if err != nil || read.ReadAt == nil {
		t.Fatalf("read=%+v err=%v", read, err)
	}

	dueAt := now.Add(30 * time.Minute)
	task, err := tasks.Create(model.Task{
		WorkspaceID: workspace.ID, UserID: owner.ID, Title: "Due soon",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityHigh, DueAt: &dueAt,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := collab.AddAssignee(workspace.ID, task.ID, member.ID, owner.ID, now); err != nil {
		t.Fatal(err)
	}
	candidates, err := notifications.ListReminderCandidates(now, now.Add(time.Hour), 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range candidates {
		if candidate.TaskID == task.ID && candidate.UserID == member.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected member reminder candidate: %+v", candidates)
	}

	resolvedOrgID, err := notifications.WorkspaceOrganization(workspace.ID)
	if err != nil || resolvedOrgID == nil || *resolvedOrgID != org.ID {
		t.Fatalf("workspace organization=%v err=%v", resolvedOrgID, err)
	}
}
