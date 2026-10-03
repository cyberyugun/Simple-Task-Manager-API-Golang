package model

import "time"

const (
	OperationsSupportStandard   = "standard"
	OperationsSupportPriority   = "priority"
	OperationsSupportEnterprise = "enterprise"

	OperationalAlertOpen         = "open"
	OperationalAlertAcknowledged = "acknowledged"

	MaintenanceStatusScheduled = "scheduled"
	MaintenanceStatusCanceled  = "canceled"
	MaintenanceStatusCompleted = "completed"

	IncidentStatusOpen       = "open"
	IncidentStatusMonitoring = "monitoring"
	IncidentStatusResolved   = "resolved"

	IncidentSeverity1 = "sev1"
	IncidentSeverity2 = "sev2"
	IncidentSeverity3 = "sev3"
	IncidentSeverity4 = "sev4"
)

type OperationsPolicy struct {
	OrganizationID               int64     `json:"organization_id"`
	MonthlyBudgetCents           int64     `json:"monthly_budget_cents"`
	BudgetAlertThresholdPercent  int       `json:"budget_alert_threshold_percent"`
	SLOTargetBasisPoints         int       `json:"slo_target_basis_points"`
	IncidentEscalationMinutes    int       `json:"incident_escalation_minutes"`
	SupportTier                  string    `json:"support_tier"`
	SupportContact               string    `json:"support_contact,omitempty"`
	UpdatedByUserID              int64     `json:"updated_by_user_id"`
	CreatedAt                    time.Time `json:"created_at"`
	UpdatedAt                    time.Time `json:"updated_at"`
}

type CostAllocation struct {
	ID              int64          `json:"id"`
	OrganizationID  int64          `json:"organization_id"`
	Category        string         `json:"category"`
	Source          string         `json:"source"`
	AmountCents     int64          `json:"amount_cents"`
	Currency        string         `json:"currency"`
	PeriodStart     time.Time      `json:"period_start"`
	PeriodEnd       time.Time      `json:"period_end"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedByUserID int64          `json:"created_by_user_id"`
	CreatedAt       time.Time      `json:"created_at"`
}

type OperationalAlert struct {
	ID                   int64      `json:"id"`
	OrganizationID       int64      `json:"organization_id"`
	Fingerprint          string     `json:"-"`
	Type                 string     `json:"type"`
	Metric               string     `json:"metric"`
	Status               string     `json:"status"`
	Message              string     `json:"message"`
	CurrentValue         int64      `json:"current_value"`
	ThresholdValue       int64      `json:"threshold_value"`
	DetectedAt           time.Time  `json:"detected_at"`
	AcknowledgedAt       *time.Time `json:"acknowledged_at,omitempty"`
	AcknowledgedByUserID *int64     `json:"acknowledged_by_user_id,omitempty"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type MaintenanceWindow struct {
	ID              int64     `json:"id"`
	OrganizationID  int64     `json:"organization_id"`
	Title           string    `json:"title"`
	Description     string    `json:"description,omitempty"`
	Status          string    `json:"status"`
	StartsAt        time.Time `json:"starts_at"`
	EndsAt          time.Time `json:"ends_at"`
	CreatedByUserID int64     `json:"created_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type OperationalIncident struct {
	ID              int64      `json:"id"`
	OrganizationID  int64      `json:"organization_id"`
	Title           string     `json:"title"`
	Severity        string     `json:"severity"`
	Status          string     `json:"status"`
	Summary         string     `json:"summary,omitempty"`
	StartedAt       time.Time  `json:"started_at"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	CreatedByUserID int64      `json:"created_by_user_id"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type SLOStatus struct {
	PeriodStart             time.Time `json:"period_start"`
	PeriodEnd               time.Time `json:"period_end"`
	TargetBasisPoints       int       `json:"target_basis_points"`
	AvailabilityBasisPoints int       `json:"availability_basis_points"`
	DowntimeSeconds         int64     `json:"downtime_seconds"`
	WithinTarget            bool      `json:"within_target"`
}

type CapacityForecast struct {
	Metric                string `json:"metric"`
	CurrentValue          int64  `json:"current_value"`
	ProjectedPeriodValue  int64  `json:"projected_period_value"`
	Limit                 int64  `json:"limit"`
	ProjectedUtilization  int    `json:"projected_utilization_percent"`
}

type OperationsDashboard struct {
	Policy                 OperationsPolicy      `json:"policy"`
	CurrentSpendCents      int64                 `json:"current_spend_cents"`
	ProjectedSpendCents    int64                 `json:"projected_spend_cents"`
	BudgetUtilization      int                   `json:"budget_utilization_percent"`
	OpenAlerts             []OperationalAlert    `json:"open_alerts"`
	OpenIncidents          []OperationalIncident `json:"open_incidents"`
	UpcomingMaintenance    []MaintenanceWindow   `json:"upcoming_maintenance"`
	SLO                    SLOStatus              `json:"slo"`
	CapacityForecasts      []CapacityForecast     `json:"capacity_forecasts"`
	GeneratedAt            time.Time              `json:"generated_at"`
}

type UpdateOperationsPolicyRequest struct {
	MonthlyBudgetCents          int64  `json:"monthly_budget_cents"`
	BudgetAlertThresholdPercent int    `json:"budget_alert_threshold_percent"`
	SLOTargetBasisPoints        int    `json:"slo_target_basis_points"`
	IncidentEscalationMinutes   int    `json:"incident_escalation_minutes"`
	SupportTier                 string `json:"support_tier"`
	SupportContact              string `json:"support_contact,omitempty"`
}

type CreateCostAllocationRequest struct {
	Category    string         `json:"category"`
	Source      string         `json:"source"`
	AmountCents int64          `json:"amount_cents"`
	PeriodStart time.Time      `json:"period_start"`
	PeriodEnd   time.Time      `json:"period_end"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type CreateMaintenanceWindowRequest struct {
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
}

type UpdateMaintenanceWindowRequest struct {
	Status string `json:"status"`
}

type CreateOperationalIncidentRequest struct {
	Title     string    `json:"title"`
	Severity  string    `json:"severity"`
	Summary   string    `json:"summary,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

type UpdateOperationalIncidentRequest struct {
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
}
