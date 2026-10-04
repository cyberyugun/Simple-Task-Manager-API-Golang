package model

import "time"

const (
	ExtensionPublisherDraft     = "draft"
	ExtensionPublisherSubmitted = "submitted"
	ExtensionPublisherVerified  = "verified"
	ExtensionPublisherRejected  = "rejected"

	MarketplaceAppDraft     = "draft"
	MarketplaceAppSubmitted = "submitted"
	MarketplaceAppApproved  = "approved"
	MarketplaceAppRejected  = "rejected"
	MarketplaceAppSuspended = "suspended"

	ExtensionInstallationActive      = "active"
	ExtensionInstallationUninstalled = "uninstalled"

	ExtensionExecutionRemote = "remote_webhook_api"

	ExtensionPackWorkflow   = "workflow"
	ExtensionPackCompliance = "compliance"
	ExtensionPackReporting  = "reporting"
)

type ExtensionPublisher struct {
	ID               int64      `json:"id"`
	WorkspaceID      int64      `json:"workspace_id"`
	Name             string     `json:"name"`
	Slug             string     `json:"slug"`
	WebsiteURL       string     `json:"website_url,omitempty"`
	Status           string     `json:"status"`
	CreatedByUserID  int64      `json:"created_by_user_id"`
	ReviewedByUserID *int64     `json:"reviewed_by_user_id,omitempty"`
	ReviewNote       string     `json:"review_note,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	SubmittedAt      *time.Time `json:"submitted_at,omitempty"`
	ReviewedAt       *time.Time `json:"reviewed_at,omitempty"`
}

type CreateExtensionPublisherRequest struct {
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	WebsiteURL string `json:"website_url,omitempty"`
}

type ReviewExtensionPublisherRequest struct {
	Decision string `json:"decision"`
	Note     string `json:"note,omitempty"`
}

type ExtensionPack struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	Description string         `json:"description,omitempty"`
	Definition  map[string]any `json:"definition,omitempty"`
}

type ExtensionConfigField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret"`
}

type MarketplaceApplication struct {
	ID                   int64                  `json:"id"`
	PublisherID          int64                  `json:"publisher_id"`
	PublisherWorkspaceID int64                  `json:"publisher_workspace_id"`
	Slug                 string                 `json:"slug"`
	Name                 string                 `json:"name"`
	Summary              string                 `json:"summary"`
	Description          string                 `json:"description"`
	Version              string                 `json:"version"`
	ManifestVersion      int                    `json:"manifest_version"`
	ExecutionModel       string                 `json:"execution_model"`
	HomepageURL          string                 `json:"homepage_url,omitempty"`
	PrivacyURL           string                 `json:"privacy_url,omitempty"`
	Categories           []string               `json:"categories"`
	RequestedScopes      []string               `json:"requested_scopes"`
	EventTypes           []string               `json:"event_types"`
	ConfigSchema         []ExtensionConfigField `json:"config_schema"`
	Packs                []ExtensionPack        `json:"packs"`
	DailyRequestLimit    int64                  `json:"daily_request_limit"`
	MonthlyRequestLimit  int64                  `json:"monthly_request_limit"`
	Status               string                 `json:"status"`
	CreatedByUserID      int64                  `json:"created_by_user_id"`
	ReviewedByUserID     *int64                 `json:"reviewed_by_user_id,omitempty"`
	ReviewNote           string                 `json:"review_note,omitempty"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`
	SubmittedAt          *time.Time             `json:"submitted_at,omitempty"`
	ReviewedAt           *time.Time             `json:"reviewed_at,omitempty"`
}

type CreateMarketplaceApplicationRequest struct {
	PublisherID         int64                  `json:"publisher_id"`
	Slug                string                 `json:"slug"`
	Name                string                 `json:"name"`
	Summary             string                 `json:"summary"`
	Description         string                 `json:"description"`
	Version             string                 `json:"version"`
	HomepageURL         string                 `json:"homepage_url,omitempty"`
	PrivacyURL          string                 `json:"privacy_url,omitempty"`
	Categories          []string               `json:"categories"`
	RequestedScopes     []string               `json:"requested_scopes"`
	EventTypes          []string               `json:"event_types,omitempty"`
	ConfigSchema        []ExtensionConfigField `json:"config_schema,omitempty"`
	Packs               []ExtensionPack        `json:"packs,omitempty"`
	DailyRequestLimit   int64                  `json:"daily_request_limit,omitempty"`
	MonthlyRequestLimit int64                  `json:"monthly_request_limit,omitempty"`
}

type ReviewMarketplaceApplicationRequest struct {
	Decision string `json:"decision"`
	Note     string `json:"note,omitempty"`
}

type MarketplaceListing struct {
	Application MarketplaceApplication `json:"application"`
	Publisher   ExtensionPublisher     `json:"publisher"`
}

type ExtensionInstallation struct {
	ID                  int64             `json:"id"`
	OrganizationID      int64             `json:"organization_id"`
	ApplicationID       int64             `json:"application_id"`
	WorkspaceID         int64             `json:"workspace_id"`
	Status              string            `json:"status"`
	GrantedScopes       []string          `json:"granted_scopes"`
	Config              map[string]string `json:"config"`
	SecretRefs          map[string]string `json:"secret_refs"`
	InstallSecretPrefix string            `json:"install_secret_prefix"`
	DailyRequestLimit   int64             `json:"daily_request_limit"`
	MonthlyRequestLimit int64             `json:"monthly_request_limit"`
	InstalledByUserID   int64             `json:"installed_by_user_id"`
	InstalledAt         time.Time         `json:"installed_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
	UninstalledByUserID *int64            `json:"uninstalled_by_user_id,omitempty"`
	UninstalledAt       *time.Time        `json:"uninstalled_at,omitempty"`
}

type InstallExtensionRequest struct {
	ApplicationID int64             `json:"application_id"`
	WorkspaceID   int64             `json:"workspace_id"`
	GrantedScopes []string          `json:"granted_scopes"`
	Config        map[string]string `json:"config,omitempty"`
	SecretRefs    map[string]string `json:"secret_refs,omitempty"`
}

type ExtensionInstallationSecret struct {
	Installation ExtensionInstallation `json:"installation"`
	Secret       string                `json:"secret"`
}

type ExtensionTokenRequest struct {
	Secret string `json:"secret"`
}

type ExtensionTokenResult struct {
	AccessToken    string   `json:"access_token"`
	TokenType      string   `json:"token_type"`
	ExpiresIn      int64    `json:"expires_in"`
	Scopes         []string `json:"scopes"`
	WorkspaceID    int64    `json:"workspace_id"`
	InstallationID int64    `json:"installation_id"`
}

type ExtensionEventSubscription struct {
	ID                    int64     `json:"id"`
	InstallationID        int64     `json:"installation_id"`
	OrganizationID        int64     `json:"organization_id"`
	WorkspaceID           int64     `json:"workspace_id"`
	WebhookSubscriptionID int64     `json:"webhook_subscription_id"`
	URL                   string    `json:"url"`
	EventTypes            []string  `json:"event_types"`
	CreatedByUserID       int64     `json:"created_by_user_id"`
	CreatedAt             time.Time `json:"created_at"`
}

type CreateExtensionEventSubscriptionRequest struct {
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types"`
}

type ExtensionEventSubscriptionSecret struct {
	Subscription  ExtensionEventSubscription `json:"subscription"`
	SigningSecret string                     `json:"signing_secret"`
}

type ExtensionUsageDaily struct {
	InstallationID int64     `json:"installation_id"`
	OrganizationID int64     `json:"organization_id"`
	WorkspaceID    int64     `json:"workspace_id"`
	Date           string    `json:"date"`
	Requests       int64     `json:"requests"`
	Errors         int64     `json:"errors"`
	TotalLatencyMS int64     `json:"total_latency_ms"`
	LastRequestAt  time.Time `json:"last_request_at"`
}

type ExtensionUsageSummary struct {
	InstallationID      int64                 `json:"installation_id"`
	DailyRequestLimit   int64                 `json:"daily_request_limit"`
	MonthlyRequestLimit int64                 `json:"monthly_request_limit"`
	TodayRequests       int64                 `json:"today_requests"`
	MonthRequests       int64                 `json:"month_requests"`
	TotalRequests       int64                 `json:"total_requests"`
	TotalErrors         int64                 `json:"total_errors"`
	AverageLatencyMS    float64               `json:"average_latency_ms"`
	Daily               []ExtensionUsageDaily `json:"daily"`
}

type ExtensionRequestDecision struct {
	IsExtension    bool
	Allowed        bool
	InstallationID int64
	WorkspaceID    int64
	Message        string
	StatusCode     int
}
