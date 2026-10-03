package model

import "time"

const EventSchemaVersion = 1

const (
	EventTaskCreated   = "task.created"
	EventTaskUpdated   = "task.updated"
	EventTaskCompleted = "task.completed"
	EventTaskDeleted   = "task.deleted"
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
	OccurredAt    time.Time      `json:"occurred_at"`
}

type EventEnvelope struct {
	EventID       string         `json:"event_id"`
	EventType     string         `json:"event_type"`
	SchemaVersion int            `json:"schema_version"`
	WorkspaceID   int64          `json:"workspace_id"`
	AggregateType string         `json:"aggregate_type"`
	AggregateID   string         `json:"aggregate_id"`
	OccurredAt    time.Time      `json:"occurred_at"`
	Data          map[string]any `json:"data"`
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
	SigningSecret   string    `json:"signing_secret,omitempty"`
}

type CreateWebhookSubscriptionRequest struct {
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types"`
}

type WebhookDelivery struct {
	ID               int64      `json:"id"`
	SubscriptionID   int64      `json:"subscription_id"`
	EventID          string     `json:"event_id"`
	EventType        string     `json:"event_type,omitempty"`
	AttemptCount     int        `json:"attempt_count"`
	MaxAttempts      int        `json:"max_attempts"`
	NextAttemptAt    time.Time  `json:"next_attempt_at"`
	DeliveredAt      *time.Time `json:"delivered_at,omitempty"`
	DeadLetteredAt   *time.Time `json:"dead_lettered_at,omitempty"`
	LastStatus       *int       `json:"last_status,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	URL              string     `json:"-"`
	WorkspaceID      int64      `json:"-"`
	AggregateType    string     `json:"-"`
	AggregateID      string     `json:"-"`
	SchemaVersion    int        `json:"-"`
	Payload          map[string]any `json:"-"`
	OccurredAt       time.Time  `json:"-"`
}

type TaskCreateResult struct {
	Task     Task
	Replayed bool
}
