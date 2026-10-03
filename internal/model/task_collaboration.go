package model

import "time"

const (
	TaskStatusBacklog    = "BACKLOG"
	TaskStatusTodo       = "TODO"
	TaskStatusInProgress = "IN_PROGRESS"
	TaskStatusBlocked    = "BLOCKED"
	TaskStatusInReview   = "IN_REVIEW"
	TaskStatusDone       = "DONE"
	TaskStatusArchived   = "ARCHIVED"

	TaskPriorityLow    = "LOW"
	TaskPriorityMedium = "MEDIUM"
	TaskPriorityHigh   = "HIGH"
	TaskPriorityUrgent = "URGENT"

	RecurrenceDaily   = "daily"
	RecurrenceWeekly  = "weekly"
	RecurrenceMonthly = "monthly"
)

type TaskProject struct {
	ID              int64      `json:"id"`
	WorkspaceID     int64      `json:"workspace_id"`
	Name            string     `json:"name"`
	Description     string     `json:"description,omitempty"`
	CreatedByUserID int64      `json:"created_by_user_id"`
	ArchivedAt      *time.Time `json:"archived_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type TaskList struct {
	ID              int64      `json:"id"`
	WorkspaceID     int64      `json:"workspace_id"`
	ProjectID       *int64     `json:"project_id,omitempty"`
	Name            string     `json:"name"`
	Position        int64      `json:"position"`
	CreatedByUserID int64      `json:"created_by_user_id"`
	ArchivedAt      *time.Time `json:"archived_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type TaskLabel struct {
	ID          int64     `json:"id"`
	WorkspaceID int64     `json:"workspace_id"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type TaskComment struct {
	ID          int64      `json:"id"`
	TaskID      int64      `json:"task_id"`
	WorkspaceID int64      `json:"workspace_id"`
	UserID      int64      `json:"user_id"`
	Body        string     `json:"body"`
	EditedAt    *time.Time `json:"edited_at,omitempty"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type TaskActivity struct {
	ID          int64          `json:"id"`
	WorkspaceID int64          `json:"workspace_id"`
	TaskID      int64          `json:"task_id"`
	ActorUserID *int64         `json:"actor_user_id,omitempty"`
	Action      string         `json:"action"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

type TaskDependency struct {
	TaskID          int64     `json:"task_id"`
	DependsOnTaskID int64     `json:"depends_on_task_id"`
	CreatedByUserID int64     `json:"created_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
}

type TaskRecurrenceRule struct {
	TaskID        int64      `json:"task_id"`
	Frequency     string     `json:"frequency"`
	IntervalCount int        `json:"interval_count"`
	Timezone      string     `json:"timezone"`
	NextRunAt     *time.Time `json:"next_run_at,omitempty"`
	Active        bool       `json:"active"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type TaskCustomFieldDefinition struct {
	ID          int64     `json:"id"`
	WorkspaceID int64     `json:"workspace_id"`
	Name        string    `json:"name"`
	FieldType   string    `json:"field_type"`
	Required    bool      `json:"required"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type TaskCustomFieldValue struct {
	TaskID    int64     `json:"task_id"`
	FieldID   int64     `json:"field_id"`
	Value     any       `json:"value"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateTaskProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type CreateTaskListRequest struct {
	ProjectID *int64 `json:"project_id,omitempty"`
	Name      string `json:"name"`
	Position  int64  `json:"position,omitempty"`
}

type CreateTaskLabelRequest struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

type CreateTaskCommentRequest struct {
	Body string `json:"body"`
}

type UpdateTaskCommentRequest struct {
	Body string `json:"body"`
}

type AddTaskUserRequest struct {
	UserID int64 `json:"user_id"`
}

type AddTaskDependencyRequest struct {
	DependsOnTaskID int64 `json:"depends_on_task_id"`
}

type SetTaskRecurrenceRequest struct {
	Frequency     string     `json:"frequency"`
	IntervalCount int        `json:"interval_count"`
	Timezone      string     `json:"timezone"`
	NextRunAt     *time.Time `json:"next_run_at,omitempty"`
	Active        bool       `json:"active"`
}

type CreateTaskCustomFieldRequest struct {
	Name      string `json:"name"`
	FieldType string `json:"field_type"`
	Required  bool   `json:"required"`
}

type SetTaskCustomFieldValueRequest struct {
	Value any `json:"value"`
}
