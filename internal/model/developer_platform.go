package model

import "time"

const (
	DeveloperAppStatusDraft     = "draft"
	DeveloperAppStatusSubmitted = "submitted"
	DeveloperAppStatusApproved  = "approved"
	DeveloperAppStatusRejected  = "rejected"
	DeveloperAppStatusSuspended = "suspended"

	DeveloperCredentialOAuthClient = "oauth_client"
	DeveloperCredentialAPIKey      = "api_key"

	DeveloperEnvironmentSandbox    = "sandbox"
	DeveloperEnvironmentProduction = "production"

	DeveloperCredentialActive  = "active"
	DeveloperCredentialRevoked = "revoked"
)

type DeveloperApplication struct {
	ID                  int64      `json:"id"`
	WorkspaceID         int64      `json:"workspace_id"`
	Name                string     `json:"name"`
	Description         string     `json:"description,omitempty"`
	Status              string     `json:"status"`
	AllowedScopes       []string   `json:"allowed_scopes"`
	DailyRequestLimit   int64      `json:"daily_request_limit"`
	MonthlyRequestLimit int64      `json:"monthly_request_limit"`
	SandboxEnabled      bool       `json:"sandbox_enabled"`
	CreatedByUserID     int64      `json:"created_by_user_id"`
	ReviewedByUserID    *int64     `json:"reviewed_by_user_id,omitempty"`
	ReviewNote          string     `json:"review_note,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	SubmittedAt         *time.Time `json:"submitted_at,omitempty"`
	ReviewedAt          *time.Time `json:"reviewed_at,omitempty"`
}

type DeveloperCredential struct {
	ID              int64      `json:"id"`
	AppID           int64      `json:"app_id"`
	WorkspaceID     int64      `json:"workspace_id"`
	Kind            string     `json:"kind"`
	Environment     string     `json:"environment"`
	ExternalID      string     `json:"external_id"`
	KeyPrefix       string     `json:"key_prefix,omitempty"`
	Scopes          []string   `json:"scopes"`
	RedirectURIs    []string   `json:"redirect_uris,omitempty"`
	Status          string     `json:"status"`
	RotatedFromID   *int64     `json:"rotated_from_id,omitempty"`
	CreatedByUserID int64      `json:"created_by_user_id"`
	CreatedAt       time.Time  `json:"created_at"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
}

type DeveloperCredentialSecret struct {
	Credential   DeveloperCredential `json:"credential"`
	ClientID     string              `json:"client_id,omitempty"`
	ClientSecret string              `json:"client_secret,omitempty"`
	APIKey       string              `json:"api_key,omitempty"`
}

type DeveloperUsageDaily struct {
	AppID          int64     `json:"app_id"`
	WorkspaceID    int64     `json:"workspace_id"`
	Date           string    `json:"date"`
	Requests       int64     `json:"requests"`
	Errors         int64     `json:"errors"`
	TotalLatencyMS int64     `json:"total_latency_ms"`
	LastRequestAt  time.Time `json:"last_request_at"`
}

type DeveloperUsageSummary struct {
	AppID               int64                 `json:"app_id"`
	DailyRequestLimit   int64                 `json:"daily_request_limit"`
	MonthlyRequestLimit int64                 `json:"monthly_request_limit"`
	TodayRequests       int64                 `json:"today_requests"`
	MonthRequests       int64                 `json:"month_requests"`
	TotalRequests       int64                 `json:"total_requests"`
	TotalErrors         int64                 `json:"total_errors"`
	AverageLatencyMS    float64               `json:"average_latency_ms"`
	Daily               []DeveloperUsageDaily `json:"daily"`
}

type DeveloperWebhookTest struct {
	ID              int64          `json:"id"`
	AppID           int64          `json:"app_id"`
	WorkspaceID     int64          `json:"workspace_id"`
	URL             string         `json:"url"`
	EventType       string         `json:"event_type"`
	Payload         map[string]any `json:"payload"`
	DryRun          bool           `json:"dry_run"`
	Status          string         `json:"status"`
	HTTPStatus      int            `json:"http_status,omitempty"`
	ResponsePreview string         `json:"response_preview,omitempty"`
	Error           string         `json:"error,omitempty"`
	CreatedByUserID int64          `json:"created_by_user_id"`
	CreatedAt       time.Time      `json:"created_at"`
}

type DeveloperSandboxContract struct {
	AppID         int64    `json:"app_id"`
	WorkspaceID   int64    `json:"workspace_id"`
	Isolated      bool     `json:"isolated"`
	BasePath      string   `json:"base_path"`
	AllowedScopes []string `json:"allowed_scopes"`
	Notes         []string `json:"notes"`
}

type DeveloperDocEntry struct {
	Path        string `json:"path"`
	Method      string `json:"method,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Description string `json:"description,omitempty"`
}

type DeveloperSDK struct {
	Language string `json:"language"`
	Slug     string `json:"slug"`
	Path     string `json:"path"`
}

type DeveloperRequestDecision struct {
	IsDeveloper bool   `json:"-"`
	Allowed     bool   `json:"-"`
	AppID       int64  `json:"-"`
	WorkspaceID int64  `json:"-"`
	Environment string `json:"-"`
	Message     string `json:"-"`
	StatusCode  int    `json:"-"`
}

type CreateDeveloperApplicationRequest struct {
	Name                string   `json:"name"`
	Description         string   `json:"description,omitempty"`
	AllowedScopes       []string `json:"allowed_scopes"`
	DailyRequestLimit   int64    `json:"daily_request_limit,omitempty"`
	MonthlyRequestLimit int64    `json:"monthly_request_limit,omitempty"`
	SandboxEnabled      *bool    `json:"sandbox_enabled,omitempty"`
}

type ReviewDeveloperApplicationRequest struct {
	Decision string `json:"decision"`
	Note     string `json:"note,omitempty"`
}

type CreateDeveloperCredentialRequest struct {
	Kind         string   `json:"kind"`
	Environment  string   `json:"environment"`
	Name         string   `json:"name,omitempty"`
	Scopes       []string `json:"scopes"`
	RedirectURIs []string `json:"redirect_uris,omitempty"`
	ExpiresIn    int64    `json:"expires_in_seconds,omitempty"`
}

type DeveloperWebhookTestRequest struct {
	URL       string         `json:"url"`
	EventType string         `json:"event_type"`
	Payload   map[string]any `json:"payload,omitempty"`
	DryRun    *bool          `json:"dry_run,omitempty"`
}
