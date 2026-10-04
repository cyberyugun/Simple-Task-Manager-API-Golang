package model

import "time"

const (
	ConnectorCredentialActive  = "active"
	ConnectorCredentialExpired = "expired"
	ConnectorCredentialRevoked = "revoked"
	ConnectorCredentialError   = "error"

	ConnectorSecretBackendDatabase = "database_envelope"
	ConnectorSecretBackendAWS      = "aws_secrets_manager"
	ConnectorSecretBackendAzure    = "azure_key_vault"
	ConnectorSecretBackendGCP      = "gcp_secret_manager"
	ConnectorSecretBackendVault    = "hashicorp_vault"
)

type ConnectorOAuthSession struct {
	ID                int64      `json:"id"`
	OrganizationID    int64      `json:"organization_id"`
	ConnectionID      int64      `json:"connection_id"`
	StateHash         string     `json:"-"`
	EncryptedVerifier string     `json:"-"`
	RedirectURI       string     `json:"redirect_uri"`
	RequestedScopes   []string   `json:"requested_scopes"`
	CreatedByUserID   int64      `json:"created_by_user_id"`
	ExpiresAt         time.Time  `json:"expires_at"`
	ConsumedAt        *time.Time `json:"consumed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

type ConnectorCredentialMetadata struct {
	ConnectionID      int64      `json:"connection_id"`
	OrganizationID    int64      `json:"organization_id"`
	SecretBackend     string     `json:"secret_backend"`
	SecretRef         string     `json:"secret_ref"`
	KeyVersion        int        `json:"key_version"`
	CredentialVersion int        `json:"credential_version"`
	Status            string     `json:"status"`
	GrantedScopes     []string   `json:"granted_scopes"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	LastRefreshAt     *time.Time `json:"last_refresh_at,omitempty"`
	LastValidatedAt   *time.Time `json:"last_validated_at,omitempty"`
	RotatedAt         *time.Time `json:"rotated_at,omitempty"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type ConnectorCredentialAccess struct {
	ID             int64     `json:"id"`
	OrganizationID int64     `json:"organization_id"`
	ConnectionID   int64     `json:"connection_id"`
	ActorUserID    *int64    `json:"actor_user_id,omitempty"`
	Action         string    `json:"action"`
	SecretBackend  string    `json:"secret_backend"`
	SecretRef      string    `json:"secret_ref"`
	CreatedAt      time.Time `json:"created_at"`
}

type ConnectorOAuthStart struct {
	AuthorizationURL string    `json:"authorization_url"`
	State            string    `json:"state"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type CompleteConnectorOAuthRequest struct {
	State string `json:"state"`
	Code  string `json:"code"`
}

type RotateConnectorCredentialRequest struct {
	SecretBackend string `json:"secret_backend,omitempty"`
	KeyVersion    int    `json:"key_version,omitempty"`
}

type ConnectorConnectionTest struct {
	ConnectionID int64     `json:"connection_id"`
	Healthy      bool      `json:"healthy"`
	HTTPStatus   int       `json:"http_status,omitempty"`
	ScopesValid  bool      `json:"scopes_valid"`
	CheckedAt    time.Time `json:"checked_at"`
	Message      string    `json:"message,omitempty"`
}

type ConnectorSecretBackend struct {
	Key                string `json:"key"`
	DisplayName        string `json:"display_name"`
	ExternalVault      bool   `json:"external_vault"`
	EnvelopeEncryption bool   `json:"envelope_encryption"`
	CustomerManagedKey bool   `json:"customer_managed_key"`
}
