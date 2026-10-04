package service

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const azureBlobAPIVersion = "2023-11-03"

type AzureBlobObjectStoreConfig struct {
	AccountName             string
	AccountKey              string
	Container               string
	Endpoint                string
	AccessToken             string
	TenantID                string
	ClientID                string
	FederatedTokenFile      string
	AuthorityHost           string
	UseManagedIdentity      bool
	ManagedIdentityEndpoint string
	Timeout                 time.Duration
	AllowInsecure           bool
	Encryption              string
	EncryptionKeyID         string
}

type azureStorageTokenProvider interface {
	Token(context.Context) (string, error)
}

type azureStorageStaticTokenProvider struct {
	token string
}

func (p azureStorageStaticTokenProvider) Token(context.Context) (string, error) {
	if strings.TrimSpace(p.token) == "" {
		return "", fmt.Errorf("%w: Azure Storage access token is not configured", ErrInvalidAttachment)
	}
	return strings.TrimSpace(p.token), nil
}

type azureStorageWorkloadIdentityProvider struct {
	clientID      string
	tokenFile     string
	tokenEndpoint *url.URL
	client        *http.Client
	mu            sync.Mutex
	token         string
	expiresAt     time.Time
}

func (p *azureStorageWorkloadIdentityProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.token != "" && p.expiresAt.After(time.Now().UTC().Add(5*time.Minute)) {
		return p.token, nil
	}
	assertion, err := os.ReadFile(p.tokenFile)
	if err != nil {
		return "", fmt.Errorf("%w: read Azure Storage federated token: %v", ErrInvalidAttachment, err)
	}
	if strings.TrimSpace(string(assertion)) == "" {
		return "", fmt.Errorf("%w: Azure Storage federated token is empty", ErrInvalidAttachment)
	}
	values := url.Values{}
	values.Set("client_id", p.clientID)
	values.Set("scope", "https://storage.azure.com/.default")
	values.Set("grant_type", "client_credentials")
	values.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
	values.Set("client_assertion", strings.TrimSpace(string(assertion)))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenEndpoint.String(), strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "simple-task-manager-azure-storage-workload-identity/1")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: Azure Storage workload identity token request failed: %v", ErrInvalidAttachment, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: Azure Storage workload identity token HTTP %d", ErrInvalidAttachment, resp.StatusCode)
	}
	token, expiry, err := parseAzureTokenResponse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidAttachment, err)
	}
	p.token = token
	p.expiresAt = expiry
	return token, nil
}

type azureStorageManagedIdentityProvider struct {
	clientID  string
	endpoint  *url.URL
	client    *http.Client
	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func (p *azureStorageManagedIdentityProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.token != "" && p.expiresAt.After(time.Now().UTC().Add(5*time.Minute)) {
		return p.token, nil
	}
	u := *p.endpoint
	q := u.Query()
	q.Set("api-version", "2018-02-01")
	q.Set("resource", "https://storage.azure.com/")
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
	req.Header.Set("User-Agent", "simple-task-manager-azure-storage-managed-identity/1")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: Azure Storage managed identity token request failed: %v", ErrInvalidAttachment, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: Azure Storage managed identity token HTTP %d", ErrInvalidAttachment, resp.StatusCode)
	}
	token, expiry, err := parseAzureTokenResponse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidAttachment, err)
	}
	p.token = token
	p.expiresAt = expiry
	return token, nil
}

type azureUserDelegationKey struct {
	SignedOID     string `xml:"SignedOid"`
	SignedTID     string `xml:"SignedTid"`
	SignedStart   string `xml:"SignedStart"`
	SignedExpiry  string `xml:"SignedExpiry"`
	SignedService string `xml:"SignedService"`
	SignedVersion string `xml:"SignedVersion"`
	Value         string `xml:"Value"`
}

type AzureBlobObjectStore struct {
	accountName    string
	container      string
	endpoint       *url.URL
	accountKey     []byte
	tokens         azureStorageTokenProvider
	client         *http.Client
	encryption     string
	encryptionKey  string
	delegationMu   sync.Mutex
	delegationKey  azureUserDelegationKey
	delegationEnd  time.Time
	allowInsecure  bool
}

func NewAzureBlobObjectStore(cfg AzureBlobObjectStoreConfig) (*AzureBlobObjectStore, error) {
	account := strings.TrimSpace(cfg.AccountName)
	if !validAzureStorageAccountName(account) {
		return nil, fmt.Errorf("%w: invalid Azure Storage account name", ErrInvalidAttachment)
	}
	container := strings.TrimSpace(cfg.Container)
	if !validAzureBlobContainer(container) {
		return nil, fmt.Errorf("%w: invalid Azure Blob container", ErrInvalidAttachment)
	}
	endpointRaw := strings.TrimSpace(cfg.Endpoint)
	if endpointRaw == "" {
		endpointRaw = "https://" + account + ".blob.core.windows.net"
	}
	endpoint, err := parseAzureBlobEndpoint(endpointRaw, cfg.AllowInsecure)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	store := &AzureBlobObjectStore{
		accountName: account, container: container, endpoint: endpoint, client: client,
		encryption: strings.TrimSpace(cfg.Encryption), encryptionKey: strings.TrimSpace(cfg.EncryptionKeyID),
		allowInsecure: cfg.AllowInsecure,
	}
	if rawKey := strings.TrimSpace(cfg.AccountKey); rawKey != "" {
		key, err := base64.StdEncoding.DecodeString(rawKey)
		if err != nil || len(key) < 16 {
			return nil, fmt.Errorf("%w: invalid Azure Storage account key", ErrInvalidAttachment)
		}
		store.accountKey = key
		return store, nil
	}
	tokens, err := newAzureStorageTokenProvider(cfg, client)
	if err != nil {
		return nil, err
	}
	store.tokens = tokens
	return store, nil
}

func NewAzureBlobObjectStoreFromEnv(cfg AttachmentConfig) (*AzureBlobObjectStore, error) {
	timeout := 15 * time.Second
	if raw := strings.TrimSpace(os.Getenv("ATTACHMENT_AZURE_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("%w: ATTACHMENT_AZURE_TIMEOUT must be a positive duration", ErrInvalidAttachment)
		}
		timeout = parsed
	}
	useManagedIdentity := false
	if raw := strings.TrimSpace(os.Getenv("ATTACHMENT_AZURE_USE_MANAGED_IDENTITY")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: ATTACHMENT_AZURE_USE_MANAGED_IDENTITY must be true or false", ErrInvalidAttachment)
		}
		useManagedIdentity = value
	}
	return NewAzureBlobObjectStore(AzureBlobObjectStoreConfig{
		AccountName:             os.Getenv("ATTACHMENT_AZURE_STORAGE_ACCOUNT"),
		AccountKey:              os.Getenv("ATTACHMENT_AZURE_STORAGE_ACCOUNT_KEY"),
		Container:               cfg.Bucket,
		Endpoint:                os.Getenv("ATTACHMENT_AZURE_BLOB_ENDPOINT"),
		AccessToken:             os.Getenv("ATTACHMENT_AZURE_ACCESS_TOKEN"),
		TenantID:                os.Getenv("AZURE_TENANT_ID"),
		ClientID:                os.Getenv("AZURE_CLIENT_ID"),
		FederatedTokenFile:      os.Getenv("AZURE_FEDERATED_TOKEN_FILE"),
		AuthorityHost:           os.Getenv("AZURE_AUTHORITY_HOST"),
		UseManagedIdentity:      useManagedIdentity,
		ManagedIdentityEndpoint: os.Getenv("ATTACHMENT_AZURE_MANAGED_IDENTITY_ENDPOINT"),
		Timeout:                 timeout,
		AllowInsecure:           cfg.AllowInsecure,
		Encryption:              cfg.Encryption,
		EncryptionKeyID:         cfg.EncryptionKeyID,
	})
}

func newAzureStorageTokenProvider(cfg AzureBlobObjectStoreConfig, client *http.Client) (azureStorageTokenProvider, error) {
	if token := strings.TrimSpace(cfg.AccessToken); token != "" {
		return azureStorageStaticTokenProvider{token: token}, nil
	}
	tenantID := strings.TrimSpace(cfg.TenantID)
	clientID := strings.TrimSpace(cfg.ClientID)
	tokenFile := strings.TrimSpace(cfg.FederatedTokenFile)
	if tenantID != "" || tokenFile != "" {
		if tenantID == "" || clientID == "" || tokenFile == "" {
			return nil, fmt.Errorf("%w: Azure Storage workload identity requires tenant id, client id and federated token file", ErrInvalidAttachment)
		}
		authority := strings.TrimRight(strings.TrimSpace(cfg.AuthorityHost), "/")
		if authority == "" {
			authority = "https://login.microsoftonline.com"
		}
		endpoint, err := parseAzureIdentityEndpoint(authority+"/"+url.PathEscape(tenantID)+"/oauth2/v2.0/token", cfg.AllowInsecure, false)
		if err != nil {
			return nil, err
		}
		return &azureStorageWorkloadIdentityProvider{
			clientID: clientID, tokenFile: tokenFile, tokenEndpoint: endpoint, client: client,
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
		return &azureStorageManagedIdentityProvider{clientID: clientID, endpoint: endpoint, client: client}, nil
	}
	return nil, fmt.Errorf("%w: Azure Storage account key, workload identity, managed identity or access token is required", ErrInvalidAttachment)
}

func (s *AzureBlobObjectStore) PresignUpload(objectKey, contentType, sha256 string, sizeBytes int64, expiresAt time.Time) (string, map[string]string, error) {
	if strings.TrimSpace(contentType) == "" || !validSHA256(strings.TrimSpace(sha256)) || sizeBytes <= 0 {
		return "", nil, ErrInvalidAttachment
	}
	u, err := s.presign(context.Background(), objectKey, "cw", expiresAt)
	if err != nil {
		return "", nil, err
	}
	headers := map[string]string{
		"Content-Type":       contentType,
		"x-ms-blob-type":     "BlockBlob",
		"x-ms-meta-sha256":   strings.ToLower(strings.TrimSpace(sha256)),
		"x-ms-version":       azureBlobAPIVersion,
	}
	switch strings.ToLower(strings.TrimSpace(s.encryption)) {
	case "", "none", "microsoft-managed":
	case "cmk", "encryption_scope", "azure_cmk":
		if s.encryptionKey == "" {
			return "", nil, fmt.Errorf("%w: Azure encryption scope is required", ErrInvalidAttachment)
		}
		headers["x-ms-encryption-scope"] = s.encryptionKey
	default:
		return "", nil, fmt.Errorf("%w: unsupported Azure Blob encryption mode", ErrInvalidAttachment)
	}
	return u, headers, nil
}

func (s *AzureBlobObjectStore) PresignDownload(objectKey string, expiresAt time.Time) (string, error) {
	return s.presign(context.Background(), objectKey, "r", expiresAt)
}

func (s *AzureBlobObjectStore) VerifyUpload(ctx context.Context, objectKey string, sizeBytes int64, sha256 string) error {
	resp, err := s.doAuthorized(ctx, http.MethodHead, objectKey, "r")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: Azure Blob HEAD HTTP %d", ErrInvalidAttachment, resp.StatusCode)
	}
	if resp.ContentLength != sizeBytes {
		return fmt.Errorf("%w: Azure Blob object size mismatch", ErrInvalidAttachment)
	}
	storedHash := strings.ToLower(strings.TrimSpace(resp.Header.Get("x-ms-meta-sha256")))
	if storedHash == "" || storedHash != strings.ToLower(strings.TrimSpace(sha256)) {
		return fmt.Errorf("%w: Azure Blob digest metadata mismatch", ErrInvalidAttachment)
	}
	return nil
}

func (s *AzureBlobObjectStore) Delete(ctx context.Context, objectKey string) error {
	resp, err := s.doAuthorized(ctx, http.MethodDelete, objectKey, "d")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: Azure Blob delete HTTP %d", ErrInvalidAttachment, resp.StatusCode)
	}
	verify, err := s.doAuthorized(ctx, http.MethodHead, objectKey, "r")
	if err != nil {
		return err
	}
	defer verify.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(verify.Body, 4*1024))
	if verify.StatusCode != http.StatusNotFound && verify.StatusCode != http.StatusGone {
		return fmt.Errorf("%w: Azure Blob deletion verification HTTP %d", ErrInvalidAttachment, verify.StatusCode)
	}
	return nil
}

func (s *AzureBlobObjectStore) doAuthorized(ctx context.Context, method, objectKey, permission string) (*http.Response, error) {
	u, err := s.objectURL(objectKey)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-ms-version", azureBlobAPIVersion)
	req.Header.Set("x-ms-date", time.Now().UTC().Format(http.TimeFormat))
	req.Header.Set("User-Agent", "simple-task-manager-azure-blob/1")
	if len(s.accountKey) > 0 {
		signed, err := s.presign(ctx, objectKey, permission, time.Now().UTC().Add(5*time.Minute))
		if err != nil {
			return nil, err
		}
		req.URL, err = url.Parse(signed)
		if err != nil {
			return nil, err
		}
	} else {
		token, err := s.tokens.Token(ctx)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: Azure Blob request failed: %v", ErrInvalidAttachment, err)
	}
	return resp, nil
}

func (s *AzureBlobObjectStore) presign(ctx context.Context, objectKey, permission string, expiresAt time.Time) (string, error) {
	if expiresAt.Before(time.Now().UTC().Add(time.Second)) || expiresAt.After(time.Now().UTC().Add(7*24*time.Hour)) {
		return "", fmt.Errorf("%w: Azure Blob SAS TTL must be between 1 second and 7 days", ErrInvalidAttachment)
	}
	u, err := s.objectURL(objectKey)
	if err != nil {
		return "", err
	}
	start := time.Now().UTC().Add(-5 * time.Minute)
	if len(s.accountKey) > 0 {
		return s.serviceSAS(u, objectKey, permission, start, expiresAt), nil
	}
	key, err := s.userDelegationKey(ctx, expiresAt)
	if err != nil {
		return "", err
	}
	return s.userDelegationSAS(u, objectKey, permission, start, expiresAt, key)
}

func (s *AzureBlobObjectStore) serviceSAS(u *url.URL, objectKey, permission string, start, expiry time.Time) string {
	st := azureSASTime(start)
	se := azureSASTime(expiry)
	spr := "https"
	if u.Scheme == "http" {
		spr = "https,http"
	}
	canonicalResource := "/blob/" + s.accountName + "/" + s.container + "/" + objectKey
	encryptionScope := s.signedEncryptionScope(permission)
	stringToSign := strings.Join([]string{
		permission, st, se, canonicalResource, "", "", spr, azureBlobAPIVersion, "b", "", encryptionScope, "", "", "", "", "",
	}, "\n")
	signature := base64.StdEncoding.EncodeToString(hmacSHA256(s.accountKey, stringToSign))
	q := u.Query()
	q.Set("sv", azureBlobAPIVersion)
	q.Set("spr", spr)
	q.Set("st", st)
	q.Set("se", se)
	q.Set("sr", "b")
	q.Set("sp", permission)
	if encryptionScope != "" {
		q.Set("ses", encryptionScope)
	}
	q.Set("sig", signature)
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *AzureBlobObjectStore) userDelegationSAS(u *url.URL, objectKey, permission string, start, expiry time.Time, key azureUserDelegationKey) (string, error) {
	rawKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key.Value))
	if err != nil || len(rawKey) < 16 {
		return "", fmt.Errorf("%w: invalid Azure user delegation key", ErrInvalidAttachment)
	}
	st := azureSASTime(start)
	se := azureSASTime(expiry)
	spr := "https"
	if u.Scheme == "http" {
		spr = "https,http"
	}
	canonicalResource := "/blob/" + s.accountName + "/" + s.container + "/" + objectKey
	encryptionScope := s.signedEncryptionScope(permission)
	stringToSign := strings.Join([]string{
		permission, st, se, canonicalResource,
		key.SignedOID, key.SignedTID, key.SignedStart, key.SignedExpiry, key.SignedService, key.SignedVersion,
		"", "", "", "", spr, azureBlobAPIVersion, "b", "", encryptionScope, "", "", "", "", "",
	}, "\n")
	signature := base64.StdEncoding.EncodeToString(hmacSHA256(rawKey, stringToSign))
	q := u.Query()
	q.Set("sv", azureBlobAPIVersion)
	q.Set("spr", spr)
	q.Set("st", st)
	q.Set("se", se)
	q.Set("sr", "b")
	q.Set("sp", permission)
	q.Set("skoid", key.SignedOID)
	q.Set("sktid", key.SignedTID)
	q.Set("skt", key.SignedStart)
	q.Set("ske", key.SignedExpiry)
	q.Set("sks", key.SignedService)
	q.Set("skv", key.SignedVersion)
	if encryptionScope != "" {
		q.Set("ses", encryptionScope)
	}
	q.Set("sig", signature)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s *AzureBlobObjectStore) signedEncryptionScope(permission string) string {
	switch strings.ToLower(strings.TrimSpace(s.encryption)) {
	case "cmk", "encryption_scope", "azure_cmk":
		if strings.Contains(permission, "w") || strings.Contains(permission, "c") {
			return s.encryptionKey
		}
	}
	return ""
}

func (s *AzureBlobObjectStore) userDelegationKey(ctx context.Context, neededExpiry time.Time) (azureUserDelegationKey, error) {
	s.delegationMu.Lock()
	defer s.delegationMu.Unlock()
	if s.delegationKey.Value != "" && s.delegationEnd.After(neededExpiry.Add(5*time.Minute)) {
		return s.delegationKey, nil
	}
	now := time.Now().UTC()
	keyStart := now.Add(-5 * time.Minute)
	keyExpiry := neededExpiry.Add(time.Hour)
	maxExpiry := keyStart.Add(7 * 24 * time.Hour)
	if keyExpiry.After(maxExpiry) {
		keyExpiry = maxExpiry
	}
	body := struct {
		XMLName xml.Name `xml:"KeyInfo"`
		Start   string   `xml:"Start"`
		Expiry  string   `xml:"Expiry"`
	}{
		Start: azureSASTime(keyStart), Expiry: azureSASTime(keyExpiry),
	}
	rawBody, err := xml.Marshal(body)
	if err != nil {
		return azureUserDelegationKey{}, err
	}
	u := *s.endpoint
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	q := u.Query()
	q.Set("restype", "service")
	q.Set("comp", "userdelegationkey")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(string(rawBody)))
	if err != nil {
		return azureUserDelegationKey{}, err
	}
	token, err := s.tokens.Token(ctx)
	if err != nil {
		return azureUserDelegationKey{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/xml")
	req.Header.Set("x-ms-version", azureBlobAPIVersion)
	req.Header.Set("x-ms-date", now.Format(http.TimeFormat))
	req.Header.Set("User-Agent", "simple-task-manager-azure-blob/1")
	resp, err := s.client.Do(req)
	if err != nil {
		return azureUserDelegationKey{}, fmt.Errorf("%w: Azure user delegation key request failed: %v", ErrInvalidAttachment, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if err != nil {
		return azureUserDelegationKey{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return azureUserDelegationKey{}, fmt.Errorf("%w: Azure user delegation key HTTP %d", ErrInvalidAttachment, resp.StatusCode)
	}
	var key azureUserDelegationKey
	if err := xml.Unmarshal(raw, &key); err != nil {
		return azureUserDelegationKey{}, fmt.Errorf("%w: invalid Azure user delegation key response", ErrInvalidAttachment)
	}
	if key.SignedOID == "" || key.SignedTID == "" || key.SignedStart == "" || key.SignedExpiry == "" ||
		key.SignedService == "" || key.SignedVersion == "" || key.Value == "" {
		return azureUserDelegationKey{}, fmt.Errorf("%w: incomplete Azure user delegation key response", ErrInvalidAttachment)
	}
	parsedExpiry, err := time.Parse(time.RFC3339, key.SignedExpiry)
	if err != nil {
		return azureUserDelegationKey{}, fmt.Errorf("%w: invalid Azure user delegation expiry", ErrInvalidAttachment)
	}
	s.delegationKey = key
	s.delegationEnd = parsedExpiry.UTC()
	return key, nil
}

func (s *AzureBlobObjectStore) objectURL(objectKey string) (*url.URL, error) {
	objectKey = strings.Trim(strings.TrimSpace(objectKey), "/")
	if !validAzureBlobObjectKey(objectKey) {
		return nil, ErrInvalidAttachment
	}
	u := *s.endpoint
	u.Path = strings.TrimRight(u.Path, "/") + "/" + s.container + "/" + objectKey
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return &u, nil
}

func azureSASTime(value time.Time) string {
	return value.UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z")
}

func parseAzureBlobEndpoint(raw string, allowInsecure bool) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" || endpoint.RawQuery != "" {
		return nil, fmt.Errorf("%w: invalid Azure Blob endpoint", ErrInvalidAttachment)
	}
	if endpoint.Scheme != "https" && !(allowInsecure && endpoint.Scheme == "http") {
		return nil, fmt.Errorf("%w: Azure Blob endpoint must use HTTPS", ErrInvalidAttachment)
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/")
	return endpoint, nil
}

func validAzureStorageAccountName(value string) bool {
	if len(value) < 3 || len(value) > 24 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func validAzureBlobContainer(value string) bool {
	if len(value) < 3 || len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' || strings.Contains(value, "--") {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validAzureBlobObjectKey(value string) bool {
	if value == "" || strings.Contains(value, "\\") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, r := range part {
			if r < 0x20 || r == 0x7f {
				return false
			}
		}
	}
	return true
}
