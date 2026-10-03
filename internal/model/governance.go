package model

import "time"

const (
	DataClassificationPublic       = "public"
	DataClassificationInternal     = "internal"
	DataClassificationConfidential = "confidential"
	DataClassificationRestricted   = "restricted"

	PrivacyRequestAccess  = "access"
	PrivacyRequestExport  = "export"
	PrivacyRequestDelete  = "delete"
	PrivacyRequestCorrect = "correct"

	PrivacyStatusPending   = "pending"
	PrivacyStatusCompleted = "completed"
	PrivacyStatusRejected  = "rejected"
)

type GovernancePolicy struct {
	WorkspaceID                 int64     `json:"workspace_id"`
	DefaultClassification       string    `json:"default_classification"`
	AuditRetentionDays          int       `json:"audit_retention_days"`
	OperationalRetentionDays    int       `json:"operational_retention_days"`
	PrivacyRequestSLAHours      int       `json:"privacy_request_sla_hours"`
	RequireDPA                  bool      `json:"require_dpa"`
	RestrictCrossRegionTransfer bool      `json:"restrict_cross_region_transfer"`
	AllowedDataRegions          []string  `json:"allowed_data_regions"`
	UpdatedByUserID             int64     `json:"updated_by_user_id"`
	UpdatedAt                   time.Time `json:"updated_at"`
}

type DataInventoryEntry struct {
	ID                   int64     `json:"id"`
	WorkspaceID          int64     `json:"workspace_id"`
	ResourceType         string    `json:"resource_type"`
	FieldName            string    `json:"field_name"`
	Classification       string    `json:"classification"`
	ContainsPersonalData bool      `json:"contains_personal_data"`
	DataRegion           string    `json:"data_region,omitempty"`
	RetentionDays        int       `json:"retention_days"`
	UpdatedByUserID      int64     `json:"updated_by_user_id"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type LegalHold struct {
	ID               int64      `json:"id"`
	WorkspaceID      int64      `json:"workspace_id"`
	Name             string     `json:"name"`
	Reason           string     `json:"reason"`
	ResourceType     string     `json:"resource_type"`
	ResourceID       string     `json:"resource_id,omitempty"`
	CreatedByUserID  int64      `json:"created_by_user_id"`
	CreatedAt        time.Time  `json:"created_at"`
	ReleasedByUserID *int64     `json:"released_by_user_id,omitempty"`
	ReleasedAt       *time.Time `json:"released_at,omitempty"`
}

type PrivacyRequest struct {
	ID                int64      `json:"id"`
	WorkspaceID       int64      `json:"workspace_id"`
	SubjectUserID     int64      `json:"subject_user_id"`
	Type              string     `json:"type"`
	Status            string     `json:"status"`
	Reason            string     `json:"reason,omitempty"`
	RequestedByUserID int64      `json:"requested_by_user_id"`
	RequestedAt       time.Time  `json:"requested_at"`
	DueAt             time.Time  `json:"due_at"`
	CompletedByUserID *int64     `json:"completed_by_user_id,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

type ComplianceEvidence struct {
	ID              int64          `json:"id"`
	WorkspaceID     int64          `json:"workspace_id"`
	Framework       string         `json:"framework"`
	Control         string         `json:"control"`
	EvidenceType    string         `json:"evidence_type"`
	Description     string         `json:"description"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedByUserID int64          `json:"created_by_user_id"`
	CreatedAt       time.Time      `json:"created_at"`
}

type UpdateGovernancePolicyRequest struct {
	DefaultClassification       string   `json:"default_classification"`
	AuditRetentionDays          int      `json:"audit_retention_days"`
	OperationalRetentionDays    int      `json:"operational_retention_days"`
	PrivacyRequestSLAHours      int      `json:"privacy_request_sla_hours"`
	RequireDPA                  bool     `json:"require_dpa"`
	RestrictCrossRegionTransfer bool     `json:"restrict_cross_region_transfer"`
	AllowedDataRegions          []string `json:"allowed_data_regions"`
}

type UpsertDataInventoryRequest struct {
	ResourceType         string `json:"resource_type"`
	FieldName            string `json:"field_name"`
	Classification       string `json:"classification"`
	ContainsPersonalData bool   `json:"contains_personal_data"`
	DataRegion           string `json:"data_region,omitempty"`
	RetentionDays        int    `json:"retention_days"`
}

type CreateLegalHoldRequest struct {
	Name         string `json:"name"`
	Reason       string `json:"reason"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id,omitempty"`
}

type CreatePrivacyRequestRequest struct {
	SubjectUserID int64  `json:"subject_user_id"`
	Type          string `json:"type"`
	Reason        string `json:"reason,omitempty"`
}

type CompletePrivacyRequestRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type CreateComplianceEvidenceRequest struct {
	Framework    string         `json:"framework"`
	Control      string         `json:"control"`
	EvidenceType string         `json:"evidence_type"`
	Description  string         `json:"description"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}
