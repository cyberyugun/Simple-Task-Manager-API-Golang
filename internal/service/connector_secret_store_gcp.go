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

type GCPSecretManagerConfig struct {
	ProjectID         string
	Prefix            string
	CMEKKeyName       string
	Endpoint          string
	AccessToken       string
	UseMetadata       bool
	MetadataEndpoint  string
	Timeout           time.Duration
	AllowInsecure     bool
}

type gcpAccessTokenProvider interface {
	Token(context.Context) (string, error)
}

type gcpStaticTokenProvider struct {
	token string
}

func (p gcpStaticTokenProvider) Token(context.Context) (string, error) {
	if strings.TrimSpace(p.token) == "" {
		return "", fmt.Errorf("%w: GCP access token is not configured", ErrConnectorSecretStore)
	}
	return p.token, nil
}

type gcpMetadataTokenProvider struct {
	endpoint  *url.URL
	client    *http.Client
	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func (p *gcpMetadataTokenProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.token != "" && p.expiresAt.After(time.Now().UTC().Add(4*time.Minute)) {
		return p.token, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "simple-task-manager-gcp-workload-identity/1")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: GCP metadata token request failed: %v", ErrConnectorSecretStore, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: GCP metadata token HTTP %d", ErrConnectorSecretStore, resp.StatusCode)
	}
	var decoded struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("%w: invalid GCP metadata token response", ErrConnectorSecretStore)
	}
	decoded.AccessToken = strings.TrimSpace(decoded.AccessToken)
	if decoded.AccessToken == "" || decoded.ExpiresIn <= 0 {
		return "", fmt.Errorf("%w: incomplete GCP metadata token response", ErrConnectorSecretStore)
	}
	p.token = decoded.AccessToken
	p.expiresAt = time.Now().UTC().Add(time.Duration(decoded.ExpiresIn) * time.Second)
	return p.token, nil
}

type GCPSecretManagerSecretStore struct {
	projectID string
	prefix    string
	cmekKey   string
	endpoint  *url.URL
	client    *http.Client
	tokens    gcpAccessTokenProvider
}

type gcpSecretManagerAPIError struct {
	Status  int
	Code    int
	Message string
	StatusText string
}

func (e *gcpSecretManagerAPIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("GCP Secret Manager HTTP %d", e.Status)
	}
	return fmt.Sprintf("GCP Secret Manager %s: %s", e.StatusText, e.Message)
}

func NewGCPSecretManagerSecretStore(cfg GCPSecretManagerConfig) (*GCPSecretManagerSecretStore, error) {
	projectID := strings.TrimSpace(cfg.ProjectID)
	if !validGCPProjectID(projectID) {
		return nil, fmt.Errorf("%w: invalid GCP project id", ErrConnectorSecretStore)
	}
	prefix := strings.Trim(strings.TrimSpace(cfg.Prefix), "-_")
	if prefix == "" {
		prefix = "stm-connectors"
	}
	if !validGCPSecretID(prefix) {
		return nil, fmt.Errorf("%w: invalid GCP secret prefix", ErrConnectorSecretStore)
	}
	endpointRaw := strings.TrimSpace(cfg.Endpoint)
	if endpointRaw == "" {
		endpointRaw = "https://secretmanager.googleapis.com"
	}
	endpoint, err := parseGCPServiceEndpoint(endpointRaw, cfg.AllowInsecure)
	if err != nil {
		return nil, err
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
	tokens, err := newGCPAccessTokenProvider(cfg, timeout)
	if err != nil {
		return nil, err
	}
	cmek := strings.TrimSpace(cfg.CMEKKeyName)
	if cmek != "" && !validGCPKMSKeyName(cmek) {
		return nil, fmt.Errorf("%w: invalid GCP CMEK key name", ErrConnectorSecretStore)
	}
	return &GCPSecretManagerSecretStore{
		projectID: projectID, prefix: prefix, cmekKey: cmek,
		endpoint: endpoint, client: client, tokens: tokens,
	}, nil
}

func (s *GCPSecretManagerSecretStore) Backend() string {
	return model.ConnectorSecretBackendGCP
}
