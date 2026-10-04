package model

import "time"

const (
	RegionMigrationPendingApproval = "pending_approval"
	RegionMigrationApproved        = "approved"
	RegionMigrationRejected        = "rejected"
	RegionMigrationRunning         = "running"
	RegionMigrationCompleted       = "completed"
	RegionMigrationFailed          = "failed"
	RegionMigrationCanceled        = "canceled"

	RegionTransferPendingApproval = "pending_approval"
	RegionTransferApproved        = "approved"
	RegionTransferRejected        = "rejected"
	RegionTransferCompleted       = "completed"

	FailoverExercisePlanned   = "planned"
	FailoverExerciseCompleted = "completed"
	FailoverExerciseFailed    = "failed"

	RegionalPlacementActive    = "active"
	RegionalPlacementMigrating = "migrating"
	RegionalPlacementDegraded  = "degraded"
)

type GlobalRegion struct {
	Key         string `json:"key"`
	Provider    string `json:"provider"`
	Geography   string `json:"geography"`
	DisplayName string `json:"display_name"`
}

type OrganizationRegionPolicy struct {
	OrganizationID             int64     `json:"organization_id"`
	HomeRegion                 string    `json:"home_region"`
	AllowedRegions             []string  `json:"allowed_regions"`
	FailoverRegions            []string  `json:"failover_regions"`
	DataResidencyEnforced      bool      `json:"data_residency_enforced"`
	CrossRegionApprovalRequired bool     `json:"cross_region_approval_required"`
	RPOSeconds                 int       `json:"rpo_seconds"`
	RTOSeconds                 int       `json:"rto_seconds"`
	UpdatedByUserID            int64     `json:"updated_by_user_id"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

type RegionalPlacement struct {
	ID                 int64      `json:"id"`
	OrganizationID     int64      `json:"organization_id"`
	ResourceType       string     `json:"resource_type"`
	ResourceID         string     `json:"resource_id"`
	PrimaryRegion      string     `json:"primary_region"`
	ReplicaRegions     []string   `json:"replica_regions"`
	Status             string     `json:"status"`
	LastReplicatedAt   *time.Time `json:"last_replicated_at,omitempty"`
	LastVerifiedAt     *time.Time `json:"last_verified_at,omitempty"`
	UpdatedByUserID    int64      `json:"updated_by_user_id"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type RegionMigration struct {
	ID               int64      `json:"id"`
	OrganizationID   int64      `json:"organization_id"`
	Scope            string     `json:"scope"`
	ResourceType     string     `json:"resource_type,omitempty"`
	ResourceID       string     `json:"resource_id,omitempty"`
	SourceRegion     string     `json:"source_region"`
	TargetRegion     string     `json:"target_region"`
	Status           string     `json:"status"`
	Reason           string     `json:"reason"`
	Checkpoint       map[string]any `json:"checkpoint,omitempty"`
	RequestedByUserID int64     `json:"requested_by_user_id"`
	RequestedAt      time.Time  `json:"requested_at"`
	DecidedByUserID  *int64     `json:"decided_by_user_id,omitempty"`
	DecisionComment  string     `json:"decision_comment,omitempty"`
	DecidedAt        *time.Time `json:"decided_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type CrossRegionTransfer struct {
	ID                int64      `json:"id"`
	OrganizationID    int64      `json:"organization_id"`
	ResourceType      string     `json:"resource_type"`
	ResourceID        string     `json:"resource_id"`
	Classification    string     `json:"classification"`
	SourceRegion      string     `json:"source_region"`
	TargetRegion      string     `json:"target_region"`
	Status            string     `json:"status"`
	Reason            string     `json:"reason"`
	RequestedByUserID int64      `json:"requested_by_user_id"`
	RequestedAt       time.Time  `json:"requested_at"`
	DecidedByUserID   *int64     `json:"decided_by_user_id,omitempty"`
	DecisionComment   string     `json:"decision_comment,omitempty"`
	DecidedAt         *time.Time `json:"decided_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type FailoverExercise struct {
	ID                 int64      `json:"id"`
	OrganizationID     int64      `json:"organization_id"`
	SourceRegion       string     `json:"source_region"`
	TargetRegion       string     `json:"target_region"`
	Status             string     `json:"status"`
	Notes              string     `json:"notes,omitempty"`
	AchievedRPOSeconds int        `json:"achieved_rpo_seconds"`
	AchievedRTOSeconds int        `json:"achieved_rto_seconds"`
	MeetsRPO           bool       `json:"meets_rpo"`
	MeetsRTO           bool       `json:"meets_rto"`
	CreatedByUserID    int64      `json:"created_by_user_id"`
	CreatedAt          time.Time  `json:"created_at"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
}

type RegionRouteDecision struct {
	OrganizationID int64     `json:"organization_id"`
	PrimaryRegion  string    `json:"primary_region"`
	FailoverRegion string    `json:"failover_region,omitempty"`
	AllowedRegions []string  `json:"allowed_regions"`
	ResidencyEnforced bool   `json:"residency_enforced"`
	GeneratedAt    time.Time `json:"generated_at"`
}

type ResidencyViolation struct {
	WorkspaceID int64  `json:"workspace_id,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID string `json:"resource_id,omitempty"`
	Region string `json:"region"`
	Message string `json:"message"`
}

type RegionComplianceReport struct {
	OrganizationID int64                    `json:"organization_id"`
	Policy         OrganizationRegionPolicy `json:"policy"`
	Placements     []RegionalPlacement      `json:"placements"`
	Violations     []ResidencyViolation     `json:"violations"`
	Compliant      bool                     `json:"compliant"`
	GeneratedAt    time.Time                `json:"generated_at"`
}

type UpdateOrganizationRegionPolicyRequest struct {
	HomeRegion                  string   `json:"home_region"`
	AllowedRegions              []string `json:"allowed_regions"`
	FailoverRegions             []string `json:"failover_regions"`
	DataResidencyEnforced       bool     `json:"data_residency_enforced"`
	CrossRegionApprovalRequired bool     `json:"cross_region_approval_required"`
	RPOSeconds                  int      `json:"rpo_seconds"`
	RTOSeconds                  int      `json:"rto_seconds"`
}

type UpsertRegionalPlacementRequest struct {
	ResourceType     string   `json:"resource_type"`
	ResourceID       string   `json:"resource_id"`
	PrimaryRegion    string   `json:"primary_region"`
	ReplicaRegions   []string `json:"replica_regions"`
	Status           string   `json:"status,omitempty"`
}

type CreateRegionMigrationRequest struct {
	Scope        string `json:"scope"`
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`
	SourceRegion string `json:"source_region"`
	TargetRegion string `json:"target_region"`
	Reason       string `json:"reason"`
}

type DecideRegionMigrationRequest struct {
	Decision string `json:"decision"`
	Comment  string `json:"comment,omitempty"`
}

type CompleteRegionMigrationRequest struct {
	Success    bool           `json:"success"`
	Checkpoint map[string]any `json:"checkpoint,omitempty"`
}

type CreateCrossRegionTransferRequest struct {
	ResourceType   string `json:"resource_type"`
	ResourceID     string `json:"resource_id"`
	Classification string `json:"classification"`
	SourceRegion   string `json:"source_region"`
	TargetRegion   string `json:"target_region"`
	Reason         string `json:"reason"`
}

type DecideCrossRegionTransferRequest struct {
	Decision string `json:"decision"`
	Comment  string `json:"comment,omitempty"`
}

type CreateFailoverExerciseRequest struct {
	SourceRegion string `json:"source_region"`
	TargetRegion string `json:"target_region"`
	Notes        string `json:"notes,omitempty"`
}

type CompleteFailoverExerciseRequest struct {
	Success            bool `json:"success"`
	AchievedRPOSeconds int  `json:"achieved_rpo_seconds"`
	AchievedRTOSeconds int  `json:"achieved_rto_seconds"`
}
