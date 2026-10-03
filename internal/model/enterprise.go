package model

import "time"

const (
	TokenUseUser    = "user"
	TokenUseService = "service"

	ScopeTasksRead      = "tasks:read"
	ScopeTasksWrite     = "tasks:write"
	ScopeWorkspaceRead  = "workspace:read"
	ScopeWorkspaceAdmin = "workspace:admin"
	ScopeAuditRead      = "audit:read"
	ScopeIdentityAdmin  = "identity:admin"
)

type OAuthClient struct {
	ClientID        string     `json:"client_id"`
	WorkspaceID     int64      `json:"workspace_id"`
	Name            string     `json:"name"`
	SecretHash      string     `json:"-"`
	RedirectURIs    []string   `json:"redirect_uris"`
	AllowedScopes   []string   `json:"allowed_scopes"`
	CreatedByUserID int64      `json:"created_by_user_id"`
	CreatedAt       time.Time  `json:"created_at"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
}

type OAuthAuthorizationCode struct {
	CodeHash      string     `json:"-"`
	ClientID      string     `json:"client_id"`
	UserID        int64      `json:"user_id"`
	WorkspaceID   int64      `json:"workspace_id"`
	RedirectURI   string     `json:"redirect_uri"`
	Scopes        []string   `json:"scopes"`
	CodeChallenge string     `json:"-"`
	ExpiresAt     time.Time  `json:"expires_at"`
	ConsumedAt    *time.Time `json:"consumed_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type ServiceAPIKey struct {
	ID              int64      `json:"id"`
	WorkspaceID     int64      `json:"workspace_id"`
	Name            string     `json:"name"`
	KeyPrefix       string     `json:"key_prefix"`
	KeyHash         string     `json:"-"`
	Scopes          []string   `json:"scopes"`
	CreatedByUserID int64      `json:"created_by_user_id"`
	LastUsedAt      *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type EnterpriseSecurityPolicy struct {
	WorkspaceID          int64     `json:"workspace_id"`
	RequireMFA           bool      `json:"require_mfa"`
	AllowServiceAccounts bool      `json:"allow_service_accounts"`
	AllowedEmailDomains  []string  `json:"allowed_email_domains"`
	MaxSessionAgeMinutes int       `json:"max_session_age_minutes"`
	UpdatedByUserID      int64     `json:"updated_by_user_id"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type OIDCConnection struct {
	WorkspaceID     int64     `json:"workspace_id"`
	IssuerURL       string    `json:"issuer_url"`
	ClientID        string    `json:"client_id"`
	ClientSecretRef string    `json:"client_secret_ref,omitempty"`
	Scopes          []string  `json:"scopes"`
	Enabled         bool      `json:"enabled"`
	UpdatedByUserID int64     `json:"updated_by_user_id"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type SCIMUser struct {
	ID          int64     `json:"id"`
	WorkspaceID int64     `json:"workspace_id"`
	ExternalID  string    `json:"external_id"`
	UserID      int64     `json:"user_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateOAuthClientRequest struct {
	Name          string   `json:"name"`
	RedirectURIs  []string `json:"redirect_uris"`
	AllowedScopes []string `json:"allowed_scopes"`
}

type OAuthClientSecretResult struct {
	Client       OAuthClient `json:"client"`
	ClientSecret string      `json:"client_secret"`
}

type OAuthAuthorizeRequest struct {
	ClientID      string   `json:"client_id"`
	RedirectURI   string   `json:"redirect_uri"`
	Scopes        []string `json:"scopes"`
	CodeChallenge string   `json:"code_challenge"`
}

type OAuthAuthorizeResult struct {
	Code      string `json:"code"`
	ExpiresIn int64  `json:"expires_in"`
}

type OAuthTokenRequest struct {
	GrantType    string   `json:"grant_type"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	Code         string   `json:"code,omitempty"`
	CodeVerifier string   `json:"code_verifier,omitempty"`
	RedirectURI  string   `json:"redirect_uri,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
}

type OAuthTokenResult struct {
	AccessToken string   `json:"access_token"`
	TokenType   string   `json:"token_type"`
	ExpiresIn   int64    `json:"expires_in"`
	Scopes      []string `json:"scopes"`
}

type CreateAPIKeyRequest struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresIn int64    `json:"expires_in_seconds,omitempty"`
}

type APIKeySecretResult struct {
	APIKey ServiceAPIKey `json:"api_key"`
	Secret string        `json:"secret"`
}

type IntrospectionResult struct {
	Active      bool     `json:"active"`
	Subject     string   `json:"sub,omitempty"`
	ClientID    string   `json:"client_id,omitempty"`
	WorkspaceID int64    `json:"workspace_id,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
	TokenUse    string   `json:"token_use,omitempty"`
	ExpiresAt   int64    `json:"exp,omitempty"`
}

type ConfigureOIDCRequest struct {
	IssuerURL       string   `json:"issuer_url"`
	ClientID        string   `json:"client_id"`
	ClientSecretRef string   `json:"client_secret_ref,omitempty"`
	Scopes          []string `json:"scopes"`
	Enabled         bool     `json:"enabled"`
}

type SCIMUpsertUserRequest struct {
	ExternalID  string `json:"external_id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Active      bool   `json:"active"`
}

type UpdateEnterprisePolicyRequest struct {
	RequireMFA           bool     `json:"require_mfa"`
	AllowServiceAccounts bool     `json:"allow_service_accounts"`
	AllowedEmailDomains  []string `json:"allowed_email_domains"`
	MaxSessionAgeMinutes int      `json:"max_session_age_minutes"`
}
