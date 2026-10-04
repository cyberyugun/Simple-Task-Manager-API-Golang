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
	"path"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrConnectorSecretBackendUnavailable = errors.New("connector secret backend is not configured")
	ErrConnectorSecretStore               = errors.New("connector secret store operation failed")
)

type ConnectorSecretStore interface {
	Backend() string
	Put(context.Context, string, []byte) (int, error)
	Get(context.Context, string) ([]byte, int, error)
	Delete(context.Context, string) error
}

type HashiCorpVaultSecretStoreConfig struct {
	Address       string
	Token         string
	TokenFile     string
	Namespace     string
	Mount         string
	Prefix        string
	Timeout       time.Duration
	AllowInsecure bool
}

type HashiCorpVaultSecretStore struct {
	baseURL   *url.URL
	token     string
	tokenFile string
	namespace string
	mount     string
	prefix    string
	client    *http.Client
}

func NewHashiCorpVaultSecretStore(cfg HashiCorpVaultSecretStoreConfig) (*HashiCorpVaultSecretStore, error) {
	rawAddress := strings.TrimSpace(cfg.Address)
	if rawAddress == "" {
		return nil, ErrConnectorSecretBackendUnavailable
	}
	baseURL, err := url.Parse(rawAddress)
	if err != nil || baseURL.Host == "" || baseURL.User != nil || baseURL.Fragment != "" {
		return nil, fmt.Errorf("%w: invalid Vault address", ErrConnectorSecretStore)
	}
	if baseURL.Scheme != "https" && !(cfg.AllowInsecure && baseURL.Scheme == "http") {
		return nil, fmt.Errorf("%w: Vault address must use HTTPS", ErrConnectorSecretStore)
	}
	token := strings.TrimSpace(cfg.Token)
	tokenFile := strings.TrimSpace(cfg.TokenFile)
	if token == "" && tokenFile == "" {
		return nil, fmt.Errorf("%w: Vault token or token file is required", ErrConnectorSecretStore)
	}
	mount := strings.Trim(strings.TrimSpace(cfg.Mount), "/")
	if mount == "" {
		mount = "secret"
	}
	prefix := strings.Trim(strings.TrimSpace(cfg.Prefix), "/")
	if prefix == "" {
		prefix = "simple-task-manager/connectors"
	}
	if !validVaultLogicalPath(mount) || !validVaultLogicalPath(prefix) {
		return nil, fmt.Errorf("%w: invalid Vault mount or prefix", ErrConnectorSecretStore)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &HashiCorpVaultSecretStore{
		baseURL: baseURL, token: token, tokenFile: tokenFile, namespace: strings.TrimSpace(cfg.Namespace),
		mount: mount, prefix: prefix,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func NewHashiCorpVaultSecretStoreFromEnv(allowInsecure bool) (*HashiCorpVaultSecretStore, error) {
	address := strings.TrimSpace(os.Getenv("CONNECTOR_VAULT_ADDR"))
	if address == "" {
		return nil, nil
	}
	timeout := 10 * time.Second
	if raw := strings.TrimSpace(os.Getenv("CONNECTOR_VAULT_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("%w: CONNECTOR_VAULT_TIMEOUT must be a positive duration", ErrConnectorSecretStore)
		}
		timeout = parsed
	}
	return NewHashiCorpVaultSecretStore(HashiCorpVaultSecretStoreConfig{
		Address: address, Token: os.Getenv("CONNECTOR_VAULT_TOKEN"),
		TokenFile: os.Getenv("CONNECTOR_VAULT_TOKEN_FILE"), Namespace: os.Getenv("CONNECTOR_VAULT_NAMESPACE"),
		Mount: os.Getenv("CONNECTOR_VAULT_KV_MOUNT"), Prefix: os.Getenv("CONNECTOR_VAULT_PREFIX"),
		Timeout: timeout, AllowInsecure: allowInsecure,
	})
}

func (s *HashiCorpVaultSecretStore) Backend() string {
	return model.ConnectorSecretBackendVault
}

func (s *HashiCorpVaultSecretStore) Put(ctx context.Context, ref string, plaintext []byte) (int, error) {
	endpoint, err := s.endpoint("data", ref)
	if err != nil {
		return 0, err
	}
	payload, err := json.Marshal(map[string]any{
		"data": map[string]string{"payload": base64.RawStdEncoding.EncodeToString(plaintext)},
	})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	s.applyHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrConnectorSecretStore, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("%w: Vault write HTTP %d", ErrConnectorSecretStore, resp.StatusCode)
	}
	var decoded struct {
		Data struct {
			Version int `json:"version"`
		} `json:"data"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return 0, fmt.Errorf("%w: invalid Vault write response", ErrConnectorSecretStore)
		}
	}
	if decoded.Data.Version <= 0 {
		decoded.Data.Version = 1
	}
	return decoded.Data.Version, nil
}

func (s *HashiCorpVaultSecretStore) Get(ctx context.Context, ref string) ([]byte, int, error) {
	endpoint, err := s.endpoint("data", ref)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	s.applyHeaders(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrConnectorSecretStore, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, 0, fmt.Errorf("%w: Vault read HTTP %d", ErrConnectorSecretStore, resp.StatusCode)
	}
	var decoded struct {
		Data struct {
			Data struct {
				Payload string `json:"payload"`
			} `json:"data"`
			Metadata struct {
				Version int `json:"version"`
			} `json:"metadata"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil || strings.TrimSpace(decoded.Data.Data.Payload) == "" {
		return nil, 0, fmt.Errorf("%w: invalid Vault read response", ErrConnectorSecretStore)
	}
	plaintext, err := base64.RawStdEncoding.DecodeString(decoded.Data.Data.Payload)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: invalid Vault secret payload", ErrConnectorSecretStore)
	}
	return plaintext, decoded.Data.Metadata.Version, nil
}

func (s *HashiCorpVaultSecretStore) Delete(ctx context.Context, ref string) error {
	endpoint, err := s.endpoint("metadata", ref)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	s.applyHeaders(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrConnectorSecretStore, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: Vault delete HTTP %d", ErrConnectorSecretStore, resp.StatusCode)
	}
	return nil
}

func (s *HashiCorpVaultSecretStore) endpoint(kind, ref string) (string, error) {
	ref = strings.Trim(strings.TrimSpace(ref), "/")
	if !validVaultLogicalPath(ref) {
		return "", fmt.Errorf("%w: invalid secret reference", ErrConnectorSecretStore)
	}
	u := *s.baseURL
	u.Path = path.Join(u.Path, "v1", s.mount, kind, s.prefix, ref)
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func (s *HashiCorpVaultSecretStore) applyHeaders(req *http.Request) {
	if token := s.currentToken(); token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	if s.namespace != "" {
		req.Header.Set("X-Vault-Namespace", s.namespace)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "simple-task-manager-connector-vault/1")
}

func (s *HashiCorpVaultSecretStore) currentToken() string {
	if s.tokenFile != "" {
		raw, err := os.ReadFile(s.tokenFile)
		if err == nil {
			if token := strings.TrimSpace(string(raw)); token != "" {
				return token
			}
		}
	}
	return s.token
}

func validVaultLogicalPath(value string) bool {
	if value == "" || strings.Contains(value, "\\") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') &&
				!(r >= '0' && r <= '9') && r != '-' && r != '_' && r != '.' {
				return false
			}
		}
	}
	return true
}
