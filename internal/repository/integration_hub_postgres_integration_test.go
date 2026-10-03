//go:build integration

package repository_test

import (
	"testing"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestIntegrationPostgresIntegrationHubRepository(t *testing.T) {
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
	orgs := repository.NewPostgresOrganizationRepository(db)
	integrations := repository.NewPostgresIntegrationRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	owner, err := users.Create(model.User{Name: "Integration Owner", Email: "integration-owner@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{Name: "Integration Org", Status: model.OrganizationStatusActive, OwnerUserID: owner.ID, MaxMembers: 100, MaxWorkspaces: 20, CreatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}

	connection, err := integrations.CreateIntegrationConnection(model.IntegrationConnection{
		OrganizationID: org.ID, Provider: model.IntegrationProviderGenericWebhook, Name: "receiver",
		Status: model.IntegrationConnectionActive, AuthType: model.IntegrationAuthWebhookSecret,
		Config: map[string]any{"target_url": "https://example.com/hook"}, HealthStatus: model.IntegrationHealthUnknown,
		CreatedByUserID: owner.ID, UpdatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now,
	}, "encrypted-secret")
	if err != nil || connection.ID == 0 {
		t.Fatalf("connection=%+v err=%v", connection, err)
	}
	secret, err := integrations.GetIntegrationConnectionSecret(connection.ID)
	if err != nil || secret.EncryptedCredentials != "encrypted-secret" {
		t.Fatalf("secret=%+v err=%v", secret, err)
	}

	delivery, err := integrations.CreateIntegrationDelivery(model.IntegrationDelivery{
		OrganizationID: org.ID, ConnectionID: connection.ID, EventKey: "event-1", EventType: "task.created",
		Payload: map[string]any{"id": float64(1)}, Status: model.IntegrationDeliveryPending, MaxAttempts: 5,
		AvailableAt: now, CreatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := integrations.ClaimIntegrationDeliveries("integration-test", 10, now.Add(time.Second))
	if err != nil || len(claimed) != 1 || claimed[0].ID != delivery.ID {
		t.Fatalf("claimed=%+v err=%v", claimed, err)
	}
	if err := integrations.MarkIntegrationDelivered(delivery.ID, 202, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	items, err := integrations.ListIntegrationDeliveries(org.ID, 10)
	if err != nil || len(items) != 1 || items[0].Status != model.IntegrationDeliveryDelivered {
		t.Fatalf("items=%+v err=%v", items, err)
	}

	event, err := integrations.RecordInboundIntegrationEvent(model.IntegrationInboundEvent{
		OrganizationID: org.ID, ConnectionID: connection.ID, ProviderEventID: "remote-1", EventType: "task.created",
		Payload: map[string]any{"id": float64(1)}, Status: model.IntegrationInboundAccepted, ReceivedAt: now,
	})
	if err != nil || event.ID == 0 {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	if _, err := integrations.RecordInboundIntegrationEvent(model.IntegrationInboundEvent{
		OrganizationID: org.ID, ConnectionID: connection.ID, ProviderEventID: "remote-1", EventType: "task.created",
		Payload: map[string]any{}, Status: model.IntegrationInboundAccepted, ReceivedAt: now,
	}); err != repository.ErrIntegrationInboundDuplicate {
		t.Fatalf("duplicate err=%v", err)
	}

	remaining := int64(42)
	reset := now.Add(time.Hour)
	if err := integrations.UpdateIntegrationHealth(connection.ID, model.IntegrationHealthHealthy, 0, now, &remaining, &reset); err != nil {
		t.Fatal(err)
	}
	updated, err := integrations.GetIntegrationConnection(org.ID, connection.ID)
	if err != nil || updated.HealthStatus != model.IntegrationHealthHealthy || updated.RateLimitRemaining == nil || *updated.RateLimitRemaining != 42 {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
}
