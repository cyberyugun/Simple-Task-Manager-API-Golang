package model

import "time"

const (
	NotificationChannelInApp   = "in_app"
	NotificationChannelEmail   = "email"
	NotificationChannelPush    = "push"
	NotificationChannelWebhook = "webhook"

	NotificationDeliveryPending    = "pending"
	NotificationDeliveryRetry      = "retry"
	NotificationDeliverySent       = "sent"
	NotificationDeliveryDeadLetter = "dead_letter"

	NotificationTemplateDraft     = "draft"
	NotificationTemplatePublished = "published"
	NotificationTemplateArchived  = "archived"

	NotificationDigestOff    = "off"
	NotificationDigestDaily  = "daily"
	NotificationDigestWeekly = "weekly"

	NotificationEventTaskAssigned     = "task.assigned"
	NotificationEventTaskWatcherAdded = "task.watcher.added"
	NotificationEventTaskMention      = "task.mention"
	NotificationEventTaskDueSoon      = "task.due_soon"
	NotificationEventTaskOverdue      = "task.overdue"
	NotificationEventWorkflowApproval = "workflow.approval"
	NotificationEventDigestSummary    = "digest.summary"
)

type NotificationPreference struct {
	UserID            int64           `json:"user_id"`
	Locale            string          `json:"locale"`
	Timezone          string          `json:"timezone"`
	QuietHoursEnabled bool            `json:"quiet_hours_enabled"`
	QuietStart        string          `json:"quiet_start,omitempty"`
	QuietEnd          string          `json:"quiet_end,omitempty"`
	DigestFrequency   string          `json:"digest_frequency"`
	DigestHour        int             `json:"digest_hour"`
	Channels          map[string]bool `json:"channels"`
	Events            map[string]bool `json:"events"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type UpdateNotificationPreferenceRequest struct {
	Locale            string          `json:"locale"`
	Timezone          string          `json:"timezone"`
	QuietHoursEnabled bool            `json:"quiet_hours_enabled"`
	QuietStart        string          `json:"quiet_start,omitempty"`
	QuietEnd          string          `json:"quiet_end,omitempty"`
	DigestFrequency   string          `json:"digest_frequency"`
	DigestHour        int             `json:"digest_hour"`
	Channels          map[string]bool `json:"channels,omitempty"`
	Events            map[string]bool `json:"events,omitempty"`
}

type NotificationEndpoint struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"user_id"`
	Channel   string     `json:"channel"`
	Address   string     `json:"address"`
	Secret    string     `json:"-"`
	Active    bool       `json:"active"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

type CreateNotificationEndpointRequest struct {
	Channel string `json:"channel"`
	Address string `json:"address"`
	Secret  string `json:"secret,omitempty"`
}

type NotificationTemplate struct {
	ID              int64      `json:"id"`
	OrganizationID  *int64     `json:"organization_id,omitempty"`
	Key             string     `json:"key"`
	Locale          string     `json:"locale"`
	Channel         string     `json:"channel"`
	Version         int        `json:"version"`
	Status          string     `json:"status"`
	Subject         string     `json:"subject"`
	Body            string     `json:"body"`
	CreatedByUserID *int64     `json:"created_by_user_id,omitempty"`
	PublishedByID   *int64     `json:"published_by_user_id,omitempty"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type CreateNotificationTemplateRequest struct {
	Key     string `json:"key"`
	Locale  string `json:"locale"`
	Channel string `json:"channel"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type Notification struct {
	ID             int64          `json:"id"`
	OrganizationID *int64         `json:"organization_id,omitempty"`
	WorkspaceID    *int64         `json:"workspace_id,omitempty"`
	UserID         int64          `json:"user_id"`
	EventType      string         `json:"event_type"`
	TemplateKey    string         `json:"template_key"`
	Title          string         `json:"title"`
	Body           string         `json:"body"`
	Data           map[string]any `json:"data"`
	DedupeKey      string         `json:"dedupe_key"`
	ReadAt         *time.Time     `json:"read_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type NotificationDelivery struct {
	ID             int64      `json:"id"`
	NotificationID int64      `json:"notification_id"`
	UserID         int64      `json:"user_id"`
	Channel        string     `json:"channel"`
	Destination    string     `json:"destination"`
	EndpointID     *int64     `json:"endpoint_id,omitempty"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	MaxAttempts    int        `json:"max_attempts"`
	AvailableAt    time.Time  `json:"available_at"`
	LastError      string     `json:"last_error,omitempty"`
	SentAt         *time.Time `json:"sent_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type NotificationDeliveryDetail struct {
	Delivery     NotificationDelivery `json:"delivery"`
	Notification Notification         `json:"notification"`
}

type NotificationList struct {
	Items  []Notification `json:"items"`
	Unread int64          `json:"unread"`
}

type NotificationReminderCandidate struct {
	WorkspaceID int64
	TaskID      int64
	UserID      int64
	TaskTitle   string
	DueAt       time.Time
}

type NotificationApprovalCandidate struct {
	OrganizationID int64
	WorkflowID     int64
	ExecutionID    int64
	ApprovalID     int64
	UserID         int64
	WorkflowName   string
	NodeID         string
}

type NotificationDigestCandidate struct {
	UserID int64
	Count  int64
}

type NotificationCreate struct {
	OrganizationID *int64
	WorkspaceID    *int64
	UserID         int64
	EventType      string
	TemplateKey    string
	Data           map[string]any
	DedupeKey      string
}

type RetryNotificationDeliveryRequest struct{}
