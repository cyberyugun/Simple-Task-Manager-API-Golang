package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestEventFabricSchemaRoutingOffsetReplayAndDLQ(t *testing.T) {
	workspaces := repository.NewInMemoryWorkspaceRepository()
	access, err := workspaces.Create(7, "Event Fabric", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewInMemoryEventFabricRepository()
	service := NewEventFabricService(repo, workspaces, NewEventFabricAdapterRegistry())

	first, compatibility, err := service.CreateSchema(7, access.ID, model.CreateEventSchemaRequest{
		EventType: "task.created", Compatibility: model.EventSchemaCompatibilityFull, Owner: "tasks",
		Schema: map[string]any{
			"required": []any{"id", "title"},
			"properties": map[string]any{
				"id": map[string]any{"type": "integer"},
				"title": map[string]any{"type": "string"},
			},
		},
	})
	if err != nil || !compatibility.Compatible || first.Version != 1 {
		t.Fatalf("schema=%+v compatibility=%+v err=%v", first, compatibility, err)
	}

	_, result, err := service.CreateSchema(7, access.ID, model.CreateEventSchemaRequest{
		EventType: "task.created", Compatibility: model.EventSchemaCompatibilityFull, Owner: "tasks",
		Schema: map[string]any{
			"required": []any{"id", "title", "region"},
			"properties": map[string]any{
				"id": map[string]any{"type": "integer"},
				"title": map[string]any{"type": "string"},
				"region": map[string]any{"type": "string"},
			},
		},
	})
	if !errors.Is(err, ErrEventSchemaIncompatible) || result.Compatible {
		t.Fatalf("expected incompatibility, result=%+v err=%v", result, err)
	}

	second, result, err := service.CreateSchema(7, access.ID, model.CreateEventSchemaRequest{
		EventType: "task.created", Compatibility: model.EventSchemaCompatibilityFull, Owner: "tasks",
		Schema: map[string]any{
			"required": []any{"id", "title"},
			"properties": map[string]any{
				"id": map[string]any{"type": "integer"},
				"title": map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
			},
		},
	})
	if err != nil || !result.Compatible || second.Version != 2 {
		t.Fatalf("schema=%+v compatibility=%+v err=%v", second, result, err)
	}

	subscription, err := service.CreateSubscription(7, access.ID, model.CreateEventFabricSubscriptionRequest{
		Name: "Task projection", ConsumerKey: "projection.tasks", EventTypes: []string{"task.*"},
		Adapter: model.EventFabricAdapterPostgresOutbox, MaxAttempts: 3, RetentionDays: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateRoute(7, access.ID, model.CreateEventRouteRequest{
		Name: "Task created route", EventPattern: "task.created", SubscriptionID: subscription.ID,
	}); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(map[string]any{"id": 42, "title": "Ship event fabric"})
	occurred := time.Now().UTC().Add(-time.Minute)
	event := model.DomainEvent{
		ID: 11, EventKey: "evt-11", WorkspaceID: access.ID, EventType: "task.created",
		AggregateType: "task", AggregateID: "42", SchemaVersion: 1, Data: payload, OccurredAt: occurred,
	}
	if err := service.Consume(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	pending, err := repo.ListDeliveries(access.ID, subscription.ID, model.EventFabricDeliveryPending, 10)
	if err != nil || len(pending) != 1 || pending[0].CorrelationID != event.EventKey {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}

	processed, err := service.ProcessBatch(context.Background(), "test-worker", 10)
	if err != nil || len(processed) != 1 || processed[0].Status != model.EventFabricDeliveryDelivered {
		t.Fatalf("processed=%+v err=%v", processed, err)
	}
	offset, err := service.Offset(7, access.ID, subscription.ID)
	if err != nil || offset.LastEventID != event.ID || offset.LastEventKey != event.EventKey {
		t.Fatalf("offset=%+v err=%v", offset, err)
	}
	count, err := service.ReplayRange(7, access.ID, subscription.ID, model.EventReplayRequest{FromEventID: 11, ToEventID: 11})
	if err != nil || count != 1 {
		t.Fatalf("replay count=%d err=%v", count, err)
	}

	badPayload, _ := json.Marshal(map[string]any{"id": 43})
	if err := service.Consume(context.Background(), model.DomainEvent{
		ID: 12, EventKey: "evt-12", WorkspaceID: access.ID, EventType: "task.created",
		AggregateType: "task", AggregateID: "43", SchemaVersion: 1, Data: badPayload, OccurredAt: time.Now().UTC(),
	}); !errors.Is(err, ErrInvalidEventSchema) {
		t.Fatalf("expected payload schema error, got %v", err)
	}

	external, err := service.CreateSubscription(7, access.ID, model.CreateEventFabricSubscriptionRequest{
		Name: "Kafka sink", ConsumerKey: "sink.kafka", EventTypes: []string{"task.updated"},
		Adapter: model.EventFabricAdapterKafka, MaxAttempts: 1, RetentionDays: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateRoute(7, access.ID, model.CreateEventRouteRequest{
		Name: "Kafka route", EventPattern: "task.updated", SubscriptionID: external.ID,
	}); err != nil {
		t.Fatal(err)
	}
	updatedPayload, _ := json.Marshal(map[string]any{"id": 42})
	if err := service.Consume(context.Background(), model.DomainEvent{
		ID: 13, EventKey: "evt-13", WorkspaceID: access.ID, EventType: "task.updated",
		AggregateType: "task", AggregateID: "42", SchemaVersion: 1, Data: updatedPayload, OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	items, err := service.ProcessBatch(context.Background(), "test-worker", 10)
	if err != nil {
		t.Fatal(err)
	}
	foundDead := false
	for _, item := range items {
		if item.SubscriptionID == external.ID && item.Status == model.EventFabricDeliveryDeadLetter {
			foundDead = true
		}
	}
	if !foundDead {
		t.Fatalf("expected unconfigured external adapter to isolate delivery in DLQ: %+v", items)
	}
}
