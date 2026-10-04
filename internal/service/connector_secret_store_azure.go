package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
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
	Payload    string `json:"payload"`
	Version    int    `json:"version"`
	Encoding   string `json:"encoding"`
	KeyID      string `json:"key_id,omitempty"`
	WrappedKey string `json:"wrapped_key,omitempty"`
	Nonce      string `json:"nonce,omitempty"`
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

func (s *AzureKeyVaultSecretStore) Put(ctx context.Context, ref string, plaintext []byte) (int, error) {
	name, err := s.secretName(ref)
	if err != nil {
		return 0, err
	}
	version := 1
	_, currentVersion, getErr := s.Get(ctx, ref)
	switch {
	case getErr == nil:
		version = currentVersion + 1
	case isAzureSecretNotFound(getErr):
	default:
		return 0, getErr
	}
	stored := azureStoredConnectorSecret{Version: version, Encoding: "base64"}
	if s.cmkKeyID != nil {
		ciphertext, wrappedKey, nonce, keyID, err := s.encryptWithCMK(ctx, plaintext)
		if err != nil {
			return 0, err
		}
		stored.Payload = ciphertext
		stored.WrappedKey = wrappedKey
		stored.Nonce = nonce
		stored.Encoding = "azure-kv-cmk-aes-gcm-rsa-oaep-256"
		stored.KeyID = keyID
	} else {
		stored.Payload = base64.RawStdEncoding.EncodeToString(plaintext)
	}
	rawStored, err := json.Marshal(stored)
	if err != nil {
		return 0, err
	}
	body := map[string]any{
		"value":       string(rawStored),
		"contentType": "application/vnd.simple-task-manager.connector-secret+json",
	}
	if err := s.call(ctx, http.MethodPut, s.secretURL(name), body, nil); err != nil {
		return 0, err
	}
	return version, nil
}

func (s *AzureKeyVaultSecretStore) Get(ctx context.Context, ref string) ([]byte, int, error) {
	name, err := s.secretName(ref)
	if err != nil {
		return nil, 0, err
	}
	var response struct {
		Value string `json:"value"`
	}
	if err := s.call(ctx, http.MethodGet, s.secretURL(name), nil, &response); err != nil {
		return nil, 0, err
	}
	var stored azureStoredConnectorSecret
	if err := json.Unmarshal([]byte(response.Value), &stored); err != nil || stored.Payload == "" || stored.Version <= 0 {
		return nil, 0, fmt.Errorf("%w: invalid Azure Key Vault connector secret payload", ErrConnectorSecretStore)
	}
	switch stored.Encoding {
	case "base64":
		plaintext, err := base64.RawStdEncoding.DecodeString(stored.Payload)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: invalid Azure secret encoding", ErrConnectorSecretStore)
		}
		return plaintext, stored.Version, nil
	case "azure-kv-cmk-aes-gcm-rsa-oaep-256":
		if strings.TrimSpace(stored.KeyID) == "" || strings.TrimSpace(stored.WrappedKey) == "" || strings.TrimSpace(stored.Nonce) == "" {
			return nil, 0, fmt.Errorf("%w: incomplete Azure CMK envelope metadata", ErrConnectorSecretStore)
		}
		plaintext, err := s.decryptWithCMK(ctx, stored.KeyID, stored.Payload, stored.WrappedKey, stored.Nonce)
		if err != nil {
			return nil, 0, err
		}
		return plaintext, stored.Version, nil
	default:
		return nil, 0, fmt.Errorf("%w: unsupported Azure secret encoding", ErrConnectorSecretStore)
	}
}

func (s *AzureKeyVaultSecretStore) Delete(ctx context.Context, ref string) error {
	name, err := s.secretName(ref)
	if err != nil {
		return err
	}
	return s.call(ctx, http.MethodDelete, s.secretURL(name), nil, nil)
}

func (s *AzureKeyVaultSecretStore) secretName(ref string) (string, error) {
	ref = strings.Trim(strings.TrimSpace(ref), "/")
	if !validVaultLogicalPath(ref) {
		return "", fmt.Errorf("%w: invalid Azure secret reference", ErrConnectorSecretStore)
	}
	digest := sha256Hex([]byte(s.prefix + "/" + ref))
	return s.prefix + "-" + digest[:48], nil
}

func (s *AzureKeyVaultSecretStore) secretURL(name string) string {
	u := *s.vaultURL
	u.Path = "/secrets/" + url.PathEscape(name)
	q := u.Query()
	q.Set("api-version", azureKeyVaultAPIVersion)
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *AzureKeyVaultSecretStore) encryptWithCMK(ctx context.Context, plaintext []byte) (string, string, string, string, error) {
	dataKey := make([]byte, 32)
	if _, err := rand.Read(dataKey); err != nil {
		return "", "", "", "", err
	}
	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return "", "", "", "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", "", "", err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	endpoint := azureKeyOperationURL(s.cmkKeyID, "wrapkey")
	var response struct {
		KID   string `json:"kid"`
		Value string `json:"value"`
	}
	if err := s.call(ctx, http.MethodPost, endpoint, map[string]any{
		"alg":   "RSA-OAEP-256",
		"value": base64.RawURLEncoding.EncodeToString(dataKey),
	}, &response); err != nil {
		return "", "", "", "", err
	}
	if strings.TrimSpace(response.Value) == "" {
		return "", "", "", "", fmt.Errorf("%w: Azure CMK wrap returned empty key", ErrConnectorSecretStore)
	}
	keyID := strings.TrimSpace(response.KID)
	if keyID == "" {
		keyID = s.cmkKeyID.String()
	}
	return base64.RawStdEncoding.EncodeToString(ciphertext), response.Value,
		base64.RawStdEncoding.EncodeToString(nonce), keyID, nil
}

func (s *AzureKeyVaultSecretStore) decryptWithCMK(ctx context.Context, keyID, ciphertext, wrappedKey, nonceText string) ([]byte, error) {
	parsedKey, err := parseAzureCMKKeyID(keyID, s.vaultURL, s.vaultURL.Scheme == "http")
	if err != nil {
		return nil, err
	}
	endpoint := azureKeyOperationURL(parsedKey, "unwrapkey")
	var response struct {
		Value string `json:"value"`
	}
	if err := s.call(ctx, http.MethodPost, endpoint, map[string]any{
		"alg":   "RSA-OAEP-256",
		"value": wrappedKey,
	}, &response); err != nil {
		return nil, err
	}
	dataKey, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(response.Value))
	if err != nil || len(dataKey) != 32 {
		return nil, fmt.Errorf("%w: invalid Azure CMK unwrapped key", ErrConnectorSecretStore)
	}
	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.RawStdEncoding.DecodeString(nonceText)
	if err != nil || len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("%w: invalid Azure CMK envelope nonce", ErrConnectorSecretStore)
	}
	ciphertextRaw, err := base64.RawStdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid Azure CMK envelope ciphertext", ErrConnectorSecretStore)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertextRaw, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: Azure CMK envelope decrypt failed", ErrConnectorSecretStore)
	}
	return plaintext, nil
}

func (s *AzureKeyVaultSecretStore) call(ctx context.Context, method, endpoint string, body any, output any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	token, err := s.tokens.Token(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "simple-task-manager-azure-key-vault/1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: Azure Key Vault request failed: %v", ErrConnectorSecretStore, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &azureKeyVaultAPIError{Status: resp.StatusCode, Code: "Unknown"}
		var decoded struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &decoded) == nil {
			if strings.TrimSpace(decoded.Error.Code) != "" {
				apiErr.Code = strings.TrimSpace(decoded.Error.Code)
			}
			apiErr.Message = strings.TrimSpace(decoded.Error.Message)
		}
		return apiErr
	}
	if output != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, output); err != nil {
			return fmt.Errorf("%w: invalid Azure Key Vault response", ErrConnectorSecretStore)
		}
	}
	return nil
}

func parseAzureTokenResponse(raw []byte) (string, time.Time, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var response map[string]any
	if err := decoder.Decode(&response); err != nil {
		return "", time.Time{}, fmt.Errorf("%w: invalid Azure identity response", ErrConnectorSecretStore)
	}
	token := strings.TrimSpace(fmt.Sprint(response["access_token"]))
	if token == "" || token == "<nil>" {
		return "", time.Time{}, fmt.Errorf("%w: Azure identity response missing access token", ErrConnectorSecretStore)
	}
	now := time.Now().UTC()
	expiry := now.Add(time.Hour)
	if seconds, ok := azureInt64(response["expires_in"]); ok && seconds > 0 {
		expiry = now.Add(time.Duration(seconds) * time.Second)
	} else if unix, ok := azureInt64(response["expires_on"]); ok && unix > 0 {
		expiry = time.Unix(unix, 0).UTC()
	}
	return token, expiry, nil
}

func azureInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Int64()
		return number, err == nil
	case float64:
		return int64(typed), typed > 0
	case string:
		number, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return number, err == nil
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	default:
		return 0, false
	}
}

func parseAzureVaultURL(raw string, allowInsecure bool) (*url.URL, error) {
	vaultURL, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || vaultURL.Host == "" || vaultURL.User != nil || vaultURL.Fragment != "" || vaultURL.RawQuery != "" {
		return nil, fmt.Errorf("%w: invalid Azure Key Vault URL", ErrConnectorSecretStore)
	}
	if vaultURL.Scheme != "https" && !(allowInsecure && vaultURL.Scheme == "http") {
		return nil, fmt.Errorf("%w: Azure Key Vault URL must use HTTPS", ErrConnectorSecretStore)
	}
	vaultURL.Path = ""
	return vaultURL, nil
}

func parseAzureCMKKeyID(raw string, vaultURL *url.URL, allowInsecure bool) (*url.URL, error) {
	keyURL, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || keyURL.Host == "" || keyURL.User != nil || keyURL.Fragment != "" || keyURL.RawQuery != "" {
		return nil, fmt.Errorf("%w: invalid Azure CMK key id", ErrConnectorSecretStore)
	}
	if keyURL.Scheme != "https" && !(allowInsecure && keyURL.Scheme == "http") {
		return nil, fmt.Errorf("%w: Azure CMK key id must use HTTPS", ErrConnectorSecretStore)
	}
	if !strings.EqualFold(keyURL.Host, vaultURL.Host) || !strings.HasPrefix(keyURL.Path, "/keys/") {
		return nil, fmt.Errorf("%w: Azure CMK must belong to the configured vault", ErrConnectorSecretStore)
	}
	return keyURL, nil
}

func azureKeyOperationURL(keyID *url.URL, operation string) string {
	u := *keyID
	u.Path = strings.TrimRight(u.Path, "/") + "/" + operation
	q := u.Query()
	q.Set("api-version", azureKeyVaultAPIVersion)
	u.RawQuery = q.Encode()
	return u.String()
}

func parseAzureIdentityEndpoint(raw string, allowInsecure, allowIMDS bool) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return nil, fmt.Errorf("%w: invalid Azure identity endpoint", ErrConnectorSecretStore)
	}
	if endpoint.Scheme == "https" {
		return endpoint, nil
	}
	if endpoint.Scheme != "http" {
		return nil, fmt.Errorf("%w: Azure identity endpoint must use HTTP or HTTPS", ErrConnectorSecretStore)
	}
	host := strings.Split(endpoint.Host, ":")[0]
	if allowIMDS && host == "169.254.169.254" {
		return endpoint, nil
	}
	if allowInsecure {
		return endpoint, nil
	}
	return nil, fmt.Errorf("%w: Azure identity endpoint must use HTTPS", ErrConnectorSecretStore)
}

func validAzureSecretName(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') &&
			!(r >= '0' && r <= '9') && r != '-' {
			return false
		}
	}
	return true
}

func isAzureSecretNotFound(err error) bool {
	var apiErr *azureKeyVaultAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == http.StatusNotFound || strings.EqualFold(apiErr.Code, "SecretNotFound")
}

func (s *AzureKeyVaultSecretStore) Backend() string {
	return model.ConnectorSecretBackendAzure
}
