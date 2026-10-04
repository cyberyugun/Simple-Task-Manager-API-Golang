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

func NewGCPSecretManagerSecretStoreFromEnv(allowInsecure bool) (*GCPSecretManagerSecretStore, error) {
	rawEnabled := strings.TrimSpace(os.Getenv("CONNECTOR_GCP_SECRET_MANAGER_ENABLED"))
	if rawEnabled == "" {
		return nil, nil
	}
	enabled, err := strconv.ParseBool(rawEnabled)
	if err != nil {
		return nil, fmt.Errorf("%w: CONNECTOR_GCP_SECRET_MANAGER_ENABLED must be true or false", ErrConnectorSecretStore)
	}
	if !enabled {
		return nil, nil
	}
	timeout := 10 * time.Second
	if raw := strings.TrimSpace(os.Getenv("CONNECTOR_GCP_TIMEOUT")); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil || parsed <= 0 {
			return nil, fmt.Errorf("%w: CONNECTOR_GCP_TIMEOUT must be a positive duration", ErrConnectorSecretStore)
		}
		timeout = parsed
	}
	useMetadata := true
	if raw := strings.TrimSpace(os.Getenv("CONNECTOR_GCP_USE_METADATA")); raw != "" {
		value, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: CONNECTOR_GCP_USE_METADATA must be true or false", ErrConnectorSecretStore)
		}
		useMetadata = value
	}
	projectID := firstNonEmpty(
		os.Getenv("CONNECTOR_GCP_PROJECT_ID"),
		os.Getenv("GOOGLE_CLOUD_PROJECT"),
		os.Getenv("GCP_PROJECT_ID"),
	)
	return NewGCPSecretManagerSecretStore(GCPSecretManagerConfig{
		ProjectID:        projectID,
		Prefix:           os.Getenv("CONNECTOR_GCP_SECRET_PREFIX"),
		CMEKKeyName:      os.Getenv("CONNECTOR_GCP_CMEK_KEY_NAME"),
		Endpoint:         os.Getenv("CONNECTOR_GCP_SECRET_MANAGER_ENDPOINT"),
		AccessToken:      os.Getenv("CONNECTOR_GCP_ACCESS_TOKEN"),
		UseMetadata:      useMetadata,
		MetadataEndpoint: os.Getenv("CONNECTOR_GCP_METADATA_ENDPOINT"),
		Timeout:          timeout,
		AllowInsecure:    allowInsecure,
	})
}

func newGCPAccessTokenProvider(cfg GCPSecretManagerConfig, timeout time.Duration) (gcpAccessTokenProvider, error) {
	if token := strings.TrimSpace(cfg.AccessToken); token != "" {
		return gcpStaticTokenProvider{token: token}, nil
	}
	if !cfg.UseMetadata {
		return nil, fmt.Errorf("%w: GCP workload identity metadata or access token is required", ErrConnectorSecretStore)
	}
	rawEndpoint := strings.TrimSpace(cfg.MetadataEndpoint)
	if rawEndpoint == "" {
		rawEndpoint = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token"
	}
	endpoint, err := parseGCPMetadataEndpoint(rawEndpoint, cfg.AllowInsecure)
	if err != nil {
		return nil, err
	}
	return &gcpMetadataTokenProvider{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (s *GCPSecretManagerSecretStore) Put(ctx context.Context, ref string, plaintext []byte) (int, error) {
	secretID, err := s.secretID(ref)
	if err != nil {
		return 0, err
	}
	exists, err := s.secretExists(ctx, secretID)
	if err != nil {
		return 0, err
	}
	if !exists {
		automatic := map[string]any{}
		if s.cmekKey != "" {
			automatic["customerManagedEncryption"] = map[string]any{"kmsKeyName": s.cmekKey}
		}
		createEndpoint := s.projectURL("/secrets")
		u, err := url.Parse(createEndpoint)
		if err != nil {
			return 0, err
		}
		q := u.Query()
		q.Set("secretId", secretID)
		u.RawQuery = q.Encode()
		if err := s.call(ctx, http.MethodPost, u.String(), map[string]any{
			"replication": map[string]any{"automatic": automatic},
		}, nil); err != nil && !isGCPSecretAlreadyExists(err) {
			return 0, err
		}
	}
	var response struct {
		Name string `json:"name"`
	}
	if err := s.call(ctx, http.MethodPost, s.secretResourceURL(secretID)+":addVersion", map[string]any{
		"payload": map[string]string{"data": base64.StdEncoding.EncodeToString(plaintext)},
	}, &response); err != nil {
		return 0, err
	}
	version, err := gcpSecretVersionFromName(response.Name)
	if err != nil {
		return 0, err
	}
	return version, nil
}

func (s *GCPSecretManagerSecretStore) Get(ctx context.Context, ref string) ([]byte, int, error) {
	secretID, err := s.secretID(ref)
	if err != nil {
		return nil, 0, err
	}
	var response struct {
		Name    string `json:"name"`
		Payload struct {
			Data string `json:"data"`
		} `json:"payload"`
	}
	endpoint := s.secretResourceURL(secretID) + "/versions/latest:access"
	if err := s.call(ctx, http.MethodGet, endpoint, nil, &response); err != nil {
		return nil, 0, err
	}
	version, err := gcpSecretVersionFromName(response.Name)
	if err != nil {
		return nil, 0, err
	}
	plaintext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(response.Payload.Data))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: invalid GCP secret payload", ErrConnectorSecretStore)
	}
	return plaintext, version, nil
}

func (s *GCPSecretManagerSecretStore) Delete(ctx context.Context, ref string) error {
	secretID, err := s.secretID(ref)
	if err != nil {
		return err
	}
	return s.call(ctx, http.MethodDelete, s.secretResourceURL(secretID), nil, nil)
}

func (s *GCPSecretManagerSecretStore) secretExists(ctx context.Context, secretID string) (bool, error) {
	err := s.call(ctx, http.MethodGet, s.secretResourceURL(secretID), nil, nil)
	if err == nil {
		return true, nil
	}
	if isGCPSecretNotFound(err) {
		return false, nil
	}
	return false, err
}

func (s *GCPSecretManagerSecretStore) secretID(ref string) (string, error) {
	ref = strings.Trim(strings.TrimSpace(ref), "/")
	if !validVaultLogicalPath(ref) {
		return "", fmt.Errorf("%w: invalid GCP secret reference", ErrConnectorSecretStore)
	}
	digest := sha256Hex([]byte(s.prefix + "/" + ref))
	secretID := s.prefix + "-" + digest[:48]
	if !validGCPSecretID(secretID) {
		return "", fmt.Errorf("%w: invalid derived GCP secret id", ErrConnectorSecretStore)
	}
	return secretID, nil
}

func (s *GCPSecretManagerSecretStore) projectURL(suffix string) string {
	u := *s.endpoint
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/projects/" + url.PathEscape(s.projectID) + suffix
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func (s *GCPSecretManagerSecretStore) secretResourceURL(secretID string) string {
	return s.projectURL("/secrets/" + url.PathEscape(secretID))
}

func (s *GCPSecretManagerSecretStore) Backend() string {
	return model.ConnectorSecretBackendGCP
}
