package model

import "time"

type Task struct {
	ID                int64     `json:"id"`
	WorkspaceID       int64     `json:"workspace_id,omitempty"`
	UserID            int64     `json:"-"`
	PersonalWorkspace bool      `json:"-"`
	Title             string    `json:"title"`
	Description       string    `json:"description"`
	Completed         bool      `json:"completed"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type CreateTaskRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type UpdateTaskRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type TaskQuery struct {
	Page      int
	Limit     int
	Search    string
	Completed *bool
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
