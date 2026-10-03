package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

type notificationTestSender struct {
	channel string
	fail    bool
	calls   int
}

func (s *notificationTestSender) Channel() string { return s.channel }
func (s *notificationTestSender) Send(_ context.Context, _ model.Notification, _ model.NotificationDelivery) error {
	s.calls++
	if s.fail {
		return errors.New("delivery failed")
	}
	return nil
}

func TestNotificationPreferenceRoutingDedupAndDelivery(t *testing.T) {
	repo := repository.NewInMemoryNotificationRepository()
	users := repository.NewInMemoryUserRepository()
	orgs := repository.NewInMemoryOrganizationRepository()
	now := time.Now().UTC()

	user, err := users.Create(model.User{Name: "Notify User", Email: "notify@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Notify Org", Status: model.OrganizationStatusActive, OwnerUserID: user.ID,
		MaxMembers: 100, MaxWorkspaces: 20, CreatedByUserID: user.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	inApp := &notificationTestSender{channel: model.NotificationChannelInApp}
	email := &notificationTestSender{channel: model.NotificationChannelEmail}
	svc := NewNotificationService(repo, users, orgs, inApp, email)

	pref, err := svc.UpdatePreference(user.ID, &org.ID, model.UpdateNotificationPreferenceRequest{
		InAppEnabled: true, EmailEnabled: true, Digest: model.NotificationDigestImmediate,
		Timezone: "Asia/Jakarta", Locale: "id", QuietHoursStart: "23:00", QuietHoursEnd: "06:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if pref.Timezone != "Asia/Jakarta" || pref.QuietHoursStart != "23:00:00" {
		t.Fatalf("unexpected preference: %+v", pref)
	}

	signal := model.NotificationSignal{
		UserIDs: []int64{user.ID}, OrganizationID: &org.ID,
		EventType: model.NotificationEventIncidentCreated,
		Title: "Incident", Body: "Database degraded", DedupKey: "incident:1",
	}
	if err := svc.EmitNotificationSignal(signal); err != nil {
		t.Fatal(err)
	}
	if err := svc.EmitNotificationSignal(signal); err != nil {
		t.Fatal(err)
	}
	items, err := svc.Inbox(user.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("dedup expected one notification, got %d", len(items))
	}
	if _, err := svc.ProcessDeliveries(context.Background(), 100); err != nil {
		t.Fatal(err)
	}
	if inApp.calls != 1 || email.calls != 1 {
		t.Fatalf("delivery calls in_app=%d email=%d", inApp.calls, email.calls)
	}
	stats, err := svc.Stats(user.ID, &org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 1 || stats.ByStatus[model.NotificationDeliverySent] != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if _, err := svc.MarkRead(user.ID, items[0].ID); err != nil {
		t.Fatal(err)
	}
	stats, _ = svc.Stats(user.ID, &org.ID)
	if stats.Unread != 0 {
		t.Fatalf("unread=%d", stats.Unread)
	}
}

func TestNotificationMutedEventSuppressesCreation(t *testing.T) {
	repo := repository.NewInMemoryNotificationRepository()
	users := repository.NewInMemoryUserRepository()
	orgs := repository.NewInMemoryOrganizationRepository()
	now := time.Now().UTC()
	user, _ := users.Create(model.User{Name: "Muted", Email: "muted@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	svc := NewNotificationService(repo, users, orgs, &notificationTestSender{channel: model.NotificationChannelInApp})
	if _, err := svc.UpdatePreference(user.ID, nil, model.UpdateNotificationPreferenceRequest{
		InAppEnabled: true, Digest: model.NotificationDigestImmediate, Timezone: "UTC", Locale: "en",
		MutedEventTypes: []string{model.NotificationEventTaskAssigned},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EmitNotificationSignal(model.NotificationSignal{
		UserIDs: []int64{user.ID}, EventType: model.NotificationEventTaskAssigned,
		Title: "Task assigned", DedupKey: "task:1",
	}); err != nil {
		t.Fatal(err)
	}
	items, err := svc.Inbox(user.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("muted event should create no notification, got %d", len(items))
	}
}

func TestNotificationFailedDeliveryRetriesAndDeadLetters(t *testing.T) {
	repo := repository.NewInMemoryNotificationRepository()
	users := repository.NewInMemoryUserRepository()
	orgs := repository.NewInMemoryOrganizationRepository()
	now := time.Now().UTC()
	user, _ := users.Create(model.User{Name: "Retry", Email: "retry@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	email := &notificationTestSender{channel: model.NotificationChannelEmail, fail: true}
	svc := NewNotificationService(repo, users, orgs, email)
	if _, err := svc.UpdatePreference(user.ID, nil, model.UpdateNotificationPreferenceRequest{
		EmailEnabled: true, Digest: model.NotificationDigestImmediate, Timezone: "UTC", Locale: "en",
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EmitNotificationSignal(model.NotificationSignal{
		UserIDs: []int64{user.ID}, EventType: model.NotificationEventBilling,
		Title: "Billing", DedupKey: "billing:retry",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ProcessDeliveries(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	stats, err := svc.Stats(user.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ByStatus[model.NotificationDeliveryFailed] != 1 {
		t.Fatalf("expected one failed delivery: %+v", stats)
	}
}

func TestMoveOutsideQuietHours(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Jakarta")
	inside := time.Date(2026, 10, 4, 23, 30, 0, 0, loc)
	moved := moveOutsideQuietHours(inside, "23:00:00", "06:00:00")
	if moved.Hour() != 6 || moved.Day() != 5 {
		t.Fatalf("moved=%s", moved)
	}
}
