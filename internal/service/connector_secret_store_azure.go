package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

const azureKeyVaultAPIVersion = "7.4"

type AzureKeyVaultConfig struct {
	VaultURL                string
	Prefix                  string
	AccessToken             string
	TenantID                string
	ClientID                string
	FederatedTokenFile      string
	AuthorityHost           string
	UseManagedIdentity      bool
	ManagedIdentityEndpoint string
	CMKKeyID                string
	Timeout                 time.Duration
	AllowInsecure           bool
}

type azureAccessTokenProvider interface {
	Token(context.Context) (string, error)
}

type azureStaticTokenProvider struct {
	token string
}

func (p azureStaticTokenProvider) Token(context.Context) (string, error) {
	if strings.TrimSpace(p.token) == "" {
		return "", fmt.Errorf("%w: Azure access token is not configured", ErrConnectorSecretStore)
	}
	return p.token, nil
}

type azureOAuthTokenProvider struct {
	tenantID      string
	clientID      string
	tokenFile     string
	tokenEndpoint *url.URL
	client        *http.Client
	mu            sync.Mutex
	token         string
	expiresAt     time.Time
}

func (p *azureOAuthTokenProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.token != "" && p.expiresAt.After(time.Now().UTC().Add(5*time.Minute)) {
		return p.token, nil
	}
	assertion, err := os.ReadFile(p.tokenFile)
	if err != nil {
		return "", fmt.Errorf("%w: read Azure federated token: %v", ErrConnectorSecretStore, err)
	}
	if strings.TrimSpace(string(assertion)) == "" {
		return "", fmt.Errorf("%w: Azure federated token is empty", ErrConnectorSecretStore)
	}
	values := url.Values{}
	values.Set("client_id", p.clientID)
	values.Set("scope", "https://vault.azure.net/.default")
	values.Set("grant_type", "client_credentials")
	values.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
	values.Set("client_assertion", strings.TrimSpace(string(assertion)))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenEndpoint.String(), strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "simple-task-manager-azure-workload-identity/1")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: Azure workload identity token request failed: %v", ErrConnectorSecretStore, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: Azure workload identity token HTTP %d", ErrConnectorSecretStore, resp.StatusCode)
	}
	token, expiry, err := parseAzureTokenResponse(raw)
	if err != nil {
		return "", err
	}
	p.token = token
	p.expiresAt = expiry
	return token, nil
}

type azureManagedIdentityTokenProvider struct {
	clientID  string
	endpoint  *url.URL
	client    *http.Client
	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func (p *azureManagedIdentityTokenProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.token != "" && p.expiresAt.After(time.Now().UTC().Add(5*time.Minute)) {
		return p.token, nil
	}
	u := *p.endpoint
	q := u.Query()
	q.Set("api-version", "2018-02-01")
	q.Set("resource", "https://vault.azure.net")
	if strings.TrimSpace(p.clientID) != "" {
		q.Set("client_id", p.clientID)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata", "true")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "simple-task-manager-azure-managed-identity/1")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: Azure managed identity token request failed: %v", ErrConnectorSecretStore, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: Azure managed identity token HTTP %d", ErrConnectorSecretStore, resp.StatusCode)
	}
	token, expiry, err := parseAzureTokenResponse(raw)
	if err != nil {
		return "", err
	}
	p.token = token
	p.expiresAt = expiry
	return token, nil
}

type AzureKeyVaultSecretStore struct {
	vaultURL *url.URL
	prefix   string
	cmkKeyID *url.URL
	client   *http.Client
	tokens   azureAccessTokenProvider
}

type azureKeyVaultAPIError struct {
	Status  int
	Code    string
	Message string
}

func (e *azureKeyVaultAPIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("Azure Key Vault %s (HTTP %d)", e.Code, e.Status)
	}
	return fmt.Sprintf("Azure Key Vault %s: %s", e.Code, e.Message)
}

type azureStoredConnectorSecret struct {
	Payload  string `json:"payload"`
	Version  int    `json:"version"`
	Encoding string `json:"encoding"`
	KeyID    string `json:"key_id,omitempty"`
}

func NewAzureKeyVaultSecretStore(cfg AzureKeyVaultConfig) (*AzureKeyVaultSecretStore, error) {
	vaultURL, err := parseAzureVaultURL(cfg.VaultURL, cfg.AllowInsecure)
	if err != nil {
		return nil, err
	}
	prefix := strings.Trim(strings.TrimSpace(cfg.Prefix), "-")
	if prefix == "" {
		prefix = "stm-connectors"
	}
	if !validAzureSecretName(prefix) {
		return nil, fmt.Errorf("%w: invalid Azure secret prefix", ErrConnectorSecretStore)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	tokenProvider, err := newAzureAccessTokenProvider(cfg, client)
	if err != nil {
		return nil, err
	}
	var cmkKeyID *url.URL
	if strings.TrimSpace(cfg.CMKKeyID) != "" {
		cmkKeyID, err = parseAzureCMKKeyID(cfg.CMKKeyID, vaultURL, cfg.AllowInsecure)
		if err != nil {
			return nil, err
		}
	}
	return &AzureKeyVaultSecretStore{
		vaultURL: vaultURL, prefix: prefix, cmkKeyID: cmkKeyID, client: client, tokens: tokenProvider,
	}, nil
}

func NewAzureKeyVaultSecretStoreFromEnv(allowInsecure bool) (*AzureKeyVaultSecretStore, error) {
	rawEnabled := strings.TrimSpace(os.Getenv("CONNECTOR_AZURE_KEY_VAULT_ENABLED"))
	if rawEnabled == "" {
		return nil, nil
	}
	enabled, err := strconv.ParseBool(rawEnabled)
	if err != nil {
		return nil, fmt.Errorf("%w: CONNECTOR_AZURE_KEY_VAULT_ENABLED must be true or false", ErrConnectorSecretStore)
	}
	if !enabled {
		return nil, nil
	}
	timeout := 10 * time.Second
	if raw := strings.TrimSpace(os.Getenv("CONNECTOR_AZURE_TIMEOUT")); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil || parsed <= 0 {
			return nil, fmt.Errorf("%w: CONNECTOR_AZURE_TIMEOUT must be a positive duration", ErrConnectorSecretStore)
		}
		timeout = parsed
	}
	useManagedIdentity := false
	if raw := strings.TrimSpace(os.Getenv("CONNECTOR_AZURE_USE_MANAGED_IDENTITY")); raw != "" {
		value, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: CONNECTOR_AZURE_USE_MANAGED_IDENTITY must be true or false", ErrConnectorSecretStore)
		}
		useManagedIdentity = value
	}
	return NewAzureKeyVaultSecretStore(AzureKeyVaultConfig{
		VaultURL:                 os.Getenv("CONNECTOR_AZURE_KEY_VAULT_URL"),
		Prefix:                   os.Getenv("CONNECTOR_AZURE_SECRET_PREFIX"),
		AccessToken:              os.Getenv("CONNECTOR_AZURE_ACCESS_TOKEN"),
		TenantID:                 os.Getenv("AZURE_TENANT_ID"),
		ClientID:                 os.Getenv("AZURE_CLIENT_ID"),
		FederatedTokenFile:       os.Getenv("AZURE_FEDERATED_TOKEN_FILE"),
		AuthorityHost:            os.Getenv("AZURE_AUTHORITY_HOST"),
		UseManagedIdentity:       useManagedIdentity,
		ManagedIdentityEndpoint:  os.Getenv("CONNECTOR_AZURE_MANAGED_IDENTITY_ENDPOINT"),
		CMKKeyID:                 os.Getenv("CONNECTOR_AZURE_CMK_KEY_ID"),
		Timeout:                  timeout,
		AllowInsecure:            allowInsecure,
	})
}

func newAzureAccessTokenProvider(cfg AzureKeyVaultConfig, client *http.Client) (azureAccessTokenProvider, error) {
	if token := strings.TrimSpace(cfg.AccessToken); token != "" {
		return azureStaticTokenProvider{token: token}, nil
	}
	tenantID := strings.TrimSpace(cfg.TenantID)
	clientID := strings.TrimSpace(cfg.ClientID)
	tokenFile := strings.TrimSpace(cfg.FederatedTokenFile)
	if tenantID != "" || tokenFile != "" {
		if tenantID == "" || clientID == "" || tokenFile == "" {
			return nil, fmt.Errorf("%w: Azure workload identity requires tenant id, client id and federated token file", ErrConnectorSecretStore)
		}
		authority := strings.TrimRight(strings.TrimSpace(cfg.AuthorityHost), "/")
		if authority == "" {
			authority = "https://login.microsoftonline.com"
		}
		endpoint, err := parseAzureIdentityEndpoint(authority+"/"+url.PathEscape(tenantID)+"/oauth2/v2.0/token", cfg.AllowInsecure, false)
		if err != nil {
			return nil, err
		}
		return &azureOAuthTokenProvider{
			tenantID: tenantID, clientID: clientID, tokenFile: tokenFile,
			tokenEndpoint: endpoint, client: client,
		}, nil
	}
	if cfg.UseManagedIdentity {
		rawEndpoint := strings.TrimSpace(cfg.ManagedIdentityEndpoint)
		if rawEndpoint == "" {
			rawEndpoint = "http://169.254.169.254/metadata/identity/oauth2/token"
		}
		endpoint, err := parseAzureIdentityEndpoint(rawEndpoint, cfg.AllowInsecure, true)
		if err != nil {
			return nil, err
		}
		return &azureManagedIdentityTokenProvider{clientID: clientID, endpoint: endpoint, client: client}, nil
	}
	return nil, fmt.Errorf("%w: Azure workload identity, managed identity or access token is required", ErrConnectorSecretStore)
}

func (s *AzureKeyVaultSecretStore) Backend() string {
	return model.ConnectorSecretBackendAzure
}
