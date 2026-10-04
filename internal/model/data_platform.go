package model

import "time"

const (
	DataWarehouseBigQuery   = "bigquery"
	DataWarehouseSnowflake  = "snowflake"
	DataWarehouseRedshift   = "redshift"
	DataWarehouseDatabricks = "databricks"

	BIContractPowerBI = "powerbi"
	BIContractTableau = "tableau"
	BIContractLooker  = "looker"

	DataPlatformStatusActive   = "active"
	DataPlatformStatusDisabled = "disabled"

	DataExportModeIncremental = "incremental"
	DataExportModeFull        = "full"

	DataExportPending   = "pending"
	DataExportRunning   = "running"
	DataExportSucceeded = "succeeded"
	DataExportFailed    = "failed"

	MaskingNone   = "none"
	MaskingRedact = "redact"
	MaskingHash   = "hash"

	SchemaCompatibilityBackward = "backward"
	SchemaStatusActive           = "active"
	SchemaStatusDeprecated       = "deprecated"
)

type DataMaskingPolicy struct {
	Mode   string   `json:"mode"`
	Fields []string `json:"fields"`
}

type DataPlatformConnection struct {
	ID                  int64             `json:"id"`
	OrganizationID      int64             `json:"organization_id"`
	Name                string            `json:"name"`
	Provider            string            `json:"provider"`
	Target              string            `json:"target"`
	BIContracts         []string          `json:"bi_contracts"`
	Config              map[string]string `json:"config"`
	SecretRef           string            `json:"secret_ref,omitempty"`
	Status              string            `json:"status"`
	Masking             DataMaskingPolicy `json:"masking"`
	FreshnessSLOMinutes int                `json:"freshness_slo_minutes"`
	MaxMonthlyCostUSD   float64            `json:"max_monthly_cost_usd"`
	CreatedByUserID     int64              `json:"created_by_user_id"`
	CreatedAt           time.Time          `json:"created_at"`
	UpdatedAt           time.Time          `json:"updated_at"`
}

type CreateDataPlatformConnectionRequest struct {
	Name                string            `json:"name"`
	Provider            string            `json:"provider"`
	Target              string            `json:"target"`
	BIContracts         []string          `json:"bi_contracts"`
	Config              map[string]string `json:"config,omitempty"`
	SecretRef           string            `json:"secret_ref,omitempty"`
	Masking             DataMaskingPolicy `json:"masking"`
	FreshnessSLOMinutes int                `json:"freshness_slo_minutes"`
	MaxMonthlyCostUSD   float64            `json:"max_monthly_cost_usd"`
}

type DatasetField struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Nullable   bool     `json:"nullable"`
	Tags       []string `json:"tags,omitempty"`
	SourcePath string   `json:"source_path,omitempty"`
}

type DatasetSchemaVersion struct {
	ID              int64          `json:"id"`
	OrganizationID  int64          `json:"organization_id"`
	ConnectionID    int64          `json:"connection_id"`
	Version         int            `json:"version"`
	Compatibility   string         `json:"compatibility"`
	Status          string         `json:"status"`
	Fields          []DatasetField `json:"fields"`
	CreatedByUserID int64          `json:"created_by_user_id"`
	CreatedAt       time.Time      `json:"created_at"`
	DeprecatedAt    *time.Time     `json:"deprecated_at,omitempty"`
}

type CreateDatasetSchemaRequest struct {
	Fields []DatasetField `json:"fields"`
}

type DataExportCheckpoint struct {
	OrganizationID int64     `json:"organization_id"`
	ConnectionID   int64     `json:"connection_id"`
	Version        int       `json:"version"`
	LastUpdatedAt  time.Time `json:"last_updated_at"`
	LastRecordKey  string    `json:"last_record_key"`
	LastJobID      int64     `json:"last_job_id"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type DataExportJob struct {
	ID                 int64      `json:"id"`
	OrganizationID     int64      `json:"organization_id"`
	ConnectionID       int64      `json:"connection_id"`
	Mode               string     `json:"mode"`
	Status             string     `json:"status"`
	SchemaVersion      int        `json:"schema_version"`
	RequestedByUserID  int64      `json:"requested_by_user_id"`
	Rows               int        `json:"rows"`
	Bytes              int64      `json:"bytes"`
	EstimatedCostUSD   float64    `json:"estimated_cost_usd"`
	DeliveryURI        string     `json:"delivery_uri,omitempty"`
	PayloadHash        string     `json:"payload_hash,omitempty"`
	CheckpointBefore   string     `json:"checkpoint_before,omitempty"`
	CheckpointAfter    string     `json:"checkpoint_after,omitempty"`
	Error              string     `json:"error,omitempty"`
	StartedAt          time.Time  `json:"started_at"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

type RunDataExportRequest struct {
	Mode string `json:"mode,omitempty"`
}

type AnalyticsTaskRecord struct {
	OrganizationID int64      `json:"organization_id"`
	WorkspaceID    int64      `json:"workspace_id"`
	TaskID         int64      `json:"task_id"`
	Title          string     `json:"title"`
	Status         string     `json:"status"`
	Priority       string     `json:"priority"`
	ProjectID      *int64     `json:"project_id,omitempty"`
	DueAt          *time.Time `json:"due_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type DataLineageRecord struct {
	ID             int64          `json:"id"`
	OrganizationID int64          `json:"organization_id"`
	ConnectionID   int64          `json:"connection_id"`
	ExportJobID    int64          `json:"export_job_id"`
	SourceDatasets []string       `json:"source_datasets"`
	TargetDataset  string         `json:"target_dataset"`
	Fields         []DatasetField `json:"fields"`
	GovernanceTags []string       `json:"governance_tags"`
	CreatedAt      time.Time      `json:"created_at"`
}

type WarehouseAdapterCapability struct {
	Key              string   `json:"key"`
	DisplayName      string   `json:"display_name"`
	DeliveryMode     string   `json:"delivery_mode"`
	BIContracts      []string `json:"bi_contracts"`
	SupportsRecovery bool     `json:"supports_recovery"`
}

type WarehouseDeliveryReceipt struct {
	Provider    string `json:"provider"`
	Target      string `json:"target"`
	DeliveryURI string `json:"delivery_uri"`
	BatchID     string `json:"batch_id"`
	RowCount    int    `json:"row_count"`
	Bytes       int64  `json:"bytes"`
	PayloadHash string `json:"payload_hash"`
}

type BIExportContract struct {
	Key         string         `json:"key"`
	DisplayName string         `json:"display_name"`
	Dataset     string         `json:"dataset"`
	Version     int            `json:"version"`
	Fields      []DatasetField `json:"fields"`
	Notes       []string       `json:"notes"`
}

type ReverseETLHook struct {
	ID              int64             `json:"id"`
	OrganizationID  int64             `json:"organization_id"`
	ConnectionID    int64             `json:"connection_id"`
	Name            string            `json:"name"`
	SourceObject    string            `json:"source_object"`
	Destination     string            `json:"destination"`
	FieldMapping    map[string]string `json:"field_mapping"`
	Enabled         bool              `json:"enabled"`
	CreatedByUserID int64             `json:"created_by_user_id"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type CreateReverseETLHookRequest struct {
	ConnectionID int64             `json:"connection_id"`
	Name         string            `json:"name"`
	SourceObject string            `json:"source_object"`
	Destination  string            `json:"destination"`
	FieldMapping map[string]string `json:"field_mapping"`
	Enabled      bool              `json:"enabled"`
}

type DataPlatformDashboard struct {
	OrganizationID       int64                    `json:"organization_id"`
	Connections          int                      `json:"connections"`
	ActiveConnections    int                      `json:"active_connections"`
	SucceededExports     int                      `json:"succeeded_exports"`
	FailedExports        int                      `json:"failed_exports"`
	RowsExported         int64                    `json:"rows_exported"`
	EstimatedCostUSD     float64                  `json:"estimated_cost_usd"`
	FreshnessBreaches    int                      `json:"freshness_breaches"`
	LatestJobs           []DataExportJob          `json:"latest_jobs"`
	ConnectionFreshness  map[int64]time.Time       `json:"connection_freshness"`
	GeneratedAt          time.Time                `json:"generated_at"`
}
