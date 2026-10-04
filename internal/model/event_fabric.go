package model

import "time"

const (
	EventSchemaCompatibilityNone     = "none"
	EventSchemaCompatibilityBackward = "backward"
	EventSchemaCompatibilityForward  = "forward"
	EventSchemaCompatibilityFull     = "full"

	EventSchemaStatusActive     = "active"
	EventSchemaStatusDeprecated = "deprecated"

	EventFabricSubscriptionActive = "active"
	EventFabricSubscriptionPaused = "paused"

	EventFabricAdapterPostgresOutbox = "postgres_outbox"
	EventFabricAdapterKafka          = "kafka"
	EventFabricAdapterRabbitMQ       = "rabbitmq"
	EventFabricAdapterNATS           = "nats"
	EventFabricAdapterCloudBus       = "cloud_event_bus"

	EventFabricDeliveryPending    = "pending"
	EventFabricDeliveryRetry      = "retry"
	EventFabricDeliveryDelivered  = "delivered"
	EventFabricDeliveryDeadLetter = "dead_letter"
)

type EventSchemaVersion struct {
	ID              int64          `json:"id"`
	WorkspaceID     int64          `json:"workspace_id"`
	EventType       string         `json:"event_type"`
	Version         int            `json:"version"`
	Compatibility   string         `json:"compatibility"`
	Status          string         `json:"status"`
	Owner           string         `json:"owner"`
	Description     string         `json:"description,omitempty"`
	Schema          map[string]any `json:"schema"`
	CreatedByUserID int64          `json:"created_by_user_id"`
	CreatedAt       time.Time      `json:"created_at"`
	DeprecatedAt    *time.Time     `json:"deprecated_at,omitempty"`
}

type EventFabricSubscription struct {
	ID              int64     `json:"id"`
	WorkspaceID     int64     `json:"workspace_id"`
	Name            string    `json:"name"`
	ConsumerKey     string    `json:"consumer_key"`
	EventTypes      []string  `json:"event_types"`
	Adapter         string    `json:"adapter"`
	Status          string    `json:"status"`
	MaxAttempts     int       `json:"max_attempts"`
	RetentionDays   int       `json:"retention_days"`
	CreatedByUserID int64     `json:"created_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type EventRoute struct {
	ID              int64     `json:"id"`
	WorkspaceID     int64     `json:"workspace_id"`
	Name            string    `json:"name"`
	EventPattern    string    `json:"event_pattern"`
	SubscriptionID  int64     `json:"subscription_id"`
	Active          bool      `json:"active"`
	CreatedByUserID int64     `json:"created_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type EventConsumerOffset struct {
	SubscriptionID int64      `json:"subscription_id"`
	WorkspaceID    int64      `json:"workspace_id"`
	LastEventID    int64      `json:"last_event_id"`
	LastEventKey   string     `json:"last_event_key"`
	LastOccurredAt *time.Time `json:"last_occurred_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type EventFabricDelivery struct {
	ID              int64          `json:"id"`
	WorkspaceID     int64          `json:"workspace_id"`
	SubscriptionID  int64          `json:"subscription_id"`
	OutboxEventID   int64          `json:"outbox_event_id"`
	EventKey        string         `json:"event_key"`
	EventType       string         `json:"event_type"`
	SchemaVersion   int            `json:"schema_version"`
	Payload         map[string]any `json:"payload"`
	CorrelationID   string         `json:"correlation_id"`
	CausationID     string         `json:"causation_id,omitempty"`
	OccurredAt      time.Time      `json:"occurred_at"`
	Status          string         `json:"status"`
	Attempts        int            `json:"attempts"`
	MaxAttempts     int            `json:"max_attempts"`
	AvailableAt     time.Time      `json:"available_at"`
	LockedAt        *time.Time     `json:"-"`
	LockedBy        string         `json:"-"`
	LastError       string         `json:"last_error,omitempty"`
	DeliveredAt     *time.Time     `json:"delivered_at,omitempty"`
	DeadLetteredAt  *time.Time     `json:"dead_lettered_at,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type EventFabricAdapterCapability struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	Durable     bool   `json:"durable"`
	Configured  bool   `json:"configured"`
}

type CreateEventSchemaRequest struct {
	EventType     string         `json:"event_type"`
	Compatibility string         `json:"compatibility"`
	Owner         string         `json:"owner"`
	Description   string         `json:"description,omitempty"`
	Schema        map[string]any `json:"schema"`
}

type CreateEventFabricSubscriptionRequest struct {
	Name          string   `json:"name"`
	ConsumerKey   string   `json:"consumer_key"`
	EventTypes    []string `json:"event_types"`
	Adapter       string   `json:"adapter"`
	MaxAttempts   int      `json:"max_attempts,omitempty"`
	RetentionDays int      `json:"retention_days,omitempty"`
}

type UpdateEventFabricSubscriptionRequest struct {
	Status string `json:"status"`
}

type CreateEventRouteRequest struct {
	Name           string `json:"name"`
	EventPattern   string `json:"event_pattern"`
	SubscriptionID int64  `json:"subscription_id"`
}

type EventReplayRequest struct {
	FromEventID int64 `json:"from_event_id,omitempty"`
	ToEventID   int64 `json:"to_event_id,omitempty"`
}

type EventSchemaCompatibilityResult struct {
	Compatible bool     `json:"compatible"`
	Reasons    []string `json:"reasons,omitempty"`
}
