package model

import "time"

const (
	LifecycleRunRunning   = "running"
	LifecycleRunCompleted = "completed"
	LifecycleRunSkipped   = "skipped"
	LifecycleRunFailed    = "failed"

	ConsentGranted   = "granted"
	ConsentWithdrawn = "withdrawn"
)

type LifecycleRun struct {
	ID                int64      `json:"id"`
	WorkspaceID       int64      `json:"workspace_id"`
	Status            string     `json:"status"`
	ArchivedCount     int        `json:"archived_count"`
	PurgedCount       int        `json:"purged_count"`
	SkippedReason     string     `json:"skipped_reason,omitempty"`
	TriggeredByUserID *int64     `json:"triggered_by_user_id,omitempty"`
	StartedAt         time.Time  `json:"started_at"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
}

type PrivacyExportPackage struct {
	ID              int64          `json:"id"`
	WorkspaceID     int64          `json:"workspace_id"`
	PrivacyRequestID int64         `json:"privacy_request_id"`
	SubjectUserID   int64          `json:"subject_user_id"`
	ChecksumSHA256  string         `json:"checksum_sha256"`
	Payload         map[string]any `json:"payload"`
	CreatedByUserID int64          `json:"created_by_user_id"`
	CreatedAt       time.Time      `json:"created_at"`
}

type ConsentRecord struct {
	ID              int64     `json:"id"`
	WorkspaceID     int64     `json:"workspace_id"`
	SubjectUserID   int64     `json:"subject_user_id"`
	Purpose         string    `json:"purpose"`
	Status          string    `json:"status"`
	PolicyVersion   string    `json:"policy_version"`
	Source          string    `json:"source"`
	RecordedByUserID int64    `json:"recorded_by_user_id"`
	RecordedAt      time.Time `json:"recorded_at"`
}

type CreateConsentRequest struct {
	SubjectUserID int64  `json:"subject_user_id"`
	Purpose       string `json:"purpose"`
	Status        string `json:"status"`
	PolicyVersion string `json:"policy_version"`
	Source        string `json:"source"`
}

type GovernanceReport struct {
	WorkspaceID             int64             `json:"workspace_id"`
	DefaultClassification   string            `json:"default_classification"`
	OperationalRetentionDays int              `json:"operational_retention_days"`
	DataInventoryEntries    int               `json:"data_inventory_entries"`
	PersonalDataEntries     int               `json:"personal_data_entries"`
	ActiveLegalHolds        int               `json:"active_legal_holds"`
	PendingPrivacyRequests  int               `json:"pending_privacy_requests"`
	OverduePrivacyRequests  int               `json:"overdue_privacy_requests"`
	ConsentRecords          int               `json:"consent_records"`
	LatestLifecycleRun      *LifecycleRun     `json:"latest_lifecycle_run,omitempty"`
	GeneratedAt             time.Time         `json:"generated_at"`
}
