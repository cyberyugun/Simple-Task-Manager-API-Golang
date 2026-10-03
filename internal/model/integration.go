package model

import "time"

const (
	IntegrationProviderSlack          = "slack"
	IntegrationProviderMicrosoftTeams = "microsoft_teams"
	IntegrationProviderJira           = "jira"
	IntegrationProviderGitHub         = "github"
	IntegrationProviderGenericWebhook = "generic_webhook"

	IntegrationAuthOAuth2        = "oauth2"
	IntegrationAuthBearerToken   = "bearer_token"
	IntegrationAuthWebhookSecret = "webhook_secret"

	IntegrationConnectionActive   = "active"
	IntegrationConnectionDisabled = "disabled"
	IntegrationConnectionError    = "error"

	IntegrationHealthUnknown  = "unknown"
	IntegrationHealthHealthy  = "healthy"
	IntegrationHealthDegraded = "degraded"

	IntegrationDeliveryPending    = "pending"
	IntegrationDeliveryRetry      = "retry"
	IntegrationDeliveryDelivered  = "delivered"
	IntegrationDeliveryDeadLetter = "dead_letter"

	IntegrationInboundAccepted  = "accepted"
	IntegrationInboundDuplicate = "duplicate"
)

type IntegrationConnector struct {
	Provider           string   `json:"provider"`
	DisplayName        string   `json:"display_name"`
	AuthTypes          []string `json:"auth_types"`
	SupportsInbound    bool     `json:"supports_inbound"`
	SupportsOutbound   bool     `json:"supports_outbound"`
	SupportsOAuth      bool     `json:"supports_oauth"`
	SupportsRateLimits bool     `json:"supports_rate_limits"`
}

type IntegrationConnection struct {
	ID                  int64          `json:"id"`
	OrganizationID      int64          `json:"organization_id"`
	Provider            string         `json:"provider"`
	Name                string         `json:"name"`
	Status              string         `json:"status"`
	AuthType            string         `json:"auth_type"`
	Config              map[string]any `json:"config"`
	HealthStatus        string         `json:"health_status"`
	LastHealthCheckedAt *time.Time     `json:"last_health_checked_at,omitempty"`
	ConsecutiveFailures int            `json:"consecutive_failures"`
	RateLimitRemaining  *int64         `json:"rate_limit_remaining,omitempty"`
	RateLimitResetAt    *time.Time     `json:"rate_limit_reset_at,omitempty"`
	CreatedByUserID     int64          `json:"created_by_user_id"`
	UpdatedByUserID     int64          `json:"updated_by_user_id"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

type IntegrationConnectionSecret struct {
	ConnectionID         int64
	OrganizationID       int64
	EncryptedCredentials string
}

type IntegrationDelivery struct {
	ID              int64          `json:"id"`
	OrganizationID  int64          `json:"organization_id"`
	ConnectionID    int64          `json:"connection_id"`
	EventKey        string         `json:"event_key"`
	EventType       string         `json:"event_type"`
	Payload         map[string]any `json:"payload"`
	Status          string         `json:"status"`
	Attempts        int            `json:"attempts"`
	MaxAttempts     int            `json:"max_attempts"`
	AvailableAt     time.Time      `json:"available_at"`
	LockedAt        *time.Time     `json:"-"`
	LockedBy        string         `json:"-"`
	LastError       string         `json:"last_error,omitempty"`
	LastHTTPStatus  int            `json:"last_http_status,omitempty"`
	DeliveredAt     *time.Time     `json:"delivered_at,omitempty"`
	DeadLetteredAt  *time.Time     `json:"dead_lettered_at,omitempty"`
	CreatedByUserID int64          `json:"created_by_user_id"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type IntegrationInboundEvent struct {
	ID              int64          `json:"id"`
	OrganizationID  int64          `json:"organization_id"`
	ConnectionID    int64          `json:"connection_id"`
	ProviderEventID string         `json:"provider_event_id"`
	EventType       string         `json:"event_type"`
	Payload         map[string]any `json:"payload"`
	Status          string         `json:"status"`
	ReceivedAt      time.Time      `json:"received_at"`
}

type CreateIntegrationConnectionRequest struct {
	Provider    string         `json:"provider"`
	Name        string         `json:"name"`
	AuthType    string         `json:"auth_type"`
	Config      map[string]any `json:"config"`
	Credentials map[string]any `json:"credentials"`
}

type UpdateIntegrationConnectionRequest struct {
	Name        string         `json:"name"`
	Status      string         `json:"status"`
	Config      map[string]any `json:"config"`
	Credentials map[string]any `json:"credentials,omitempty"`
}

type CreateIntegrationDeliveryRequest struct {
	ConnectionID int64          `json:"connection_id"`
	EventType    string         `json:"event_type"`
	Payload      map[string]any `json:"payload"`
}

type IntegrationInboundEnvelope struct {
	EventID   string         `json:"event_id"`
	EventType string         `json:"event_type"`
	Payload   map[string]any `json:"payload"`
}
