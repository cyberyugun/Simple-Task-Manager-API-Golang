package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const gcsV4Algorithm = "GOOG4-RSA-SHA256"

type GCSObjectStoreConfig struct {
	Bucket                 string
	Endpoint               string
	ServiceAccountEmail    string
	IAMCredentialsEndpoint string
	AccessToken            string
	UseMetadata            bool
	MetadataEndpoint       string
	Timeout                time.Duration
	AllowInsecure          bool
	Encryption             string
	EncryptionKeyID        string
}

type gcsAccessTokenProvider interface {
	Token(context.Context) (string, error)
}

type gcsStaticTokenProvider struct {
	token string
}

func (p gcsStaticTokenProvider) Token(context.Context) (string, error) {
	if strings.TrimSpace(p.token) == "" {
		return "", fmt.Errorf("%w: GCS access token is not configured", ErrInvalidAttachment)
	}
	return strings.TrimSpace(p.token), nil
}

type gcsMetadataTokenProvider struct {
	endpoint  *url.URL
	client    *http.Client
	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func (p *gcsMetadataTokenProvider) Token(ctx context.Context) (string, error) {
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
	req.Header.Set("User-Agent", "simple-task-manager-gcs-workload-identity/1")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: GCS metadata token request failed: %v", ErrInvalidAttachment, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%w: GCS metadata token HTTP %d", ErrInvalidAttachment, resp.StatusCode)
	}
	var decoded struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("%w: invalid GCS metadata token response", ErrInvalidAttachment)
	}
	decoded.AccessToken = strings.TrimSpace(decoded.AccessToken)
	if decoded.AccessToken == "" || decoded.ExpiresIn <= 0 {
		return "", fmt.Errorf("%w: incomplete GCS metadata token response", ErrInvalidAttachment)
	}
	p.token = decoded.AccessToken
	p.expiresAt = time.Now().UTC().Add(time.Duration(decoded.ExpiresIn) * time.Second)
	return p.token, nil
}

type GCSObjectStore struct {
	bucket              string
	endpoint            *url.URL
	serviceAccountEmail string
	iamEndpoint         *url.URL
	client              *http.Client
	tokens              gcsAccessTokenProvider
	encryption          string
	encryptionKey       string
}

func NewGCSObjectStore(cfg GCSObjectStoreConfig) (*GCSObjectStore, error) {
	bucket := strings.TrimSpace(cfg.Bucket)
	if !validGCSBucketName(bucket) {
		return nil, fmt.Errorf("%w: invalid GCS bucket", ErrInvalidAttachment)
	}
	serviceAccountEmail := strings.TrimSpace(cfg.ServiceAccountEmail)
	if !validGCSServiceAccountEmail(serviceAccountEmail) {
		return nil, fmt.Errorf("%w: GCS service account email is required for signed URLs", ErrInvalidAttachment)
	}
	endpointRaw := strings.TrimSpace(cfg.Endpoint)
	if endpointRaw == "" {
		endpointRaw = "https://storage.googleapis.com"
	}
	endpoint, err := parseGCSServiceEndpoint(endpointRaw, cfg.AllowInsecure)
	if err != nil {
		return nil, err
	}
	iamEndpointRaw := strings.TrimSpace(cfg.IAMCredentialsEndpoint)
	if iamEndpointRaw == "" {
		iamEndpointRaw = "https://iamcredentials.googleapis.com"
	}
	iamEndpoint, err := parseGCSServiceEndpoint(iamEndpointRaw, cfg.AllowInsecure)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid GCS IAM Credentials endpoint", ErrInvalidAttachment)
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
	tokens, err := newGCSAccessTokenProvider(cfg, timeout)
	if err != nil {
		return nil, err
	}
	encryption := strings.ToLower(strings.TrimSpace(cfg.Encryption))
	encryptionKey := strings.TrimSpace(cfg.EncryptionKeyID)
	switch encryption {
	case "", "none", "google-managed", "google_managed":
	case "cmek", "gcp_cmek", "google_kms":
		if !validGCPKMSKeyName(encryptionKey) {
			return nil, fmt.Errorf("%w: valid GCS CMEK key name is required", ErrInvalidAttachment)
		}
	default:
		return nil, fmt.Errorf("%w: unsupported GCS encryption mode", ErrInvalidAttachment)
	}
	return &GCSObjectStore{
		bucket: bucket, endpoint: endpoint, serviceAccountEmail: serviceAccountEmail,
		iamEndpoint: iamEndpoint, client: client, tokens: tokens,
		encryption: encryption, encryptionKey: encryptionKey,
	}, nil
}

func NewGCSObjectStoreFromEnv(cfg AttachmentConfig) (*GCSObjectStore, error) {
	timeout := 15 * time.Second
	if raw := strings.TrimSpace(os.Getenv("ATTACHMENT_GCS_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("%w: ATTACHMENT_GCS_TIMEOUT must be a positive duration", ErrInvalidAttachment)
		}
		timeout = parsed
	}
	useMetadata := true
	if raw := strings.TrimSpace(os.Getenv("ATTACHMENT_GCS_USE_METADATA")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: ATTACHMENT_GCS_USE_METADATA must be true or false", ErrInvalidAttachment)
		}
		useMetadata = value
	}
	return NewGCSObjectStore(GCSObjectStoreConfig{
		Bucket:                 cfg.Bucket,
		Endpoint:               os.Getenv("ATTACHMENT_GCS_ENDPOINT"),
		ServiceAccountEmail:    firstNonEmpty(os.Getenv("ATTACHMENT_GCS_SERVICE_ACCOUNT_EMAIL"), os.Getenv("GOOGLE_SERVICE_ACCOUNT_EMAIL")),
		IAMCredentialsEndpoint: os.Getenv("ATTACHMENT_GCS_IAM_CREDENTIALS_ENDPOINT"),
		AccessToken:            os.Getenv("ATTACHMENT_GCS_ACCESS_TOKEN"),
		UseMetadata:            useMetadata,
		MetadataEndpoint:       os.Getenv("ATTACHMENT_GCS_METADATA_ENDPOINT"),
		Timeout:                timeout,
		AllowInsecure:          cfg.AllowInsecure,
		Encryption:             cfg.Encryption,
		EncryptionKeyID:        cfg.EncryptionKeyID,
	})
}

func newGCSAccessTokenProvider(cfg GCSObjectStoreConfig, timeout time.Duration) (gcsAccessTokenProvider, error) {
	if token := strings.TrimSpace(cfg.AccessToken); token != "" {
		return gcsStaticTokenProvider{token: token}, nil
	}
	if !cfg.UseMetadata {
		return nil, fmt.Errorf("%w: GCS workload identity metadata or access token is required", ErrInvalidAttachment)
	}
	rawEndpoint := strings.TrimSpace(cfg.MetadataEndpoint)
	if rawEndpoint == "" {
		rawEndpoint = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token"
	}
	endpoint, err := parseGCPMetadataEndpoint(rawEndpoint, cfg.AllowInsecure)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid GCS metadata endpoint", ErrInvalidAttachment)
	}
	return &gcsMetadataTokenProvider{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (s *GCSObjectStore) PresignUpload(objectKey, contentType, sha256 string, sizeBytes int64, expiresAt time.Time) (string, map[string]string, error) {
	if strings.TrimSpace(contentType) == "" || !validSHA256(strings.TrimSpace(sha256)) || sizeBytes <= 0 {
		return "", nil, ErrInvalidAttachment
	}
	headers := map[string]string{
		"content-type":       strings.TrimSpace(contentType),
		"x-goog-meta-sha256": strings.ToLower(strings.TrimSpace(sha256)),
	}
	if s.encryption == "cmek" || s.encryption == "gcp_cmek" || s.encryption == "google_kms" {
		headers["x-goog-encryption-kms-key-name"] = s.encryptionKey
	}
	signedURL, err := s.presign(context.Background(), http.MethodPut, objectKey, headers, expiresAt)
	if err != nil {
		return "", nil, err
	}
	publicHeaders := map[string]string{
		"Content-Type":       strings.TrimSpace(contentType),
		"x-goog-meta-sha256": strings.ToLower(strings.TrimSpace(sha256)),
	}
	if value := headers["x-goog-encryption-kms-key-name"]; value != "" {
		publicHeaders["x-goog-encryption-kms-key-name"] = value
	}
	return signedURL, publicHeaders, nil
}

func (s *GCSObjectStore) PresignDownload(objectKey string, expiresAt time.Time) (string, error) {
	return s.presign(context.Background(), http.MethodGet, objectKey, nil, expiresAt)
}

func (s *GCSObjectStore) VerifyUpload(ctx context.Context, objectKey string, sizeBytes int64, sha256 string) error {
	resp, err := s.doAuthorized(ctx, http.MethodHead, objectKey)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: GCS HEAD HTTP %d", ErrInvalidAttachment, resp.StatusCode)
	}
	if resp.ContentLength != sizeBytes {
		return fmt.Errorf("%w: GCS object size mismatch", ErrInvalidAttachment)
	}
	storedHash := strings.ToLower(strings.TrimSpace(resp.Header.Get("x-goog-meta-sha256")))
	if storedHash == "" || storedHash != strings.ToLower(strings.TrimSpace(sha256)) {
		return fmt.Errorf("%w: GCS object digest metadata mismatch", ErrInvalidAttachment)
	}
	return nil
}

func (s *GCSObjectStore) Delete(ctx context.Context, objectKey string) error {
	resp, err := s.doAuthorized(ctx, http.MethodDelete, objectKey)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: GCS delete HTTP %d", ErrInvalidAttachment, resp.StatusCode)
	}
	verify, err := s.doAuthorized(ctx, http.MethodHead, objectKey)
	if err != nil {
		return err
	}
	defer verify.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(verify.Body, 4*1024))
	if verify.StatusCode != http.StatusNotFound && verify.StatusCode != http.StatusGone {
		return fmt.Errorf("%w: GCS deletion verification HTTP %d", ErrInvalidAttachment, verify.StatusCode)
	}
	return nil
}

func (s *GCSObjectStore) doAuthorized(ctx context.Context, method, objectKey string) (*http.Response, error) {
	u, err := s.objectURL(objectKey)
	if err != nil {
		return nil, err
	}
	token, err := s.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "simple-task-manager-gcs-object-store/1")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: GCS request failed: %v", ErrInvalidAttachment, err)
	}
	return resp, nil
}

func (s *GCSObjectStore) presign(ctx context.Context, method, objectKey string, headers map[string]string, expiresAt time.Time) (string, error) {
	now := time.Now().UTC()
	seconds := int64(expiresAt.Sub(now).Seconds())
	if seconds <= 0 || seconds > 7*24*60*60 {
		return "", fmt.Errorf("%w: GCS presign TTL must be between 1 second and 7 days", ErrInvalidAttachment)
	}
	u, err := s.objectURL(objectKey)
	if err != nil {
		return "", err
	}
	dateTime := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	scope := dateStamp + "/auto/storage/goog4_request"

	canonicalHeaders := map[string]string{"host": u.Host}
	headerNames := []string{"host"}
	for key, value := range headers {
		lower := strings.ToLower(strings.TrimSpace(key))
		if lower == "" || lower == "host" {
			continue
		}
		canonicalHeaders[lower] = normalizeGCSHeaderValue(value)
		headerNames = append(headerNames, lower)
	}
	sort.Strings(headerNames)
	signedHeaders := strings.Join(headerNames, ";")

	query := u.Query()
	query.Set("X-Goog-Algorithm", gcsV4Algorithm)
	query.Set("X-Goog-Credential", s.serviceAccountEmail+"/"+scope)
	query.Set("X-Goog-Date", dateTime)
	query.Set("X-Goog-Expires", strconv.FormatInt(seconds, 10))
	query.Set("X-Goog-SignedHeaders", signedHeaders)
	u.RawQuery = query.Encode()

	var canonicalHeaderText strings.Builder
	for _, name := range headerNames {
		canonicalHeaderText.WriteString(name)
		canonicalHeaderText.WriteByte(':')
		canonicalHeaderText.WriteString(canonicalHeaders[name])
		canonicalHeaderText.WriteByte('\n')
	}
	canonicalRequest := strings.Join([]string{
		method,
		u.EscapedPath(),
		u.Query().Encode(),
		canonicalHeaderText.String(),
		signedHeaders,
		"UNSIGNED-PAYLOAD",
	}, "\n")
	stringToSign := strings.Join([]string{
		gcsV4Algorithm,
		dateTime,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature, err := s.signBlob(ctx, []byte(stringToSign))
	if err != nil {
		return "", err
	}
	query = u.Query()
	query.Set("X-Goog-Signature", hex.EncodeToString(signature))
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (s *GCSObjectStore) signBlob(ctx context.Context, payload []byte) ([]byte, error) {
	token, err := s.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	u := *s.iamEndpoint
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/projects/-/serviceAccounts/" + url.PathEscape(s.serviceAccountEmail) + ":signBlob"
	body, err := json.Marshal(map[string]string{
		"payload": base64.StdEncoding.EncodeToString(payload),
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "simple-task-manager-gcs-signblob/1")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: GCS signBlob request failed: %v", ErrInvalidAttachment, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: GCS signBlob HTTP %d", ErrInvalidAttachment, resp.StatusCode)
	}
	var decoded struct {
		SignedBlob string `json:"signedBlob"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("%w: invalid GCS signBlob response", ErrInvalidAttachment)
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(decoded.SignedBlob))
	if err != nil || len(signature) == 0 {
		return nil, fmt.Errorf("%w: invalid GCS signBlob signature", ErrInvalidAttachment)
	}
	return signature, nil
}

func (s *GCSObjectStore) objectURL(objectKey string) (*url.URL, error) {
	objectKey = strings.Trim(strings.TrimSpace(objectKey), "/")
	if !validGCSObjectKey(objectKey) {
		return nil, ErrInvalidAttachment
	}
	u := *s.endpoint
	u.Path = strings.TrimRight(u.Path, "/") + "/" + s.bucket + "/" + objectKey
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return &u, nil
}

func normalizeGCSHeaderValue(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func parseGCSServiceEndpoint(raw string, allowInsecure bool) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" || endpoint.RawQuery != "" {
		return nil, fmt.Errorf("%w: invalid GCS endpoint", ErrInvalidAttachment)
	}
	if endpoint.Scheme != "https" && !(allowInsecure && endpoint.Scheme == "http") {
		return nil, fmt.Errorf("%w: GCS endpoint must use HTTPS", ErrInvalidAttachment)
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/")
	return endpoint, nil
}

func validGCSServiceAccountEmail(value string) bool {
	if len(value) < 3 || len(value) > 254 || strings.ContainsAny(value, " \t\r\n/") {
		return false
	}
	at := strings.LastIndex(value, "@")
	return at > 0 && at < len(value)-3
}

func validGCSBucketName(value string) bool {
	if len(value) < 3 || len(value) > 222 {
		return false
	}
	if value[0] == '.' || value[0] == '-' || value[0] == '_' ||
		value[len(value)-1] == '.' || value[len(value)-1] == '-' || value[len(value)-1] == '_' {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[0] == '_' ||
			label[len(label)-1] == '-' || label[len(label)-1] == '_' {
			return false
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				continue
			}
			return false
		}
	}
	return true
}

func validGCSObjectKey(value string) bool {
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

var _ ObjectStore = (*GCSObjectStore)(nil)
