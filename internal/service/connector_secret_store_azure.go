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

func (s *AzureKeyVaultSecretStore) Backend() string {
	return model.ConnectorSecretBackendAzure
}
