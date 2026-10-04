package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
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

	"go-simple-task-api/internal/model"
)

type AWSSecretsManagerConfig struct {
	Region               string
	Prefix               string
	KMSKeyID             string
	Endpoint             string
	STSEndpoint          string
	RoleARN              string
	WebIdentityTokenFile string
	RoleSessionName      string
	AccessKeyID          string
	SecretAccessKey      string
	SessionToken         string
	RecoveryWindowDays   int
	Timeout              time.Duration
	AllowInsecure        bool
}

type awsCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Expiration      time.Time
}

type awsCredentialProvider interface {
	Retrieve(context.Context) (awsCredentials, error)
}

type awsStaticCredentialProvider struct {
	credentials awsCredentials
}

func (p awsStaticCredentialProvider) Retrieve(context.Context) (awsCredentials, error) {
	if strings.TrimSpace(p.credentials.AccessKeyID) == "" || strings.TrimSpace(p.credentials.SecretAccessKey) == "" {
		return awsCredentials{}, fmt.Errorf("%w: AWS credentials are not configured", ErrConnectorSecretStore)
	}
	return p.credentials, nil
}

type awsWebIdentityCredentialProvider struct {
	roleARN     string
	tokenFile   string
	sessionName string
	stsEndpoint *url.URL
	client      *http.Client
	mu          sync.Mutex
	cached      awsCredentials
}

func (p *awsWebIdentityCredentialProvider) Retrieve(ctx context.Context) (awsCredentials, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cached.AccessKeyID != "" && p.cached.Expiration.After(time.Now().UTC().Add(5*time.Minute)) {
		return p.cached, nil
	}
	rawToken, err := os.ReadFile(p.tokenFile)
	if err != nil {
		return awsCredentials{}, fmt.Errorf("%w: read AWS web identity token: %v", ErrConnectorSecretStore, err)
	}
	token := strings.TrimSpace(string(rawToken))
	if token == "" {
		return awsCredentials{}, fmt.Errorf("%w: AWS web identity token is empty", ErrConnectorSecretStore)
	}
	values := url.Values{}
	values.Set("Action", "AssumeRoleWithWebIdentity")
	values.Set("Version", "2011-06-15")
	values.Set("RoleArn", p.roleARN)
	values.Set("RoleSessionName", p.sessionName)
	values.Set("WebIdentityToken", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.stsEndpoint.String(), strings.NewReader(values.Encode()))
	if err != nil {
		return awsCredentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/xml")
	req.Header.Set("User-Agent", "simple-task-manager-aws-web-identity/1")
	resp, err := p.client.Do(req)
	if err != nil {
		return awsCredentials{}, fmt.Errorf("%w: AWS STS request failed: %v", ErrConnectorSecretStore, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if err != nil {
		return awsCredentials{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return awsCredentials{}, fmt.Errorf("%w: AWS STS HTTP %d", ErrConnectorSecretStore, resp.StatusCode)
	}
	var decoded struct {
		Result struct {
			Credentials struct {
				AccessKeyID     string `xml:"AccessKeyId"`
				SecretAccessKey string `xml:"SecretAccessKey"`
				SessionToken    string `xml:"SessionToken"`
				Expiration      string `xml:"Expiration"`
			} `xml:"Credentials"`
		} `xml:"AssumeRoleWithWebIdentityResult"`
	}
	if err := xml.Unmarshal(raw, &decoded); err != nil {
		return awsCredentials{}, fmt.Errorf("%w: invalid AWS STS response", ErrConnectorSecretStore)
	}
	expiration, err := time.Parse(time.RFC3339, strings.TrimSpace(decoded.Result.Credentials.Expiration))
	if err != nil {
		return awsCredentials{}, fmt.Errorf("%w: invalid AWS STS credential expiration", ErrConnectorSecretStore)
	}
	credentials := awsCredentials{
		AccessKeyID:     strings.TrimSpace(decoded.Result.Credentials.AccessKeyID),
		SecretAccessKey: strings.TrimSpace(decoded.Result.Credentials.SecretAccessKey),
		SessionToken:    strings.TrimSpace(decoded.Result.Credentials.SessionToken),
		Expiration:      expiration,
	}
	if credentials.AccessKeyID == "" || credentials.SecretAccessKey == "" || credentials.SessionToken == "" {
		return awsCredentials{}, fmt.Errorf("%w: incomplete AWS STS credentials", ErrConnectorSecretStore)
	}
	p.cached = credentials
	return credentials, nil
}

type AWSSecretsManagerSecretStore struct {
	region             string
	prefix             string
	kmsKeyID           string
	recoveryWindowDays int
	endpoint           *url.URL
	client             *http.Client
	credentials        awsCredentialProvider
}

type awsSecretsManagerAPIError struct {
	Status  int
	Code    string
	Message string
}

func (e *awsSecretsManagerAPIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("AWS Secrets Manager %s (HTTP %d)", e.Code, e.Status)
	}
	return fmt.Sprintf("AWS Secrets Manager %s: %s", e.Code, e.Message)
}

func NewAWSSecretsManagerSecretStore(cfg AWSSecretsManagerConfig) (*AWSSecretsManagerSecretStore, error) {
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		return nil, fmt.Errorf("%w: AWS region is required", ErrConnectorSecretStore)
	}
	prefix := strings.Trim(strings.TrimSpace(cfg.Prefix), "/")
	if prefix == "" {
		prefix = "simple-task-manager/connectors"
	}
	if !validAWSSecretLogicalPath(prefix) {
		return nil, fmt.Errorf("%w: invalid AWS secret prefix", ErrConnectorSecretStore)
	}
	endpointRaw := strings.TrimSpace(cfg.Endpoint)
	if endpointRaw == "" {
		endpointRaw = "https://secretsmanager." + region + ".amazonaws.com"
	}
	endpoint, err := parseAWSServiceEndpoint(endpointRaw, cfg.AllowInsecure)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	recoveryDays := cfg.RecoveryWindowDays
	if recoveryDays == 0 {
		recoveryDays = 7
	}
	if recoveryDays < 7 || recoveryDays > 30 {
		return nil, fmt.Errorf("%w: AWS recovery window must be between 7 and 30 days", ErrConnectorSecretStore)
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	credentialProvider, err := newAWSCredentialProvider(cfg, region, timeout)
	if err != nil {
		return nil, err
	}
	return &AWSSecretsManagerSecretStore{
		region: region, prefix: prefix, kmsKeyID: strings.TrimSpace(cfg.KMSKeyID),
		recoveryWindowDays: recoveryDays, endpoint: endpoint, client: client, credentials: credentialProvider,
	}, nil
}

func NewAWSSecretsManagerSecretStoreFromEnv(allowInsecure bool) (*AWSSecretsManagerSecretStore, error) {
	enabled, err := strconv.ParseBool(strings.TrimSpace(os.Getenv("CONNECTOR_AWS_SECRETS_MANAGER_ENABLED")))
	if strings.TrimSpace(os.Getenv("CONNECTOR_AWS_SECRETS_MANAGER_ENABLED")) == "" {
		enabled = false
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: CONNECTOR_AWS_SECRETS_MANAGER_ENABLED must be true or false", ErrConnectorSecretStore)
	}
	if !enabled {
		return nil, nil
	}
	timeout := 10 * time.Second
	if raw := strings.TrimSpace(os.Getenv("CONNECTOR_AWS_TIMEOUT")); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil || parsed <= 0 {
			return nil, fmt.Errorf("%w: CONNECTOR_AWS_TIMEOUT must be a positive duration", ErrConnectorSecretStore)
		}
		timeout = parsed
	}
	recoveryDays := 7
	if raw := strings.TrimSpace(os.Getenv("CONNECTOR_AWS_RECOVERY_WINDOW_DAYS")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: invalid CONNECTOR_AWS_RECOVERY_WINDOW_DAYS", ErrConnectorSecretStore)
		}
		recoveryDays = parsed
	}
	region := firstNonEmpty(
		os.Getenv("CONNECTOR_AWS_REGION"),
		os.Getenv("AWS_REGION"),
		os.Getenv("AWS_DEFAULT_REGION"),
	)
	return NewAWSSecretsManagerSecretStore(AWSSecretsManagerConfig{
		Region: region, Prefix: os.Getenv("CONNECTOR_AWS_SECRET_PREFIX"), KMSKeyID: os.Getenv("CONNECTOR_AWS_KMS_KEY_ID"),
		Endpoint: os.Getenv("CONNECTOR_AWS_SECRETS_ENDPOINT"), STSEndpoint: os.Getenv("CONNECTOR_AWS_STS_ENDPOINT"),
		RoleARN: os.Getenv("AWS_ROLE_ARN"), WebIdentityTokenFile: os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE"),
		RoleSessionName: os.Getenv("AWS_ROLE_SESSION_NAME"), AccessKeyID: os.Getenv("AWS_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"), SessionToken: os.Getenv("AWS_SESSION_TOKEN"),
		RecoveryWindowDays: recoveryDays, Timeout: timeout, AllowInsecure: allowInsecure,
	})
}

func newAWSCredentialProvider(cfg AWSSecretsManagerConfig, region string, timeout time.Duration) (awsCredentialProvider, error) {
	roleARN := strings.TrimSpace(cfg.RoleARN)
	tokenFile := strings.TrimSpace(cfg.WebIdentityTokenFile)
	if roleARN != "" || tokenFile != "" {
		if roleARN == "" || tokenFile == "" {
			return nil, fmt.Errorf("%w: AWS_ROLE_ARN and AWS_WEB_IDENTITY_TOKEN_FILE must be configured together", ErrConnectorSecretStore)
		}
		sessionName := strings.TrimSpace(cfg.RoleSessionName)
		if sessionName == "" {
			sessionName = "simple-task-manager-connectors"
		}
		stsRaw := strings.TrimSpace(cfg.STSEndpoint)
		if stsRaw == "" {
			stsRaw = "https://sts." + region + ".amazonaws.com"
		}
		stsEndpoint, err := parseAWSServiceEndpoint(stsRaw, cfg.AllowInsecure)
		if err != nil {
			return nil, err
		}
		return &awsWebIdentityCredentialProvider{
			roleARN: roleARN, tokenFile: tokenFile, sessionName: sessionName, stsEndpoint: stsEndpoint,
			client: &http.Client{
				Timeout: timeout,
				CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
					return http.ErrUseLastResponse
				},
			},
		}, nil
	}
	return awsStaticCredentialProvider{credentials: awsCredentials{
		AccessKeyID: strings.TrimSpace(cfg.AccessKeyID), SecretAccessKey: strings.TrimSpace(cfg.SecretAccessKey),
		SessionToken: strings.TrimSpace(cfg.SessionToken),
	}}, nil
}

func (s *AWSSecretsManagerSecretStore) Backend() string {
	return model.ConnectorSecretBackendAWS
}

func (s *AWSSecretsManagerSecretStore) Put(ctx context.Context, ref string, plaintext []byte) (int, error) {
	secretID, err := s.secretID(ref)
	if err != nil {
		return 0, err
	}
	version := 1
	_, currentVersion, getErr := s.Get(ctx, ref)
	switch {
	case getErr == nil:
		version = currentVersion + 1
	case isAWSSecretNotFound(getErr):
	default:
		return 0, getErr
	}
	wrapped, err := json.Marshal(map[string]any{
		"payload": base64.RawStdEncoding.EncodeToString(plaintext),
		"version": version,
	})
	if err != nil {
		return 0, err
	}
	if getErr == nil {
		err = s.call(ctx, "secretsmanager.PutSecretValue", map[string]any{
			"SecretId": secretID, "SecretString": string(wrapped),
		}, nil)
	} else {
		payload := map[string]any{"Name": secretID, "SecretString": string(wrapped)}
		if s.kmsKeyID != "" {
			payload["KmsKeyId"] = s.kmsKeyID
		}
		err = s.call(ctx, "secretsmanager.CreateSecret", payload, nil)
	}
	if err != nil {
		return 0, err
	}
	return version, nil
}

func (s *AWSSecretsManagerSecretStore) Get(ctx context.Context, ref string) ([]byte, int, error) {
	secretID, err := s.secretID(ref)
	if err != nil {
		return nil, 0, err
	}
	var response struct {
		SecretString string `json:"SecretString"`
	}
	if err := s.call(ctx, "secretsmanager.GetSecretValue", map[string]any{"SecretId": secretID}, &response); err != nil {
		return nil, 0, err
	}
	var wrapped struct {
		Payload string `json:"payload"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal([]byte(response.SecretString), &wrapped); err != nil ||
		strings.TrimSpace(wrapped.Payload) == "" || wrapped.Version <= 0 {
		return nil, 0, fmt.Errorf("%w: invalid AWS secret payload", ErrConnectorSecretStore)
	}
	plaintext, err := base64.RawStdEncoding.DecodeString(wrapped.Payload)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: invalid AWS secret encoding", ErrConnectorSecretStore)
	}
	return plaintext, wrapped.Version, nil
}

func (s *AWSSecretsManagerSecretStore) Delete(ctx context.Context, ref string) error {
	secretID, err := s.secretID(ref)
	if err != nil {
		return err
	}
	return s.call(ctx, "secretsmanager.DeleteSecret", map[string]any{
		"SecretId": secretID, "RecoveryWindowInDays": s.recoveryWindowDays,
	}, nil)
}

func (s *AWSSecretsManagerSecretStore) secretID(ref string) (string, error) {
	ref = strings.Trim(strings.TrimSpace(ref), "/")
	if !validAWSSecretLogicalPath(ref) {
		return "", fmt.Errorf("%w: invalid AWS secret reference", ErrConnectorSecretStore)
	}
	return s.prefix + "/" + ref, nil
}

func (s *AWSSecretsManagerSecretStore) call(ctx context.Context, target string, payload any, output any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("Accept", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", target)
	req.Header.Set("User-Agent", "simple-task-manager-aws-secrets-manager/1")
	credentials, err := s.credentials.Retrieve(ctx)
	if err != nil {
		return err
	}
	if err := signAWSRequestV4(req, raw, credentials, s.region, "secretsmanager", time.Now().UTC()); err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: AWS Secrets Manager request failed: %v", ErrConnectorSecretStore, err)
	}
	defer resp.Body.Close()
	responseRaw, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &awsSecretsManagerAPIError{Status: resp.StatusCode}
		var body map[string]any
		if json.Unmarshal(responseRaw, &body) == nil {
			apiErr.Code = strings.TrimPrefix(strings.TrimSpace(fmt.Sprint(body["__type"])), "#")
			if idx := strings.LastIndex(apiErr.Code, "#"); idx >= 0 {
				apiErr.Code = apiErr.Code[idx+1:]
			}
			apiErr.Message = strings.TrimSpace(fmt.Sprint(body["Message"]))
			if apiErr.Message == "<nil>" {
				apiErr.Message = ""
			}
		}
		if apiErr.Code == "" {
			apiErr.Code = "Unknown"
		}
		return apiErr
	}
	if output != nil && len(responseRaw) > 0 {
		if err := json.Unmarshal(responseRaw, output); err != nil {
			return fmt.Errorf("%w: invalid AWS Secrets Manager response", ErrConnectorSecretStore)
		}
	}
	return nil
}

func signAWSRequestV4(req *http.Request, body []byte, credentials awsCredentials, region, service string, now time.Time) error {
	if credentials.AccessKeyID == "" || credentials.SecretAccessKey == "" {
		return fmt.Errorf("%w: incomplete AWS credentials", ErrConnectorSecretStore)
	}
	amzDate := now.UTC().Format("20060102T150405Z")
	dateStamp := now.UTC().Format("20060102")
	req.Header.Set("X-Amz-Date", amzDate)
	if credentials.SessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", credentials.SessionToken)
	}
	payloadHash := sha256Hex(body)
	canonicalURI := req.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQuery := req.URL.Query().Encode()
	headerKeys := []string{"content-type", "host", "x-amz-date", "x-amz-target"}
	if credentials.SessionToken != "" {
		headerKeys = append(headerKeys, "x-amz-security-token")
	}
	sort.Strings(headerKeys)
	canonicalHeaders := strings.Builder{}
	for _, key := range headerKeys {
		var value string
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
		req.Method, canonicalURI, canonicalQuery, canonicalHeaders.String(), signedHeaders, payloadHash,
	}, "\n")
	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))
	kDate := hmacSHA256([]byte("AWS4"+credentials.SecretAccessKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+credentials.AccessKeyID+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
	return nil
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func parseAWSServiceEndpoint(raw string, allowInsecure bool) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" || endpoint.RawQuery != "" {
		return nil, fmt.Errorf("%w: invalid AWS service endpoint", ErrConnectorSecretStore)
	}
	if endpoint.Scheme != "https" && !(allowInsecure && endpoint.Scheme == "http") {
		return nil, fmt.Errorf("%w: AWS service endpoint must use HTTPS", ErrConnectorSecretStore)
	}
	return endpoint, nil
}

func validAWSSecretLogicalPath(value string) bool {
	if value == "" || strings.Contains(value, "\\") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, r := range part {
			if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') &&
				!(r >= '0' && r <= '9') && r != '-' && r != '_' && r != '+' && r != '=' && r != '.' && r != '@' {
				return false
			}
		}
	}
	return true
}

func isAWSSecretNotFound(err error) bool {
	var apiErr *awsSecretsManagerAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return strings.Contains(apiErr.Code, "ResourceNotFoundException")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
