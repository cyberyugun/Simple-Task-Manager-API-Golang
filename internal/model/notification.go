package model

import "time"

const (
	NotificationChannelInApp   = "in_app"
	NotificationChannelEmail   = "email"
	NotificationChannelPush    = "push"
	NotificationChannelWebhook = "webhook"

	NotificationDeliveryPending    = "pending"
	NotificationDeliverySent       = "sent"
	NotificationDeliveryFailed     = "failed"
	NotificationDeliveryDeadLetter = "dead_letter"
	NotificationDeliverySuppressed = "suppressed"

	NotificationDigestImmediate = "immediate"
	NotificationDigestHourly    = "hourly"
	NotificationDigestDaily     = "daily"

	NotificationEventTaskAssigned       = "task.assigned"
	NotificationEventTaskMentioned      = "task.mentioned"
	NotificationEventTaskDueSoon        = "task.due_soon"
	NotificationEventTaskOverdue        = "task.overdue"
	NotificationEventWorkflowApproval   = "workflow.approval.requested"
	NotificationEventIncidentCreated    = "operations.incident.created"
	NotificationEventBilling            = "billing.event"
)

type NotificationPreference struct {
	UserID          int64      `json:"user_id"`
	OrganizationID  *int64     `json:"organization_id,omitempty"`
	InAppEnabled    bool       `json:"in_app_enabled"`
	EmailEnabled    bool       `json:"email_enabled"`
	PushEnabled     bool       `json:"push_enabled"`
	WebhookEnabled  bool       `json:"webhook_enabled"`
	Digest          string     `json:"digest"`
	Timezone        string     `json:"timezone"`
	Locale          string     `json:"locale"`
	QuietHoursStart string     `json:"quiet_hours_start,omitempty"`
	QuietHoursEnd   string     `json:"quiet_hours_end,omitempty"`
	MutedEventTypes []string   `json:"muted_event_types"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type UpdateNotificationPreferenceRequest struct {
	InAppEnabled    bool     `json:"in_app_enabled"`
	EmailEnabled    bool     `json:"email_enabled"`
	PushEnabled     bool     `json:"push_enabled"`
	WebhookEnabled  bool     `json:"webhook_enabled"`
	Digest          string   `json:"digest"`
	Timezone        string   `json:"timezone"`
	Locale          string   `json:"locale"`
	QuietHoursStart string   `json:"quiet_hours_start,omitempty"`
	QuietHoursEnd   string   `json:"quiet_hours_end,omitempty"`
	MutedEventTypes []string `json:"muted_event_types,omitempty"`
}

type Notification struct {
	ID             int64          `json:"id"`
	UserID         int64          `json:"user_id"`
	OrganizationID *int64         `json:"organization_id,omitempty"`
	WorkspaceID    *int64         `json:"workspace_id,omitempty"`
	EventType      string         `json:"event_type"`
	Title          string         `json:"title"`
	Body           string         `json:"body"`
	Data           map[string]any `json:"data,omitempty"`
	DedupKey       string         `json:"dedup_key,omitempty"`
	TemplateKey    string         `json:"template_key,omitempty"`
	TemplateVersion int           `json:"template_version,omitempty"`
	ReadAt         *time.Time     `json:"read_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

type NotificationDelivery struct {
	ID             int64      `json:"id"`
	NotificationID int64      `json:"notification_id"`
	UserID         int64      `json:"user_id"`
	Channel        string     `json:"channel"`
	Destination    string     `json:"destination,omitempty"`
	Status         string     `json:"status"`
	Attempt        int        `json:"attempt"`
	MaxAttempts    int        `json:"max_attempts"`
	ScheduledAt    time.Time  `json:"scheduled_at"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	SentAt         *time.Time `json:"sent_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type NotificationTemplate struct {
	ID             int64     `json:"id"`
	Key            string    `json:"key"`
	Channel        string    `json:"channel"`
	Locale         string    `json:"locale"`
	Version        int       `json:"version"`
	Subject        string    `json:"subject"`
	Body           string    `json:"body"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"created_at"`
}

type NotificationSuppressionRule struct {
	ID             int64     `json:"id"`
	OrganizationID int64     `json:"organization_id"`
	EventType      string    `json:"event_type"`
	Channel        string    `json:"channel"`
	Reason         string    `json:"reason"`
	Active         bool      `json:"active"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type NotificationSignal struct {
	UserIDs         []int64        `json:"user_ids"`
	OrganizationID  *int64         `json:"organization_id,omitempty"`
	WorkspaceID     *int64         `json:"workspace_id,omitempty"`
	EventType       string         `json:"event_type"`
	Title           string         `json:"title"`
	Body            string         `json:"body"`
	Data            map[string]any `json:"data,omitempty"`
	DedupKey        string         `json:"dedup_key,omitempty"`
	TemplateKey     string         `json:"template_key,omitempty"`
}

type NotificationStats struct {
	Total         int64            `json:"total"`
	Unread        int64            `json:"unread"`
	ByChannel     map[string]int64 `json:"by_channel"`
	ByStatus      map[string]int64 `json:"by_status"`
	DeadLettered  int64            `json:"dead_lettered"`
	Suppressed    int64            `json:"suppressed"`
}

type ReminderCandidate struct {
	TaskID      int64
	WorkspaceID int64
	UserID      int64
	Title       string
	DueAt       time.Time
	Kind        string
}
