package model

import "time"

const (
	ZeroTrustActionAllow  = "allow"
	ZeroTrustActionStepUp = "step_up"
	ZeroTrustActionRevoke = "revoke"

	RiskLevelLow      = "low"
	RiskLevelMedium   = "medium"
	RiskLevelHigh     = "high"
	RiskLevelCritical = "critical"

	WorkloadIdentityActive  = "active"
	WorkloadIdentityRevoked = "revoked"

	SecurityEventRiskAssessment   = "risk_assessment"
	SecurityEventImpossibleTravel = "impossible_travel"
	SecurityEventNetworkViolation = "network_violation"
	SecurityEventDPoPViolation    = "token_binding_violation"
	SecurityEventWAFAnomaly       = "waf_anomaly"
	SecurityEventWorkloadAuth     = "workload_authentication"
	SecurityEventCertificate      = "certificate_rotation"
	SecurityEventSessionRevoked   = "high_risk_session_revoked"
)

type ZeroTrustPolicy struct {
	OrganizationID          int64     `json:"organization_id"`
	Enabled                 bool      `json:"enabled"`
	RequireWorkloadMTLS     bool      `json:"require_workload_mtls"`
	RequireBoundTokens      bool      `json:"require_bound_tokens"`
	StepUpRiskScore         int       `json:"step_up_risk_score"`
	RevokeRiskScore         int       `json:"revoke_risk_score"`
	ImpossibleTravelKPH     int       `json:"impossible_travel_kph"`
	WAFBlockScore           int       `json:"waf_block_score"`
	AllowedCIDRs            []string  `json:"allowed_cidrs"`
	DeniedCIDRs             []string  `json:"denied_cidrs"`
	AutoRevokeHighRisk      bool      `json:"auto_revoke_high_risk"`
	RequireTrustedDevice    bool      `json:"require_trusted_device"`
	SIEMFederationEnabled   bool      `json:"siem_federation_enabled"`
	AuditCheckpointInterval int       `json:"audit_checkpoint_interval"`
	UpdatedByUserID         int64     `json:"updated_by_user_id"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type WorkloadIdentity struct {
	ID                  int64      `json:"id"`
	OrganizationID      int64      `json:"organization_id"`
	WorkspaceID         int64      `json:"workspace_id"`
	Name                string     `json:"name"`
	SPIFFEID            string     `json:"spiffe_id"`
	Status              string     `json:"status"`
	AllowedScopes       []string   `json:"allowed_scopes"`
	MTLSRequired        bool       `json:"mtls_required"`
	CreatedByUserID     int64      `json:"created_by_user_id"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	RevokedAt           *time.Time `json:"revoked_at,omitempty"`
	LastAuthenticatedAt *time.Time `json:"last_authenticated_at,omitempty"`
}

type WorkloadCertificate struct {
	ID                int64      `json:"id"`
	OrganizationID    int64      `json:"organization_id"`
	WorkloadID        int64      `json:"workload_id"`
	SerialNumber      string     `json:"serial_number"`
	SHA256Fingerprint string     `json:"sha256_fingerprint"`
	Subject           string     `json:"subject"`
	NotBefore         time.Time  `json:"not_before"`
	NotAfter          time.Time  `json:"not_after"`
	CreatedByUserID   int64      `json:"created_by_user_id"`
	CreatedAt         time.Time  `json:"created_at"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
	ReplacedByID      *int64     `json:"replaced_by_id,omitempty"`
}

type DeviceTrust struct {
	ID             int64      `json:"id"`
	OrganizationID int64      `json:"organization_id"`
	UserID         int64      `json:"user_id"`
	DeviceHash     string     `json:"device_hash"`
	Label          string     `json:"label,omitempty"`
	TrustLevel     string     `json:"trust_level"`
	LastSeenAt     time.Time  `json:"last_seen_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	CreatedAt      time.Time  `json:"created_at"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
}

type SecurityEvent struct {
	ID             int64          `json:"id"`
	OrganizationID int64          `json:"organization_id"`
	UserID         *int64         `json:"user_id,omitempty"`
	SessionID      *int64         `json:"session_id,omitempty"`
	WorkloadID     *int64         `json:"workload_id,omitempty"`
	Type           string         `json:"type"`
	Severity       string         `json:"severity"`
	RiskScore      int            `json:"risk_score"`
	Action         string         `json:"action"`
	SourceIP       string         `json:"source_ip,omitempty"`
	Indicators     []string       `json:"indicators"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	OccurredAt     time.Time      `json:"occurred_at"`
}

type RiskEvaluationRequest struct {
	SessionID         int64      `json:"session_id,omitempty"`
	DeviceHash        string     `json:"device_hash,omitempty"`
	SourceIP          string     `json:"source_ip,omitempty"`
	PreviousLatitude  *float64   `json:"previous_latitude,omitempty"`
	PreviousLongitude *float64   `json:"previous_longitude,omitempty"`
	CurrentLatitude   *float64   `json:"current_latitude,omitempty"`
	CurrentLongitude  *float64   `json:"current_longitude,omitempty"`
	PreviousSeenAt    *time.Time `json:"previous_seen_at,omitempty"`
	AnomalyScore      int        `json:"anomaly_score,omitempty"`
	WAFScore          int        `json:"waf_score,omitempty"`
	TokenBindingValid *bool      `json:"token_binding_valid,omitempty"`
}

type RiskEvaluation struct {
	OrganizationID   int64     `json:"organization_id"`
	UserID           int64     `json:"user_id"`
	SessionID        int64     `json:"session_id,omitempty"`
	RiskScore        int       `json:"risk_score"`
	RiskLevel        string    `json:"risk_level"`
	Action           string    `json:"action"`
	Indicators       []string  `json:"indicators"`
	MFAAuthenticated bool      `json:"mfa_authenticated"`
	SessionRevoked   bool      `json:"session_revoked"`
	EvaluatedAt      time.Time `json:"evaluated_at"`
}

type AuditCheckpoint struct {
	ID              int64     `json:"id"`
	OrganizationID  int64     `json:"organization_id"`
	Sequence        int64     `json:"sequence"`
	FirstEventID    int64     `json:"first_event_id"`
	LastEventID     int64     `json:"last_event_id"`
	EventCount      int       `json:"event_count"`
	PreviousHash    string    `json:"previous_hash"`
	PayloadHash     string    `json:"payload_hash"`
	CheckpointHash  string    `json:"checkpoint_hash"`
	Signature       string    `json:"signature"`
	CreatedByUserID int64     `json:"created_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
}

type WORMAuditExport struct {
	ID              int64     `json:"id"`
	OrganizationID  int64     `json:"organization_id"`
	FromEventID     int64     `json:"from_event_id"`
	ToEventID       int64     `json:"to_event_id"`
	EventCount      int       `json:"event_count"`
	RootHash        string    `json:"root_hash"`
	CheckpointHash  string    `json:"checkpoint_hash"`
	ObjectURI       string    `json:"object_uri"`
	CreatedByUserID int64     `json:"created_by_user_id"`
	CreatedAt       time.Time `json:"created_at"`
}

type SIEMDestination struct {
	ID              int64      `json:"id"`
	OrganizationID  int64      `json:"organization_id"`
	Name            string     `json:"name"`
	Provider        string     `json:"provider"`
	EndpointURL     string     `json:"endpoint_url"`
	EventTypes      []string   `json:"event_types"`
	Enabled         bool       `json:"enabled"`
	SecretRef       string     `json:"secret_ref,omitempty"`
	CreatedByUserID int64      `json:"created_by_user_id"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	DisabledAt      *time.Time `json:"disabled_at,omitempty"`
}

type SecurityDashboard struct {
	OrganizationID       int64            `json:"organization_id"`
	Policy               ZeroTrustPolicy  `json:"policy"`
	ActiveWorkloads      int              `json:"active_workloads"`
	CertificatesExpiring int              `json:"certificates_expiring_30d"`
	TrustedDevices       int              `json:"trusted_devices"`
	HighRiskEvents       int              `json:"high_risk_events"`
	RevokedSessions      int              `json:"revoked_sessions"`
	LatestCheckpoint     *AuditCheckpoint `json:"latest_checkpoint,omitempty"`
	RecentEvents         []SecurityEvent  `json:"recent_events"`
	GeneratedAt          time.Time        `json:"generated_at"`
}

type CreateWorkloadIdentityRequest struct {
	WorkspaceID   int64    `json:"workspace_id"`
	Name          string   `json:"name"`
	SPIFFEID      string   `json:"spiffe_id"`
	AllowedScopes []string `json:"allowed_scopes"`
	MTLSRequired  bool     `json:"mtls_required"`
}

type RegisterWorkloadCertificateRequest struct {
	CertificatePEM  string `json:"certificate_pem"`
	ReplaceExisting bool   `json:"replace_existing"`
}

type WorkloadTokenResult struct {
	AccessToken                 string   `json:"access_token"`
	TokenType                   string   `json:"token_type"`
	ExpiresIn                   int64    `json:"expires_in"`
	Scopes                      []string `json:"scopes"`
	WorkloadID                  int64    `json:"workload_id"`
	BoundCertificateFingerprint string   `json:"bound_certificate_fingerprint"`
}

type TrustDeviceRequest struct {
	DeviceHash     string `json:"device_hash"`
	Label          string `json:"label,omitempty"`
	TrustLevel     string `json:"trust_level"`
	ExpiresInHours int    `json:"expires_in_hours"`
}

type UpdateZeroTrustPolicyRequest struct {
	Enabled                 bool     `json:"enabled"`
	RequireWorkloadMTLS     bool     `json:"require_workload_mtls"`
	RequireBoundTokens      bool     `json:"require_bound_tokens"`
	StepUpRiskScore         int      `json:"step_up_risk_score"`
	RevokeRiskScore         int      `json:"revoke_risk_score"`
	ImpossibleTravelKPH     int      `json:"impossible_travel_kph"`
	WAFBlockScore           int      `json:"waf_block_score"`
	AllowedCIDRs            []string `json:"allowed_cidrs"`
	DeniedCIDRs             []string `json:"denied_cidrs"`
	AutoRevokeHighRisk      bool     `json:"auto_revoke_high_risk"`
	RequireTrustedDevice    bool     `json:"require_trusted_device"`
	SIEMFederationEnabled   bool     `json:"siem_federation_enabled"`
	AuditCheckpointInterval int      `json:"audit_checkpoint_interval"`
}

type CreateSIEMDestinationRequest struct {
	Name        string   `json:"name"`
	Provider    string   `json:"provider"`
	EndpointURL string   `json:"endpoint_url"`
	EventTypes  []string `json:"event_types"`
	Enabled     bool     `json:"enabled"`
	SecretRef   string   `json:"secret_ref,omitempty"`
}
