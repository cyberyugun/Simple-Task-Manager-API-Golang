package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
)

type S3ObjectStoreConfig struct {
	Region               string
	Bucket               string
	Endpoint             string
	AccessKeyID          string
	SecretAccessKey      string
	SessionToken         string
	RoleARN              string
	WebIdentityTokenFile string
	RoleSessionName      string
	STSEndpoint          string
	ForcePathStyle       bool
	Timeout              time.Duration
	AllowInsecure        bool
	Encryption           string
	EncryptionKeyID      string
}

type S3ObjectStore struct {
	region         string
	bucket         string
	endpoint       *url.URL
	forcePathStyle bool
	client         *http.Client
	credentials    awsCredentialProvider
	encryption     string
	encryptionKey  string
}

func NewS3ObjectStore(cfg S3ObjectStoreConfig) (*S3ObjectStore, error) {
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		return nil, fmt.Errorf("%w: S3 region is required", ErrInvalidAttachment)
	}
	bucket := strings.TrimSpace(cfg.Bucket)
	if !validS3BucketName(bucket) {
		return nil, fmt.Errorf("%w: invalid S3 bucket", ErrInvalidAttachment)
	}
	endpointRaw := strings.TrimSpace(cfg.Endpoint)
	if endpointRaw == "" {
		endpointRaw = "https://s3." + region + ".amazonaws.com"
	}
	endpoint, err := parseAWSServiceEndpoint(endpointRaw, cfg.AllowInsecure)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	credentials, err := newAWSCredentialProvider(AWSSecretsManagerConfig{
		Region: region, STSEndpoint: cfg.STSEndpoint, RoleARN: cfg.RoleARN,
		WebIdentityTokenFile: cfg.WebIdentityTokenFile, RoleSessionName: cfg.RoleSessionName,
		AccessKeyID: cfg.AccessKeyID, SecretAccessKey: cfg.SecretAccessKey, SessionToken: cfg.SessionToken,
		Timeout: timeout, AllowInsecure: cfg.AllowInsecure,
	}, region, timeout)
	if err != nil {
		return nil, err
	}
	return &S3ObjectStore{
		region: region, bucket: bucket, endpoint: endpoint, forcePathStyle: cfg.ForcePathStyle,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		credentials: credentials, encryption: strings.TrimSpace(cfg.Encryption),
		encryptionKey: strings.TrimSpace(cfg.EncryptionKeyID),
	}, nil
}

func NewS3ObjectStoreFromEnv(cfg AttachmentConfig) (*S3ObjectStore, error) {
	timeout := 15 * time.Second
	if raw := strings.TrimSpace(os.Getenv("ATTACHMENT_S3_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("%w: ATTACHMENT_S3_TIMEOUT must be a positive duration", ErrInvalidAttachment)
		}
		timeout = parsed
	}
	endpoint := strings.TrimSpace(os.Getenv("ATTACHMENT_S3_ENDPOINT"))
	forcePathRaw := strings.TrimSpace(os.Getenv("ATTACHMENT_S3_FORCE_PATH_STYLE"))
	forcePathStyle := strings.EqualFold(forcePathRaw, "true")
	if forcePathRaw == "" && endpoint != "" {
		forcePathStyle = true
	}
	region := firstNonEmpty(
		os.Getenv("ATTACHMENT_S3_REGION"),
		os.Getenv("AWS_REGION"),
		os.Getenv("AWS_DEFAULT_REGION"),
	)
	return NewS3ObjectStore(S3ObjectStoreConfig{
		Region: region, Bucket: cfg.Bucket, Endpoint: endpoint,
		AccessKeyID: os.Getenv("AWS_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		SessionToken: os.Getenv("AWS_SESSION_TOKEN"), RoleARN: firstNonEmpty(os.Getenv("ATTACHMENT_S3_ROLE_ARN"), os.Getenv("AWS_ROLE_ARN")),
		WebIdentityTokenFile: firstNonEmpty(os.Getenv("ATTACHMENT_S3_WEB_IDENTITY_TOKEN_FILE"), os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE")),
		RoleSessionName:      firstNonEmpty(os.Getenv("ATTACHMENT_S3_ROLE_SESSION_NAME"), os.Getenv("AWS_ROLE_SESSION_NAME")),
		STSEndpoint:          os.Getenv("ATTACHMENT_S3_STS_ENDPOINT"), ForcePathStyle: forcePathStyle,
		Timeout: timeout, AllowInsecure: cfg.AllowInsecure,
		Encryption: cfg.Encryption, EncryptionKeyID: cfg.EncryptionKeyID,
	})
}

func NewAttachmentObjectStore(cfg AttachmentConfig) (ObjectStore, error) {
	native := strings.EqualFold(strings.TrimSpace(os.Getenv("ATTACHMENT_STORAGE_NATIVE")), "true")
	if native {
		switch strings.TrimSpace(cfg.Provider) {
		case model.AttachmentProviderS3, model.AttachmentProviderS3Compatible:
			return NewS3ObjectStoreFromEnv(cfg)
		case model.AttachmentProviderAzureBlob:
			return NewAzureBlobObjectStoreFromEnv(cfg)
		case model.AttachmentProviderGCS:
			return NewGCSObjectStoreFromEnv(cfg)
		case "":
			return nil, fmt.Errorf("%w: native attachment storage provider is required", ErrInvalidAttachment)
		default:
			return nil, fmt.Errorf("%w: native attachment storage provider %q is not implemented", ErrInvalidAttachment, cfg.Provider)
		}
	}
	return NewSignedObjectStore(cfg)
}

func (s *S3ObjectStore) PresignUpload(objectKey, contentType, sha256 string, sizeBytes int64, expiresAt time.Time) (string, map[string]string, error) {
	if strings.TrimSpace(contentType) == "" || !validSHA256(strings.TrimSpace(sha256)) || sizeBytes <= 0 {
		return "", nil, ErrInvalidAttachment
	}
	headers := map[string]string{
		"content-type":      contentType,
		"x-amz-meta-sha256": strings.ToLower(strings.TrimSpace(sha256)),
	}
	switch strings.ToLower(s.encryption) {
	case "", "none":
	case "aes256":
		headers["x-amz-server-side-encryption"] = "AES256"
	case "aws:kms", "kms":
		headers["x-amz-server-side-encryption"] = "aws:kms"
		if s.encryptionKey != "" {
			headers["x-amz-server-side-encryption-aws-kms-key-id"] = s.encryptionKey
		}
	default:
		return "", nil, fmt.Errorf("%w: unsupported S3 encryption mode", ErrInvalidAttachment)
	}
	u, err := s.presign(http.MethodPut, objectKey, headers, expiresAt)
	if err != nil {
		return "", nil, err
	}
	publicHeaders := map[string]string{
		"Content-Type":      contentType,
		"x-amz-meta-sha256": strings.ToLower(strings.TrimSpace(sha256)),
	}
	if value := headers["x-amz-server-side-encryption"]; value != "" {
		publicHeaders["x-amz-server-side-encryption"] = value
	}
	if value := headers["x-amz-server-side-encryption-aws-kms-key-id"]; value != "" {
		publicHeaders["x-amz-server-side-encryption-aws-kms-key-id"] = value
	}
	return u, publicHeaders, nil
}

func (s *S3ObjectStore) PresignDownload(objectKey string, expiresAt time.Time) (string, error) {
	return s.presign(http.MethodGet, objectKey, nil, expiresAt)
}

func (s *S3ObjectStore) VerifyUpload(ctx context.Context, objectKey string, sizeBytes int64, sha256 string) error {
	resp, err := s.doSigned(ctx, http.MethodHead, objectKey)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: S3 HEAD HTTP %d", ErrInvalidAttachment, resp.StatusCode)
	}
	if resp.ContentLength != sizeBytes {
		return fmt.Errorf("%w: S3 object size mismatch", ErrInvalidAttachment)
	}
	storedHash := strings.ToLower(strings.TrimSpace(resp.Header.Get("x-amz-meta-sha256")))
	if storedHash == "" || storedHash != strings.ToLower(strings.TrimSpace(sha256)) {
		return fmt.Errorf("%w: S3 object digest metadata mismatch", ErrInvalidAttachment)
	}
	return nil
}

func (s *S3ObjectStore) Delete(ctx context.Context, objectKey string) error {
	resp, err := s.doSigned(ctx, http.MethodDelete, objectKey)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: S3 delete HTTP %d", ErrInvalidAttachment, resp.StatusCode)
	}
	verify, err := s.doSigned(ctx, http.MethodHead, objectKey)
	if err != nil {
		return err
	}
	defer verify.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(verify.Body, 4*1024))
	if verify.StatusCode != http.StatusNotFound && verify.StatusCode != http.StatusGone {
		return fmt.Errorf("%w: S3 deletion verification HTTP %d", ErrInvalidAttachment, verify.StatusCode)
	}
	return nil
}

func (s *S3ObjectStore) presign(method, objectKey string, headers map[string]string, expiresAt time.Time) (string, error) {
	credentials, err := s.credentials.Retrieve(context.Background())
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	seconds := int64(expiresAt.Sub(now).Seconds())
	if seconds <= 0 || seconds > 7*24*60*60 {
		return "", fmt.Errorf("%w: S3 presign TTL must be between 1 second and 7 days", ErrInvalidAttachment)
	}
	u, err := s.objectURL(objectKey)
	if err != nil {
		return "", err
	}
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	scope := dateStamp + "/" + s.region + "/s3/aws4_request"
	query := u.Query()
	query.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	query.Set("X-Amz-Credential", credentials.AccessKeyID+"/"+scope)
	query.Set("X-Amz-Date", amzDate)
	query.Set("X-Amz-Expires", strconv.FormatInt(seconds, 10))
	if credentials.SessionToken != "" {
		query.Set("X-Amz-Security-Token", credentials.SessionToken)
	}

	headerKeys := []string{"host"}
	canonical := map[string]string{"host": u.Host}
	for key, value := range headers {
		lower := strings.ToLower(strings.TrimSpace(key))
		if lower == "" || lower == "host" {
			continue
		}
		headerKeys = append(headerKeys, lower)
		canonical[lower] = strings.TrimSpace(value)
	}
	sort.Strings(headerKeys)
	query.Set("X-Amz-SignedHeaders", strings.Join(headerKeys, ";"))
	u.RawQuery = query.Encode()

	var canonicalHeaders strings.Builder
	for _, key := range headerKeys {
		canonicalHeaders.WriteString(key)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(canonical[key])
		canonicalHeaders.WriteByte('\n')
	}
	canonicalRequest := strings.Join([]string{
		method, u.EscapedPath(), u.Query().Encode(), canonicalHeaders.String(),
		strings.Join(headerKeys, ";"), "UNSIGNED-PAYLOAD",
	}, "\n")
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))
	kDate := hmacSHA256([]byte("AWS4"+credentials.SecretAccessKey), dateStamp)
	kRegion := hmacSHA256(kDate, s.region)
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")
	query = u.Query()
	query.Set("X-Amz-Signature", fmt.Sprintf("%x", hmacSHA256(kSigning, stringToSign)))
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (s *S3ObjectStore) doSigned(ctx context.Context, method, objectKey string) (*http.Response, error) {
	credentials, err := s.credentials.Retrieve(ctx)
	if err != nil {
		return nil, err
	}
	u, err := s.objectURL(objectKey)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if err := signS3HeaderRequest(req, credentials, s.region, time.Now().UTC()); err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "simple-task-manager-s3-object-store/1")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: S3 request failed: %v", ErrInvalidAttachment, err)
	}
	return resp, nil
}

func (s *S3ObjectStore) objectURL(objectKey string) (*url.URL, error) {
	objectKey = strings.Trim(strings.TrimSpace(objectKey), "/")
	if !validS3ObjectKey(objectKey) {
		return nil, ErrInvalidAttachment
	}
	u := *s.endpoint
	if s.forcePathStyle {
		u.Path = path.Join(u.Path, s.bucket, objectKey)
	} else {
		u.Host = s.bucket + "." + u.Host
		u.Path = path.Join(u.Path, objectKey)
	}
	u.RawQuery = ""
	u.Fragment = ""
	return &u, nil
}

func signS3HeaderRequest(req *http.Request, credentials awsCredentials, region string, now time.Time) error {
	if credentials.AccessKeyID == "" || credentials.SecretAccessKey == "" {
		return fmt.Errorf("%w: incomplete S3 credentials", ErrInvalidAttachment)
	}
	amzDate := now.UTC().Format("20060102T150405Z")
	dateStamp := now.UTC().Format("20060102")
	payloadHash := sha256Hex(nil)
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if credentials.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", credentials.SessionToken)
	}
	headerKeys := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	if credentials.SessionToken != "" {
		headerKeys = append(headerKeys, "x-amz-security-token")
	}
	sort.Strings(headerKeys)
	var canonicalHeaders strings.Builder
	for _, key := range headerKeys {
		value := ""
		switch key {
		case "host":
			value = req.URL.Host
		default:
			value = req.Header.Get(http.CanonicalHeaderKey(key))
		}
		canonicalHeaders.WriteString(key)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(strings.TrimSpace(value))
		canonicalHeaders.WriteByte('\n')
	}
	signedHeaders := strings.Join(headerKeys, ";")
	canonicalRequest := strings.Join([]string{
		req.Method, req.URL.EscapedPath(), req.URL.Query().Encode(),
		canonicalHeaders.String(), signedHeaders, payloadHash,
	}, "\n")
	scope := dateStamp + "/" + region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))
	kDate := hmacSHA256([]byte("AWS4"+credentials.SecretAccessKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, "s3")
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := fmt.Sprintf("%x", hmacSHA256(kSigning, stringToSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+credentials.AccessKeyID+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
	return nil
}

func validS3ObjectKey(value string) bool {
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

func validS3BucketName(value string) bool {
	if len(value) < 3 || len(value) > 63 {
		return false
	}
	if value[0] == '.' || value[0] == '-' || value[len(value)-1] == '.' || value[len(value)-1] == '-' {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			continue
		}
		return false
	}
	return !strings.Contains(value, "..")
}
