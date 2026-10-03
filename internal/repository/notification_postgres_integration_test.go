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
		_ = db.Close()
	})
	applyMigrations(t, db)

	users := repository.NewPostgresUserRepository(db)
	workspaces := repository.NewPostgresWorkspaceRepository(db)
	orgs := repository.NewPostgresOrganizationRepository(db)
	notifications := repository.NewPostgresNotificationRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	user, err := users.Create(model.User{
		Name: "Notification Owner", Email: "notification-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaces.Create(user.ID, "Notification Workspace", now)
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Notification Org", Status: model.OrganizationStatusActive, OwnerUserID: user.ID,
		MaxMembers: 100, MaxWorkspaces: 20, CreatedByUserID: user.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	pref, err := notifications.UpsertPreference(model.NotificationPreference{
		UserID: user.ID, OrganizationID: &org.ID,
		InAppEnabled: true, EmailEnabled: true, PushEnabled: false, WebhookEnabled: false,
		Digest: model.NotificationDigestHourly, Timezone: "Asia/Jakarta", Locale: "id",
		QuietHoursStart: "23:00:00", QuietHoursEnd: "06:00:00",
		MutedEventTypes: []string{"task.comment.updated"}, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pref.Digest != model.NotificationDigestHourly || pref.Timezone != "Asia/Jakarta" {
		t.Fatalf("preference=%+v", pref)
	}
	loadedPref, err := notifications.GetPreference(user.ID, &org.ID)
	if err != nil || len(loadedPref.MutedEventTypes) != 1 {
		t.Fatalf("loaded preference=%+v err=%v", loadedPref, err)
	}

	workspaceID := workspace.ID
	notification, created, err := notifications.CreateNotification(model.Notification{
		UserID: user.ID, OrganizationID: &org.ID, WorkspaceID: &workspaceID,
		EventType: model.NotificationEventTaskAssigned, Title: "Assigned",
		Body: "You were assigned", Data: map[string]any{"task_id": float64(99)},
		DedupKey: "integration-dedup", CreatedAt: now,
	})
	if err != nil || !created || notification.ID == 0 {
		t.Fatalf("notification=%+v created=%v err=%v", notification, created, err)
	}
	duplicate, created, err := notifications.CreateNotification(model.Notification{
		UserID: user.ID, OrganizationID: &org.ID, WorkspaceID: &workspaceID,
		EventType: model.NotificationEventTaskAssigned, Title: "Duplicate",
		DedupKey: "integration-dedup", CreatedAt: now,
	})
	if err != nil || created || duplicate.ID != notification.ID {
		t.Fatalf("duplicate=%+v created=%v err=%v", duplicate, created, err)
	}

	delivery, err := notifications.CreateDelivery(model.NotificationDelivery{
		NotificationID: notification.ID, UserID: user.ID, Channel: model.NotificationChannelEmail,
		Destination: user.Email, Status: model.NotificationDeliveryPending,
		MaxAttempts: 5, ScheduledAt: now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || delivery.ID == 0 {
		t.Fatalf("delivery=%+v err=%v", delivery, err)
	}
	ready, err := notifications.ClaimReadyDeliveries(now.Add(time.Second), 10)
	if err != nil || len(ready) != 1 || ready[0].ID != delivery.ID {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
	sentAt := now.Add(time.Second)
	delivery.Status = model.NotificationDeliverySent
	delivery.Attempt = 1
	delivery.SentAt = &sentAt
	delivery.UpdatedAt = sentAt
	if _, err := notifications.UpdateDelivery(delivery); err != nil {
		t.Fatal(err)
	}

	var taskID int64
	dueAt := now.Add(2 * time.Hour)
	if err := db.QueryRow(`
		INSERT INTO tasks (
			title,description,completed,user_id,workspace_id,created_by_user_id,
			status,priority,due_at,created_at,updated_at
		) VALUES ('Due task','',FALSE,$1,$2,$1,'TODO','MEDIUM',$3,$4,$4)
		RETURNING id
	`, user.ID, workspace.ID, dueAt, now).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	candidates, err := notifications.ListReminderCandidates(now, now.Add(24*time.Hour), 100)
	if err != nil || len(candidates) == 0 {
		t.Fatalf("candidates=%+v err=%v", candidates, err)
	}
	found := false
	for _, candidate := range candidates {
		if candidate.TaskID == taskID && candidate.UserID == user.ID && candidate.Kind == "due_soon" {
			found = true
		}
	}
	if !found {
		t.Fatalf("due-soon candidate missing: %+v", candidates)
	}

	if _, err := notifications.MarkNotificationRead(user.ID, notification.ID, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	stats, err := notifications.Stats(user.ID, &org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 1 || stats.Unread != 0 || stats.ByStatus[model.NotificationDeliverySent] != 1 {
		t.Fatalf("stats=%+v", stats)
	}

	admins, err := notifications.ListOrganizationAdmins(org.ID)
	if err != nil || len(admins) != 1 || admins[0] != user.ID {
		t.Fatalf("admins=%v err=%v", admins, err)
	}
}
