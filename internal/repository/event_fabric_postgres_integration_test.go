//go:build integration

package repository_test

import (
	"context"
	"testing"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
)

func TestIntegrationPostgresEventFabricSchemaRegistry(t *testing.T) {
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
	tasks := repository.NewPostgresTaskRepository(db)
	eventsRepo := repository.NewPostgresEventRepository(db)
	fabricRepo := repository.NewPostgresEventFabricRepository(db)
	fabric := service.NewEventFabricService(fabricRepo, workspaces, service.NewEventFabricAdapterRegistry())
	now := time.Now().UTC().Truncate(time.Microsecond)

	owner, err := users.Create(model.User{
		Name: "Fabric Owner", Email: "fabric-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	access, err := workspaces.Create(owner.ID, "Fabric Workspace", now)
	if err != nil {
		t.Fatal(err)
	}

	schema, compatibility, err := fabric.CreateSchema(owner.ID, access.ID, model.CreateEventSchemaRequest{
		EventType: model.EventTaskCreated, Compatibility: model.EventSchemaCompatibilityBackward, Owner: "tasks",
		Schema: map[string]any{
			"required": []any{"id", "title"},
			"properties": map[string]any{
				"id": map[string]any{"type": "integer"},
				"title": map[string]any{"type": "string"},
			},
		},
	})
	if err != nil || schema.Version != 1 || !compatibility.Compatible {
		t.Fatalf("schema=%+v compatibility=%+v err=%v", schema, compatibility, err)
	}

	subscription, err := fabric.CreateSubscription(owner.ID, access.ID, model.CreateEventFabricSubscriptionRequest{
		Name: "Projection", ConsumerKey: "projection.tasks", EventTypes: []string{"task.*"},
		Adapter: model.EventFabricAdapterPostgresOutbox, MaxAttempts: 5, RetentionDays: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fabric.CreateRoute(owner.ID, access.ID, model.CreateEventRouteRequest{
		Name: "Tasks", EventPattern: "task.created", SubscriptionID: subscription.ID,
	}); err != nil {
		t.Fatal(err)
	}

	task, err := tasks.Create(model.Task{
		WorkspaceID: access.ID, UserID: owner.ID, Title: "Route through fabric",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityMedium,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.ID == 0 {
		t.Fatal("expected task id")
	}

	outbox, err := eventsRepo.ClaimReady("integration-fabric-source", 10, now.Add(time.Second))
	if err != nil || len(outbox) != 1 {
		t.Fatalf("outbox=%+v err=%v", outbox, err)
	}
	if outbox[0].CorrelationID != outbox[0].EventKey || outbox[0].CausationID != "" {
		t.Fatalf("unexpected correlation metadata: %+v", outbox[0])
	}
	if err := fabric.Consume(context.Background(), outbox[0]); err != nil {
		t.Fatal(err)
	}
	if err := eventsRepo.MarkProcessed(outbox[0].ID, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}

	claimed, err := fabric.ProcessBatch(context.Background(), "integration-fabric-consumer", 10)
	if err != nil || len(claimed) != 1 || claimed[0].Status != model.EventFabricDeliveryDelivered {
		t.Fatalf("claimed=%+v err=%v", claimed, err)
	}
	offset, err := fabric.Offset(owner.ID, access.ID, subscription.ID)
	if err != nil || offset.LastEventID != outbox[0].ID || offset.LastEventKey != outbox[0].EventKey {
		t.Fatalf("offset=%+v err=%v", offset, err)
	}

	count, err := fabric.ReplayRange(owner.ID, access.ID, subscription.ID, model.EventReplayRequest{
		FromEventID: outbox[0].ID, ToEventID: outbox[0].ID,
	})
	if err != nil || count != 1 {
		t.Fatalf("replay count=%d err=%v", count, err)
	}
	replayed, err := fabricRepo.ListDeliveries(access.ID, subscription.ID, model.EventFabricDeliveryPending, 10)
	if err != nil || len(replayed) != 1 || replayed[0].OutboxEventID != outbox[0].ID {
		t.Fatalf("replayed=%+v err=%v", replayed, err)
	}

	if _, err := fabric.UpdateSubscription(owner.ID, access.ID, subscription.ID, model.UpdateEventFabricSubscriptionRequest{
		Status: model.EventFabricSubscriptionPaused,
	}); err != nil {
		t.Fatal(err)
	}
	routes, err := fabric.Routes(owner.ID, access.ID)
	if err != nil || len(routes) != 1 || routes[0].SubscriptionID != subscription.ID {
		t.Fatalf("routes=%+v err=%v", routes, err)
	}
}
