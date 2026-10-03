package model

import "time"

const (
	WorkflowVersionDraft     = "draft"
	WorkflowVersionPublished = "published"
	WorkflowVersionArchived  = "archived"

	WorkflowNodeTrigger     = "trigger"
	WorkflowNodeCondition   = "condition"
	WorkflowNodeTransform   = "transform"
	WorkflowNodeApproval    = "approval"
	WorkflowNodeAction      = "action"
	WorkflowNodeDelay       = "delay"
	WorkflowNodeBranch      = "branch"
	WorkflowNodeParallel    = "parallel"
	WorkflowNodeJoin        = "join"
	WorkflowNodeSubworkflow = "subworkflow"
	WorkflowNodeEnd         = "end"

	WorkflowTriggerManual    = "manual"
	WorkflowTriggerEvent     = "event"
	WorkflowTriggerScheduled = "scheduled"
	WorkflowTriggerConnector = "connector"
	WorkflowTriggerInternal  = "internal"

	WorkflowExecutionRunning         = "running"
	WorkflowExecutionWaitingApproval = "waiting_approval"
	WorkflowExecutionWaitingDelay    = "waiting_delay"
	WorkflowExecutionSucceeded       = "succeeded"
	WorkflowExecutionFailed          = "failed"
	WorkflowExecutionCancelled       = "cancelled"
	WorkflowExecutionCompensating    = "compensating"
	WorkflowExecutionCompensated     = "compensated"

	WorkflowNodeExecutionRunning      = "running"
	WorkflowNodeExecutionWaiting      = "waiting"
	WorkflowNodeExecutionRetryWaiting = "retry_wait"
	WorkflowNodeExecutionSucceeded    = "succeeded"
	WorkflowNodeExecutionFailed       = "failed"
	WorkflowNodeExecutionSkipped      = "skipped"
	WorkflowNodeExecutionCompensated  = "compensated"

	WorkflowApprovalPending  = "pending"
	WorkflowApprovalApproved = "approved"
	WorkflowApprovalRejected = "rejected"
)

type WorkflowDefinition struct {
	ID              int64     `json:"id"`
	OrganizationID  int64     `json:"organization_id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	ActiveVersionID *int64    `json:"active_version_id,omitempty"`
	CreatedByUserID int64     `json:"created_by_user_id"`
	UpdatedByUserID int64     `json:"updated_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type WorkflowVersion struct {
	ID              int64         `json:"id"`
	OrganizationID  int64         `json:"organization_id"`
	WorkflowID      int64         `json:"workflow_id"`
	Version         int           `json:"version"`
	Status          string        `json:"status"`
	Graph           WorkflowGraph `json:"graph"`
	Checksum        string        `json:"checksum,omitempty"`
	CreatedByUserID int64         `json:"created_by_user_id"`
	PublishedByID   *int64        `json:"published_by_user_id,omitempty"`
	PublishedAt     *time.Time    `json:"published_at,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

type WorkflowGraph struct {
	Nodes []WorkflowNode `json:"nodes"`
	Edges []WorkflowEdge `json:"edges"`
}

type WorkflowNode struct {
	ID                 string            `json:"id"`
	Type               string            `json:"type"`
	Name               string            `json:"name,omitempty"`
	Config             map[string]any    `json:"config,omitempty"`
	InputMapping       map[string]string `json:"input_mapping,omitempty"`
	OutputMapping      map[string]string `json:"output_mapping,omitempty"`
	Retry              WorkflowRetry     `json:"retry,omitempty"`
	TimeoutSeconds     int               `json:"timeout_seconds,omitempty"`
	CompensationNodeID string            `json:"compensation_node_id,omitempty"`
}

type WorkflowEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	When string `json:"when,omitempty"`
}

type WorkflowRetry struct {
	MaxAttempts    int `json:"max_attempts,omitempty"`
	BackoffSeconds int `json:"backoff_seconds,omitempty"`
}

type WorkflowExecution struct {
	ID                int64          `json:"id"`
	OrganizationID    int64          `json:"organization_id"`
	WorkflowID        int64          `json:"workflow_id"`
	WorkflowVersionID int64          `json:"workflow_version_id"`
	Status            string         `json:"status"`
	TriggerType       string         `json:"trigger_type"`
	TriggerKey        string         `json:"trigger_key,omitempty"`
	TriggerPayload    map[string]any `json:"trigger_payload"`
	Variables         map[string]any `json:"variables"`
	WorkflowSnapshot  WorkflowGraph  `json:"workflow_snapshot"`
	NextNodeIDs       []string       `json:"next_node_ids"`
	DryRun            bool           `json:"dry_run"`
	ErrorMessage      string         `json:"error_message,omitempty"`
	RequestedByUserID *int64         `json:"requested_by_user_id,omitempty"`
	ResumeAt          *time.Time     `json:"resume_at,omitempty"`
	StartedAt         time.Time      `json:"started_at"`
	CompletedAt       *time.Time     `json:"completed_at,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type WorkflowNodeExecution struct {
	ID           int64          `json:"id"`
	ExecutionID  int64          `json:"execution_id"`
	NodeID       string         `json:"node_id"`
	NodeType     string         `json:"node_type"`
	Status       string         `json:"status"`
	Attempt      int            `json:"attempt"`
	Input        map[string]any `json:"input"`
	Output       map[string]any `json:"output"`
	ErrorMessage string         `json:"error_message,omitempty"`
	RetryAt      *time.Time     `json:"retry_at,omitempty"`
	StartedAt    time.Time      `json:"started_at"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type WorkflowApproval struct {
	ID              int64      `json:"id"`
	OrganizationID  int64      `json:"organization_id"`
	ExecutionID     int64      `json:"execution_id"`
	NodeID          string     `json:"node_id"`
	Status          string     `json:"status"`
	RequestedAt     time.Time  `json:"requested_at"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
	DecidedByUserID *int64     `json:"decided_by_user_id,omitempty"`
	Comment         string     `json:"comment,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type WorkflowCheckpoint struct {
	ID          int64          `json:"id"`
	ExecutionID int64          `json:"execution_id"`
	Sequence    int            `json:"sequence"`
	Status      string         `json:"status"`
	NodeID      string         `json:"node_id,omitempty"`
	Variables   map[string]any `json:"variables"`
	NextNodeIDs []string       `json:"next_node_ids"`
	CreatedAt   time.Time      `json:"created_at"`
}

type WorkflowExecutionDetail struct {
	Execution   WorkflowExecution       `json:"execution"`
	Nodes       []WorkflowNodeExecution `json:"nodes"`
	Approvals   []WorkflowApproval      `json:"approvals"`
	Checkpoints []WorkflowCheckpoint    `json:"checkpoints"`
}

type WorkflowNodeSchema struct {
	Type         string         `json:"type"`
	Description  string         `json:"description"`
	ConfigSchema map[string]any `json:"config_schema"`
}

type CreateWorkflowRequest struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Graph       WorkflowGraph `json:"graph"`
}

type UpdateWorkflowDraftRequest struct {
	Graph WorkflowGraph `json:"graph"`
}

type CreateWorkflowDraftRequest struct {
	FromVersionID *int64 `json:"from_version_id,omitempty"`
}

type StartWorkflowExecutionRequest struct {
	TriggerType    string         `json:"trigger_type"`
	TriggerKey     string         `json:"trigger_key,omitempty"`
	TriggerPayload map[string]any `json:"trigger_payload,omitempty"`
	Variables      map[string]any `json:"variables,omitempty"`
	DryRun         bool           `json:"dry_run"`
}

type TriggerWorkflowRequest struct {
	TriggerType string         `json:"trigger_type"`
	TriggerKey  string         `json:"trigger_key,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
	DryRun      bool           `json:"dry_run"`
}

type DecideWorkflowApprovalRequest struct {
	Approve bool   `json:"approve"`
	Comment string `json:"comment,omitempty"`
}

type RetryWorkflowExecutionRequest struct {
	NodeID string `json:"node_id,omitempty"`
}
