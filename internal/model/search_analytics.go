package model

import "time"

const (
	ReportFormatJSON = "json"
	ReportFormatCSV  = "csv"

	ReportFrequencyDaily  = "daily"
	ReportFrequencyWeekly = "weekly"

	ReportRunPending   = "pending"
	ReportRunRunning   = "running"
	ReportRunSucceeded = "succeeded"
	ReportRunFailed    = "failed"
)

type SearchQuery struct {
	Query      string `json:"query,omitempty"`
	Status     string `json:"status,omitempty"`
	Priority   string `json:"priority,omitempty"`
	ProjectID  *int64 `json:"project_id,omitempty"`
	AssigneeID *int64 `json:"assignee_id,omitempty"`
	Page       int    `json:"page"`
	Limit      int    `json:"limit"`
}

type SearchHit struct {
	Task      Task    `json:"task"`
	Rank      float64 `json:"rank"`
	Headline  string  `json:"headline,omitempty"`
	MatchType string  `json:"match_type,omitempty"`
}

type SearchPage struct {
	Items      []SearchHit `json:"items"`
	Pagination Pagination  `json:"pagination"`
}

type SavedSearchView struct {
	ID          int64          `json:"id"`
	WorkspaceID int64          `json:"workspace_id"`
	UserID      int64          `json:"user_id"`
	Name        string         `json:"name"`
	Query       map[string]any `json:"query"`
	Shared      bool           `json:"shared"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type CreateSavedSearchViewRequest struct {
	Name   string         `json:"name"`
	Query  map[string]any `json:"query"`
	Shared bool           `json:"shared"`
}

type AnalyticsSummary struct {
	TotalTasks           int64   `json:"total_tasks"`
	OpenTasks            int64   `json:"open_tasks"`
	CompletedTasks       int64   `json:"completed_tasks"`
	OverdueTasks         int64   `json:"overdue_tasks"`
	CompletionRate       float64 `json:"completion_rate"`
	AverageCycleHours    float64 `json:"average_cycle_hours"`
	AverageLeadTimeHours float64 `json:"average_lead_time_hours"`
}

type WorkloadMetric struct {
	UserID           int64  `json:"user_id"`
	Name             string `json:"name,omitempty"`
	OpenTasks        int64  `json:"open_tasks"`
	OverdueTasks     int64  `json:"overdue_tasks"`
	EstimatedMinutes int64  `json:"estimated_minutes"`
	ActualMinutes    int64  `json:"actual_minutes"`
}

type TrendMetric struct {
	Date      string `json:"date"`
	Created   int64  `json:"created"`
	Completed int64  `json:"completed"`
	Overdue   int64  `json:"overdue"`
}

type AnalyticsDashboard struct {
	Summary    AnalyticsSummary `json:"summary"`
	ByStatus   map[string]int64 `json:"by_status"`
	ByPriority map[string]int64 `json:"by_priority"`
	Workload   []WorkloadMetric `json:"workload"`
	Trend      []TrendMetric    `json:"trend"`
	Days       int              `json:"days"`
}

type ReportTaskRow struct {
	ID               int64      `json:"id"`
	Title            string     `json:"title"`
	Status           string     `json:"status"`
	Priority         string     `json:"priority"`
	ProjectID        *int64     `json:"project_id,omitempty"`
	DueAt            *time.Time `json:"due_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	EstimatedMinutes *int       `json:"estimated_minutes,omitempty"`
	ActualMinutes    *int       `json:"actual_minutes,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type ScheduledReport struct {
	ID          int64          `json:"id"`
	WorkspaceID int64          `json:"workspace_id"`
	UserID      int64          `json:"user_id"`
	Name        string         `json:"name"`
	Format      string         `json:"format"`
	Frequency   string         `json:"frequency"`
	Timezone    string         `json:"timezone"`
	Hour        int            `json:"hour"`
	Filters     map[string]any `json:"filters"`
	Active      bool           `json:"active"`
	NextRunAt   time.Time      `json:"next_run_at"`
	LastRunAt   *time.Time     `json:"last_run_at,omitempty"`
	LastStatus  string         `json:"last_status,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type CreateScheduledReportRequest struct {
	Name      string         `json:"name"`
	Format    string         `json:"format"`
	Frequency string         `json:"frequency"`
	Timezone  string         `json:"timezone"`
	Hour      int            `json:"hour"`
	Filters   map[string]any `json:"filters,omitempty"`
	RunNow    bool           `json:"run_now,omitempty"`
}

type ReportRun struct {
	ID          int64      `json:"id"`
	ScheduleID  int64      `json:"schedule_id"`
	WorkspaceID int64      `json:"workspace_id"`
	Status      string     `json:"status"`
	Format      string     `json:"format"`
	RowCount    int        `json:"row_count"`
	Payload     []byte     `json:"-"`
	Error       string     `json:"error,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type ReportRunSummary struct {
	ID          int64      `json:"id"`
	ScheduleID  int64      `json:"schedule_id"`
	WorkspaceID int64      `json:"workspace_id"`
	Status      string     `json:"status"`
	Format      string     `json:"format"`
	RowCount    int        `json:"row_count"`
	Error       string     `json:"error,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}
