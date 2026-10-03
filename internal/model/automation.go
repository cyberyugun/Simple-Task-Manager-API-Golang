package model

import "time"

const (
	AutomationTriggerOperationsAlert     = "operations_alert"
	AutomationTriggerBillingUsagePercent = "billing_usage_percent"

	AutomationComparatorEqual          = "eq"
	AutomationComparatorGreaterOrEqual = "gte"
	AutomationComparatorLessOrEqual    = "lte"

	AutomationActionOpenIncident = "operations.open_incident"

	AutomationApprovalAutomatic = "automatic"
	AutomationApprovalRequired  = "required"

	AutomationExecutionPendingApproval = "pending_approval"
	AutomationExecutionApproved        = "approved"
	AutomationExecutionRejected        = "rejected"
	AutomationExecutionRunning         = "running"
	AutomationExecutionSucceeded       = "succeeded"
	AutomationExecutionFailed          = "failed"
	AutomationExecutionSkipped         = "skipped"
)

type AutomationPolicy struct {
	ID              int64          `json:"id"`
	OrganizationID  int64          `json:"organization_id"`
	Name            string         `json:"name"`
	Enabled         bool           `json:"enabled"`
	TriggerType     string         `json:"trigger_type"`
	TriggerKey      string         `json:"trigger_key"`
	Comparator      string         `json:"comparator"`
	Threshold       int64          `json:"threshold"`
	ActionType      string         `json:"action_type"`
	ActionConfig    map[string]any `json:"action_config"`
	ApprovalMode    string         `json:"approval_mode"`
	CooldownMinutes int            `json:"cooldown_minutes"`
	CreatedByUserID int64          `json:"created_by_user_id"`
	UpdatedByUserID int64          `json:"updated_by_user_id"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type AutomationExecution struct {
	ID               int64          `json:"id"`
	OrganizationID   int64          `json:"organization_id"`
	PolicyID         int64          `json:"policy_id"`
	DedupeKey        string         `json:"-"`
	Status           string         `json:"status"`
	TriggerSnapshot  map[string]any `json:"trigger_snapshot"`
	ActionResult     map[string]any `json:"action_result,omitempty"`
	ErrorMessage     string         `json:"error_message,omitempty"`
	RequestedAt      time.Time      `json:"requested_at"`
	ApprovedAt       *time.Time     `json:"approved_at,omitempty"`
	ApprovedByUserID *int64         `json:"approved_by_user_id,omitempty"`
	RejectedAt       *time.Time     `json:"rejected_at,omitempty"`
	RejectedByUserID *int64         `json:"rejected_by_user_id,omitempty"`
	ExecutedAt       *time.Time     `json:"executed_at,omitempty"`
	CompletedAt      *time.Time     `json:"completed_at,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type CreateAutomationPolicyRequest struct {
	Name            string         `json:"name"`
	Enabled         *bool          `json:"enabled,omitempty"`
	TriggerType     string         `json:"trigger_type"`
	TriggerKey      string         `json:"trigger_key"`
	Comparator      string         `json:"comparator"`
	Threshold       int64          `json:"threshold"`
	ActionType      string         `json:"action_type"`
	ActionConfig    map[string]any `json:"action_config"`
	ApprovalMode    string         `json:"approval_mode"`
	CooldownMinutes int            `json:"cooldown_minutes"`
}

type UpdateAutomationPolicyRequest struct {
	Name            string         `json:"name"`
	Enabled         bool           `json:"enabled"`
	TriggerType     string         `json:"trigger_type"`
	TriggerKey      string         `json:"trigger_key"`
	Comparator      string         `json:"comparator"`
	Threshold       int64          `json:"threshold"`
	ActionType      string         `json:"action_type"`
	ActionConfig    map[string]any `json:"action_config"`
	ApprovalMode    string         `json:"approval_mode"`
	CooldownMinutes int            `json:"cooldown_minutes"`
}

type DecideAutomationExecutionRequest struct {
	Approve bool `json:"approve"`
}
