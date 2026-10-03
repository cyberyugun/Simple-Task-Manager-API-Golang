package model

import "time"

const (
	OutboxStatusPending    = "pending"
	OutboxStatusProcessing = "processing"
	OutboxStatusProcessed  = "processed"
	OutboxStatusDead       = "dead"

	DeliveryStatusPending    = "pending"
	DeliveryStatusProcessing = "processing"
	DeliveryStatusDelivered  = "delivered"
	DeliveryStatusDead       = "dead"
)

type DomainEvent struct {
	ID            int64          `json:"-"`
	EventID       string         `json:"event_id"`
	WorkspaceID   int64          `json:"workspace_id"`
	AggregateType string         `json:"aggregate_type"`
	AggregateID   string         `json:"aggregate_id"`
	EventType     string         `json:"event_type"`
	SchemaVersion int            `json:"schema_version"`
	Payload       map[string]any `json:"payload"`
	Status        string         `json:"-"`
	Attempts      int            `json:"-"`
	AvailableAt   time.Time      `json:"-"`
	CreatedAt     time.Time      `json:"occurred_at"`
}

type WebhookSubscription struct {
	ID              int64     `json:"id"`
	WorkspaceID     int64     `json:"workspace_id"`
	URL             string    `json:"url"`
	EventTypes      []string  `json:"event_types"`
	Active          bool      `json:"active"`
	CreatedByUserID int64     `json:"created_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CreateWebhookSubscriptionRequest struct {
	URL        string   `json:"url"`
	Secret     string   `json:"secret"`
	EventTypes []string `json:"event_types"`
}

type WebhookDelivery struct {
	ID             int64
	SubscriptionID int64
	EventID        string
	URL            string
	SigningSecret  string
	EventTypes     []string
	Event          DomainEvent
	Attempts       int
}

type OutboxStats struct {
	PendingEvents     int64 `json:"pending_events"`
	DeadEvents        int64 `json:"dead_events"`
	PendingDeliveries int64 `json:"pending_deliveries"`
	DeadDeliveries    int64 `json:"dead_deliveries"`
}
