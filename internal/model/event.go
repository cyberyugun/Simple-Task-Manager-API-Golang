package model

import (
	"encoding/json"
	"time"
)

const (
	EventTaskCreated = "task.created"
	EventTaskUpdated = "task.updated"
	EventTaskDeleted = "task.deleted"
)

type DomainEvent struct {
	ID            int64           `json:"-"`
	EventKey      string          `json:"id"`
	WorkspaceID   int64           `json:"workspace_id"`
	EventType     string          `json:"type"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	SchemaVersion int             `json:"schema_version"`
	Data          json.RawMessage `json:"data"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Attempts      int             `json:"-"`
	MaxAttempts   int             `json:"-"`
}

type WebhookSubscription struct {
	ID              int64     `json:"id"`
	WorkspaceID     int64     `json:"workspace_id"`
	CreatedByUserID *int64    `json:"created_by_user_id,omitempty"`
	URL             string    `json:"url"`
	EventTypes      []string  `json:"event_types"`
	Active          bool      `json:"active"`
	SigningSecret   string    `json:"signing_secret,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CreateWebhookSubscriptionRequest struct {
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types"`
}

type WebhookDelivery struct {
	EventID        int64
	SubscriptionID int64
	Attempt        int
	Status         string
	HTTPStatus     int
	ResponseBody   string
	Error          string
	AttemptedAt    time.Time
	DeliveredAt    *time.Time
}

type IdempotencyRecord struct {
	WorkspaceID         int64
	Key                 string
	Method              string
	Path                string
	RequestHash         string
	State               string
	ResponseStatus      int
	ResponseContentType string
	ResponseBody        []byte
	ExpiresAt           time.Time
}
