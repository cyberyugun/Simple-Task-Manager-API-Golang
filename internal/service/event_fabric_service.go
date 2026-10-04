package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrEventFabricForbidden              = errors.New("event fabric action is forbidden")
	ErrInvalidEventSchema                = errors.New("invalid event schema")
	ErrEventSchemaIncompatible           = errors.New("event schema is incompatible with the previous version")
	ErrEventSchemaUnknownVersion         = errors.New("event schema version is not registered")
	ErrInvalidEventFabricSubscription    = errors.New("invalid event fabric subscription")
	ErrInvalidEventRoute                 = errors.New("invalid event route")
	ErrEventFabricAdapterNotConfigured   = errors.New("event fabric adapter is not configured")
	ErrInvalidEventReplay                = errors.New("invalid event replay range")
)

type EventFabricAdapter interface {
	Key() string
	Publish(context.Context, model.EventFabricSubscription, model.EventFabricDelivery) error
}

type EventFabricAdapterRegistry struct {
	adapters map[string]EventFabricAdapter
}

func NewEventFabricAdapterRegistry() *EventFabricAdapterRegistry {
	registry := &EventFabricAdapterRegistry{adapters: make(map[string]EventFabricAdapter)}
	registry.Register(postgresOutboxEventAdapter{})
	return registry
}

func (r *EventFabricAdapterRegistry) Register(adapter EventFabricAdapter) {
	if adapter == nil || strings.TrimSpace(adapter.Key()) == "" {
		return
	}
	r.adapters[adapter.Key()] = adapter
}

func (r *EventFabricAdapterRegistry) Publish(ctx context.Context, subscription model.EventFabricSubscription, delivery model.EventFabricDelivery) error {
	adapter, ok := r.adapters[subscription.Adapter]
	if !ok {
		return fmt.Errorf("%w: %s", ErrEventFabricAdapterNotConfigured, subscription.Adapter)
	}
	return adapter.Publish(ctx, subscription, delivery)
}

func (r *EventFabricAdapterRegistry) Capabilities() []model.EventFabricAdapterCapability {
	catalog := []model.EventFabricAdapterCapability{
		{Key: model.EventFabricAdapterPostgresOutbox, DisplayName: "PostgreSQL Durable Outbox", Durable: true},
		{Key: model.EventFabricAdapterKafka, DisplayName: "Apache Kafka", Durable: true},
		{Key: model.EventFabricAdapterRabbitMQ, DisplayName: "RabbitMQ", Durable: true},
		{Key: model.EventFabricAdapterNATS, DisplayName: "NATS / JetStream", Durable: true},
		{Key: model.EventFabricAdapterCloudBus, DisplayName: "Cloud Event Bus", Durable: true},
	}
	for i := range catalog {
		_, catalog[i].Configured = r.adapters[catalog[i].Key]
	}
	return catalog
}

type postgresOutboxEventAdapter struct{}

func (postgresOutboxEventAdapter) Key() string {
	return model.EventFabricAdapterPostgresOutbox
}

func (postgresOutboxEventAdapter) Publish(context.Context, model.EventFabricSubscription, model.EventFabricDelivery) error {
	return nil
}

type EventFabricService struct {
	repo       repository.EventFabricRepository
	workspaces repository.WorkspaceRepository
	adapters   *EventFabricAdapterRegistry
}

func NewEventFabricService(repo repository.EventFabricRepository, workspaces repository.WorkspaceRepository, adapters *EventFabricAdapterRegistry) *EventFabricService {
	if adapters == nil {
		adapters = NewEventFabricAdapterRegistry()
	}
	return &EventFabricService{repo: repo, workspaces: workspaces, adapters: adapters}
}

func (s *EventFabricService) Adapters() []model.EventFabricAdapterCapability {
	return s.adapters.Capabilities()
}

func (s *EventFabricService) Schemas(actorUserID, workspaceID int64, eventType string) ([]model.EventSchemaVersion, error) {
	if _, err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListSchemas(workspaceID, strings.TrimSpace(eventType))
}

func (s *EventFabricService) CreateSchema(actorUserID, workspaceID int64, req model.CreateEventSchemaRequest) (model.EventSchemaVersion, model.EventSchemaCompatibilityResult, error) {
	if _, err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.EventSchemaVersion{}, model.EventSchemaCompatibilityResult{}, err
	}
	eventType := normalizeEventType(req.EventType)
	owner := strings.TrimSpace(req.Owner)
	compatibility := strings.ToLower(strings.TrimSpace(req.Compatibility))
	if compatibility == "" {
		compatibility = model.EventSchemaCompatibilityBackward
	}
	if eventType == "" || len(eventType) > 160 || owner == "" || len(owner) > 160 || !validEventCompatibility(compatibility) {
		return model.EventSchemaVersion{}, model.EventSchemaCompatibilityResult{}, ErrInvalidEventSchema
	}
	if err := validateEventSchemaShape(req.Schema); err != nil {
		return model.EventSchemaVersion{}, model.EventSchemaCompatibilityResult{}, err
	}
	versions, err := s.repo.ListSchemas(workspaceID, eventType)
	if err != nil {
		return model.EventSchemaVersion{}, model.EventSchemaCompatibilityResult{}, err
	}
	version := 1
	result := model.EventSchemaCompatibilityResult{Compatible: true}
	if len(versions) > 0 {
		version = versions[0].Version + 1
		result = checkEventSchemaCompatibility(versions[0].Schema, req.Schema, compatibility)
		if !result.Compatible {
			return model.EventSchemaVersion{}, result, ErrEventSchemaIncompatible
		}
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateSchema(model.EventSchemaVersion{
		WorkspaceID: workspaceID, EventType: eventType, Version: version, Compatibility: compatibility,
		Status: model.EventSchemaStatusActive, Owner: owner, Description: strings.TrimSpace(req.Description),
		Schema: req.Schema, CreatedByUserID: actorUserID, CreatedAt: now,
	})
	if err == nil {
		_ = s.audit(workspaceID, actorUserID, "event_fabric.schema.created", "event_schema", fmt.Sprint(item.ID),
			map[string]any{"event_type": eventType, "version": version, "compatibility": compatibility}, now)
	}
	return item, result, err
}

func (s *EventFabricService) DeprecateSchema(actorUserID, workspaceID, schemaID int64) error {
	if _, err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := s.repo.DeprecateSchema(workspaceID, schemaID, now); err != nil {
		return err
	}
	return s.audit(workspaceID, actorUserID, "event_fabric.schema.deprecated", "event_schema", fmt.Sprint(schemaID), nil, now)
}

func (s *EventFabricService) Subscriptions(actorUserID, workspaceID int64) ([]model.EventFabricSubscription, error) {
	if _, err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListSubscriptions(workspaceID)
}

func (s *EventFabricService) CreateSubscription(actorUserID, workspaceID int64, req model.CreateEventFabricSubscriptionRequest) (model.EventFabricSubscription, error) {
	if _, err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.EventFabricSubscription{}, err
	}
	name := strings.TrimSpace(req.Name)
	consumerKey := strings.ToLower(strings.TrimSpace(req.ConsumerKey))
	adapter := strings.ToLower(strings.TrimSpace(req.Adapter))
	if adapter == "" {
		adapter = model.EventFabricAdapterPostgresOutbox
	}
	eventTypes := normalizeEventPatterns(req.EventTypes)
	if name == "" || len(name) > 200 || consumerKey == "" || len(consumerKey) > 160 ||
		len(eventTypes) == 0 || !knownEventFabricAdapter(adapter) {
		return model.EventFabricSubscription{}, ErrInvalidEventFabricSubscription
	}
	maxAttempts := req.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 8
	}
	retentionDays := req.RetentionDays
	if retentionDays == 0 {
		retentionDays = 30
	}
	if maxAttempts < 1 || maxAttempts > 50 || retentionDays < 1 || retentionDays > 3650 {
		return model.EventFabricSubscription{}, ErrInvalidEventFabricSubscription
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateSubscription(model.EventFabricSubscription{
		WorkspaceID: workspaceID, Name: name, ConsumerKey: consumerKey, EventTypes: eventTypes,
		Adapter: adapter, Status: model.EventFabricSubscriptionActive, MaxAttempts: maxAttempts,
		RetentionDays: retentionDays, CreatedByUserID: actorUserID, CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		_ = s.audit(workspaceID, actorUserID, "event_fabric.subscription.created", "event_subscription", fmt.Sprint(item.ID),
			map[string]any{"consumer_key": consumerKey, "adapter": adapter, "event_types": eventTypes}, now)
	}
	return item, err
}

func (s *EventFabricService) UpdateSubscription(actorUserID, workspaceID, subscriptionID int64, req model.UpdateEventFabricSubscriptionRequest) (model.EventFabricSubscription, error) {
	if _, err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.EventFabricSubscription{}, err
	}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status != model.EventFabricSubscriptionActive && status != model.EventFabricSubscriptionPaused {
		return model.EventFabricSubscription{}, ErrInvalidEventFabricSubscription
	}
	now := time.Now().UTC()
	item, err := s.repo.UpdateSubscriptionStatus(workspaceID, subscriptionID, status, now)
	if err == nil {
		_ = s.audit(workspaceID, actorUserID, "event_fabric.subscription.updated", "event_subscription", fmt.Sprint(subscriptionID),
			map[string]any{"status": status}, now)
	}
	return item, err
}

func (s *EventFabricService) Offset(actorUserID, workspaceID, subscriptionID int64) (model.EventConsumerOffset, error) {
	if _, err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.EventConsumerOffset{}, err
	}
	if _, err := s.repo.GetSubscription(workspaceID, subscriptionID); err != nil {
		return model.EventConsumerOffset{}, err
	}
	return s.repo.GetOffset(workspaceID, subscriptionID)
}

func (s *EventFabricService) Routes(actorUserID, workspaceID int64) ([]model.EventRoute, error) {
	if _, err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListRoutes(workspaceID)
}

func (s *EventFabricService) CreateRoute(actorUserID, workspaceID int64, req model.CreateEventRouteRequest) (model.EventRoute, error) {
	if _, err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.EventRoute{}, err
	}
	name := strings.TrimSpace(req.Name)
	pattern := normalizeEventType(req.EventPattern)
	if name == "" || len(name) > 200 || !validEventPattern(pattern) || req.SubscriptionID <= 0 {
		return model.EventRoute{}, ErrInvalidEventRoute
	}
	subscription, err := s.repo.GetSubscription(workspaceID, req.SubscriptionID)
	if err != nil {
		return model.EventRoute{}, err
	}
	if !matchesAnyEventPattern(subscription.EventTypes, pattern) && pattern != "*" {
		return model.EventRoute{}, ErrInvalidEventRoute
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateRoute(model.EventRoute{
		WorkspaceID: workspaceID, Name: name, EventPattern: pattern, SubscriptionID: req.SubscriptionID,
		Active: true, CreatedByUserID: actorUserID, CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		_ = s.audit(workspaceID, actorUserID, "event_fabric.route.created", "event_route", fmt.Sprint(item.ID),
			map[string]any{"event_pattern": pattern, "subscription_id": req.SubscriptionID}, now)
	}
	return item, err
}

func (s *EventFabricService) Deliveries(actorUserID, workspaceID, subscriptionID int64, status string) ([]model.EventFabricDelivery, error) {
	if _, err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "" && status != model.EventFabricDeliveryPending && status != model.EventFabricDeliveryRetry &&
		status != model.EventFabricDeliveryDelivered && status != model.EventFabricDeliveryDeadLetter {
		return nil, ErrInvalidEventReplay
	}
	return s.repo.ListDeliveries(workspaceID, subscriptionID, status, 200)
}

func (s *EventFabricService) ReplayDelivery(actorUserID, workspaceID, deliveryID int64) error {
	if _, err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return err
	}
	if _, err := s.repo.GetDelivery(workspaceID, deliveryID); err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := s.repo.ReplayDelivery(workspaceID, deliveryID, now); err != nil {
		return err
	}
	return s.audit(workspaceID, actorUserID, "event_fabric.delivery.redriven", "event_delivery", fmt.Sprint(deliveryID), nil, now)
}

func (s *EventFabricService) ReplayRange(actorUserID, workspaceID, subscriptionID int64, req model.EventReplayRequest) (int64, error) {
	if _, err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return 0, err
	}
	if req.FromEventID < 0 || req.ToEventID < 0 || (req.FromEventID > 0 && req.ToEventID > 0 && req.FromEventID > req.ToEventID) {
		return 0, ErrInvalidEventReplay
	}
	if _, err := s.repo.GetSubscription(workspaceID, subscriptionID); err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	count, err := s.repo.ReplayRange(workspaceID, subscriptionID, req.FromEventID, req.ToEventID, now)
	if err == nil {
		_ = s.audit(workspaceID, actorUserID, "event_fabric.subscription.replayed", "event_subscription", fmt.Sprint(subscriptionID),
			map[string]any{"from_event_id": req.FromEventID, "to_event_id": req.ToEventID, "count": count}, now)
	}
	return count, err
}

func (s *EventFabricService) Consume(_ context.Context, event model.DomainEvent) error {
	var payload map[string]any
	if err := json.Unmarshal(event.Data, &payload); err != nil {
		return fmt.Errorf("%w: payload is not an object", ErrInvalidEventSchema)
	}
	schemas, err := s.repo.ListSchemas(event.WorkspaceID, event.EventType)
	if err != nil {
		return err
	}
	if len(schemas) > 0 {
		var schema *model.EventSchemaVersion
		for i := range schemas {
			if schemas[i].Version == event.SchemaVersion && schemas[i].Status == model.EventSchemaStatusActive {
				schema = &schemas[i]
				break
			}
		}
		if schema == nil {
			return fmt.Errorf("%w: %s v%d", ErrEventSchemaUnknownVersion, event.EventType, event.SchemaVersion)
		}
		if err := validateEventPayload(payload, schema.Schema); err != nil {
			return err
		}
	}
	routes, err := s.repo.MatchingRoutes(event.WorkspaceID, event.EventType)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	correlationID := strings.TrimSpace(event.CorrelationID)
	if correlationID == "" {
		correlationID = event.EventKey
	}
	seen := make(map[int64]struct{})
	for _, route := range routes {
		if _, ok := seen[route.SubscriptionID]; ok {
			continue
		}
		subscription, err := s.repo.GetSubscription(event.WorkspaceID, route.SubscriptionID)
		if err != nil {
			return err
		}
		if subscription.Status != model.EventFabricSubscriptionActive || !matchesAnyEventPattern(subscription.EventTypes, event.EventType) {
			continue
		}
		seen[route.SubscriptionID] = struct{}{}
		_, _, err = s.repo.CreateDelivery(model.EventFabricDelivery{
			WorkspaceID: event.WorkspaceID, SubscriptionID: subscription.ID, OutboxEventID: event.ID,
			EventKey: event.EventKey, EventType: event.EventType, SchemaVersion: event.SchemaVersion,
			Payload: payload, CorrelationID: correlationID, CausationID: strings.TrimSpace(event.CausationID),
			OccurredAt: event.OccurredAt, Status: model.EventFabricDeliveryPending, MaxAttempts: subscription.MaxAttempts,
			AvailableAt: now, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *EventFabricService) ProcessBatch(ctx context.Context, workerID string, limit int) ([]model.EventFabricDelivery, error) {
	if limit <= 0 {
		limit = 50
	}
	now := time.Now().UTC()
	_, _ = s.repo.ReleaseDeliveryLocks(now.Add(-2 * time.Minute))
	items, err := s.repo.ClaimDeliveries(workerID, limit, now)
	if err != nil {
		return nil, err
	}
	for i := range items {
		subscription, getErr := s.repo.GetSubscription(items[i].WorkspaceID, items[i].SubscriptionID)
		if getErr != nil {
			return items[:i], getErr
		}
		if publishErr := s.adapters.Publish(ctx, subscription, items[i]); publishErr != nil {
			dead, markErr := s.repo.MarkDeliveryFailed(items[i].ID, publishErr.Error(), time.Now().UTC())
			if markErr != nil {
				return items[:i+1], markErr
			}
			items[i].Attempts++
			if dead {
				items[i].Status = model.EventFabricDeliveryDeadLetter
			} else {
				items[i].Status = model.EventFabricDeliveryRetry
			}
			continue
		}
		deliveredAt := time.Now().UTC()
		if err := s.repo.MarkDeliverySucceeded(items[i].ID, deliveredAt); err != nil {
			return items[:i+1], err
		}
		occurred := items[i].OccurredAt
		if err := s.repo.UpsertOffset(model.EventConsumerOffset{
			SubscriptionID: items[i].SubscriptionID, WorkspaceID: items[i].WorkspaceID,
			LastEventID: items[i].OutboxEventID, LastEventKey: items[i].EventKey,
			LastOccurredAt: &occurred, UpdatedAt: deliveredAt,
		}); err != nil {
			return items[:i+1], err
		}
		items[i].Status = model.EventFabricDeliveryDelivered
		items[i].DeliveredAt = &deliveredAt
	}
	return items, nil
}

func (s *EventFabricService) PurgeRetention(limit int) (int64, error) {
	if limit <= 0 {
		limit = 500
	}
	return s.repo.PurgeDelivered(time.Now().UTC(), limit)
}

func (s *EventFabricService) requireAdmin(userID, workspaceID int64) (model.WorkspaceAccess, error) {
	access, err := s.workspaces.ResolveAccess(userID, workspaceID, time.Now())
	if err != nil {
		return model.WorkspaceAccess{}, err
	}
	if access.Role != model.WorkspaceRoleOwner && access.Role != model.WorkspaceRoleAdmin {
		return model.WorkspaceAccess{}, ErrEventFabricForbidden
	}
	return access, nil
}

func (s *EventFabricService) audit(workspaceID, actorID int64, action, resourceType, resourceID string, metadata map[string]any, now time.Time) error {
	wid := workspaceID
	uid := actorID
	return s.workspaces.RecordAudit(model.AuditEvent{
		WorkspaceID: &wid, ActorUserID: &uid, Action: action, ResourceType: resourceType,
		ResourceID: resourceID, Metadata: metadata, CreatedAt: now,
	})
}

func normalizeEventType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validEventCompatibility(value string) bool {
	switch value {
	case model.EventSchemaCompatibilityNone, model.EventSchemaCompatibilityBackward,
		model.EventSchemaCompatibilityForward, model.EventSchemaCompatibilityFull:
		return true
	default:
		return false
	}
}

func knownEventFabricAdapter(value string) bool {
	switch value {
	case model.EventFabricAdapterPostgresOutbox, model.EventFabricAdapterKafka,
		model.EventFabricAdapterRabbitMQ, model.EventFabricAdapterNATS, model.EventFabricAdapterCloudBus:
		return true
	default:
		return false
	}
}

func normalizeEventPatterns(values []string) []string {
	seen := make(map[string]struct{})
	items := make([]string, 0, len(values))
	for _, raw := range values {
		value := normalizeEventType(raw)
		if !validEventPattern(value) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}

func validEventPattern(value string) bool {
	if value == "*" {
		return true
	}
	if value == "" || len(value) > 160 || strings.ContainsAny(value, " /\\") {
		return false
	}
	if strings.Count(value, "*") > 1 {
		return false
	}
	return !strings.Contains(value, "*") || strings.HasSuffix(value, ".*")
}

func matchesAnyEventPattern(patterns []string, eventType string) bool {
	for _, pattern := range patterns {
		if pattern == "*" || pattern == eventType {
			return true
		}
		if strings.HasSuffix(pattern, ".*") && strings.HasPrefix(eventType, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}

func validateEventSchemaShape(schema map[string]any) error {
	if schema == nil {
		return ErrInvalidEventSchema
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return ErrInvalidEventSchema
	}
	for name, raw := range properties {
		if strings.TrimSpace(name) == "" {
			return ErrInvalidEventSchema
		}
		definition, ok := raw.(map[string]any)
		if !ok || !validEventJSONType(strings.TrimSpace(fmt.Sprint(definition["type"]))) {
			return ErrInvalidEventSchema
		}
	}
	for _, required := range schemaRequired(schema) {
		if _, ok := properties[required]; !ok {
			return ErrInvalidEventSchema
		}
	}
	return nil
}

func validEventJSONType(value string) bool {
	switch value {
	case "string", "number", "integer", "boolean", "object", "array":
		return true
	default:
		return false
	}
}

func schemaRequired(schema map[string]any) []string {
	raw, ok := schema["required"]
	if !ok {
		return nil
	}
	items := make([]string, 0)
	switch values := raw.(type) {
	case []any:
		for _, value := range values {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
				items = append(items, text)
			}
		}
	case []string:
		for _, value := range values {
			if text := strings.TrimSpace(value); text != "" {
				items = append(items, text)
			}
		}
	}
	sort.Strings(items)
	return items
}

func schemaPropertyTypes(schema map[string]any) map[string]string {
	out := make(map[string]string)
	properties, _ := schema["properties"].(map[string]any)
	for name, raw := range properties {
		if definition, ok := raw.(map[string]any); ok {
			out[name] = strings.TrimSpace(fmt.Sprint(definition["type"]))
		}
	}
	return out
}

func checkEventSchemaCompatibility(previous, next map[string]any, mode string) model.EventSchemaCompatibilityResult {
	result := model.EventSchemaCompatibilityResult{Compatible: true}
	if mode == model.EventSchemaCompatibilityNone {
		return result
	}
	prevTypes := schemaPropertyTypes(previous)
	nextTypes := schemaPropertyTypes(next)
	prevRequired := toEventSet(schemaRequired(previous))
	nextRequired := toEventSet(schemaRequired(next))
	for name, oldType := range prevTypes {
		if newType, ok := nextTypes[name]; ok && newType != oldType {
			result.Reasons = append(result.Reasons, fmt.Sprintf("property %s changed type from %s to %s", name, oldType, newType))
		}
	}
	if mode == model.EventSchemaCompatibilityBackward || mode == model.EventSchemaCompatibilityFull {
		for name := range prevRequired {
			if _, ok := nextTypes[name]; !ok {
				result.Reasons = append(result.Reasons, fmt.Sprintf("required property %s was removed", name))
				continue
			}
			if _, ok := nextRequired[name]; !ok {
				result.Reasons = append(result.Reasons, fmt.Sprintf("required property %s became optional", name))
			}
		}
	}
	if mode == model.EventSchemaCompatibilityForward || mode == model.EventSchemaCompatibilityFull {
		for name := range nextRequired {
			if _, ok := prevRequired[name]; !ok {
				result.Reasons = append(result.Reasons, fmt.Sprintf("new required property %s breaks forward compatibility", name))
			}
		}
	}
	result.Compatible = len(result.Reasons) == 0
	return result
}

func toEventSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		out[item] = struct{}{}
	}
	return out
}

func validateEventPayload(payload, schema map[string]any) error {
	properties := schemaPropertyTypes(schema)
	for _, name := range schemaRequired(schema) {
		if _, ok := payload[name]; !ok {
			return fmt.Errorf("%w: missing required property %s", ErrInvalidEventSchema, name)
		}
	}
	for name, value := range payload {
		expected, ok := properties[name]
		if !ok || value == nil {
			continue
		}
		if !eventJSONValueMatchesType(value, expected) {
			return fmt.Errorf("%w: property %s must be %s", ErrInvalidEventSchema, name, expected)
		}
	}
	return nil
}

func eventJSONValueMatchesType(value any, expected string) bool {
	switch expected {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && number == float64(int64(number))
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	default:
		return false
	}
}
