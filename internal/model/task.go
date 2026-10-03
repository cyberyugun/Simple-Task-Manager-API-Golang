package model

import "time"

type Task struct {
	ID                int64      `json:"id"`
	WorkspaceID       int64      `json:"workspace_id,omitempty"`
	UserID            int64      `json:"-"`
	PersonalWorkspace bool       `json:"-"`
	Title             string     `json:"title"`
	Description       string     `json:"description"`
	Completed         bool       `json:"completed"`
	Status            string     `json:"status"`
	Priority          string     `json:"priority"`
	StartAt           *time.Time `json:"start_at,omitempty"`
	DueAt             *time.Time `json:"due_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	ProjectID         *int64     `json:"project_id,omitempty"`
	ListID            *int64     `json:"list_id,omitempty"`
	ParentTaskID      *int64     `json:"parent_task_id,omitempty"`
	Position          int64      `json:"position"`
	EstimatedMinutes  *int       `json:"estimated_minutes,omitempty"`
	ActualMinutes     *int       `json:"actual_minutes,omitempty"`
	ArchivedAt        *time.Time `json:"archived_at,omitempty"`
	DeletedAt         *time.Time `json:"deleted_at,omitempty"`
	Version           int64      `json:"version"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type CreateTaskRequest struct {
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Status           string     `json:"status,omitempty"`
	Priority         string     `json:"priority,omitempty"`
	StartAt          *time.Time `json:"start_at,omitempty"`
	DueAt            *time.Time `json:"due_at,omitempty"`
	ProjectID        *int64     `json:"project_id,omitempty"`
	ListID           *int64     `json:"list_id,omitempty"`
	ParentTaskID     *int64     `json:"parent_task_id,omitempty"`
	Position         int64      `json:"position,omitempty"`
	EstimatedMinutes *int       `json:"estimated_minutes,omitempty"`
}

type UpdateTaskRequest struct {
	Title            string     `json:"title"`
	Description      string     `json:"description"`
	Status           string     `json:"status,omitempty"`
	Priority         string     `json:"priority,omitempty"`
	StartAt          *time.Time `json:"start_at,omitempty"`
	DueAt            *time.Time `json:"due_at,omitempty"`
	ProjectID        *int64     `json:"project_id,omitempty"`
	ListID           *int64     `json:"list_id,omitempty"`
	ParentTaskID     *int64     `json:"parent_task_id,omitempty"`
	Position         int64      `json:"position,omitempty"`
	EstimatedMinutes *int       `json:"estimated_minutes,omitempty"`
	ActualMinutes    *int       `json:"actual_minutes,omitempty"`
	Version          int64      `json:"version,omitempty"`
}

type TaskQuery struct {
	Page      int
	Limit     int
	Search    string
	Completed *bool
	Status    string
	Priority  string
	ProjectID *int64
	ListID    *int64
	AssigneeID *int64
	LabelID   *int64
	Archived  *bool
	Deleted   *bool
	Sort      string
	Order     string
}

type Pagination struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type TaskPage struct {
	Items      []Task     `json:"items"`
	Pagination Pagination `json:"pagination"`
}
