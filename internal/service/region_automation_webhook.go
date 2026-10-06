package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
)

var ErrRegionAutomationUnavailable = errors.New("region automation planner is not configured or unavailable")

type RegionAutomationPlanReceipt struct {
	RequestID   string
	PlanID      string
	Status      string
	EvidenceURL string
	PlannedAt   time.Time
}

type RegionAutomationPlanner interface {
	PlanMigration(model.RegionMigration, model.OrganizationRegionPolicy) (RegionAutomationPlanReceipt, error)
}

type RegionAutomationWebhookConfig struct {
	Endpoint         string
	SigningSecret    string
	BearerToken      string
	Timeout          time.Duration
	RetryAttempts    int
	RetryBackoff     time.Duration
	MaxResponseBytes int64
	AllowInsecure    bool
}

type RegionAutomationWebhookPlanner struct {
	endpoint         *url.URL
	signingSecret    []byte
	bearerToken      string
	client           *http.Client
	retryAttempts    int
	retryBackoff     time.Duration
	maxResponseBytes int64
}

type regionAutomationPlanRequest struct {
	SchemaVersion int                            `json:"schema_version"`
	RequestID     string                         `json:"request_id"`
	Mode          string                         `json:"mode"`
	Action        string                         `json:"action"`
	Migration     regionAutomationMigration      `json:"migration"`
	Guardrails    regionAutomationPlanGuardrails `json:"guardrails"`
}

type regionAutomationMigration struct {
	ID                int64  `json:"id"`
	OrganizationID    int64  `json:"organization_id"`
	Scope             string `json:"scope"`
	ResourceType      string `json:"resource_type,omitempty"`
	ResourceID        string `json:"resource_id,omitempty"`
	SourceRegion      string `json:"source_region"`
	TargetRegion      string `json:"target_region"`
	Reason            string `json:"reason"`
	RequestedByUserID int64  `json:"requested_by_user_id"`
	DecidedByUserID   int64  `json:"decided_by_user_id"`
	RequestedAt       string `json:"requested_at"`
	DecidedAt         string `json:"decided_at"`
}

type regionAutomationPlanGuardrails struct {
	AllowedRegions        []string `json:"allowed_regions"`
	ResidencyEnforced     bool     `json:"residency_enforced"`
	RPOSeconds            int      `json:"rpo_seconds"`
	RTOSeconds            int      `json:"rto_seconds"`
	ExecutionRequiresGate bool     `json:"execution_requires_external_gate"`
}

type regionAutomationPlanResponse struct {
	RequestID   string `json:"request_id"`
	PlanID      string `json:"plan_id"`
	Status      string `json:"status"`
	EvidenceURL string `json:"evidence_url,omitempty"`
}

type regionAutomationAPIError struct {
	Status  int
	Code    string
	Message string
}

func (e *regionAutomationAPIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("region automation planner HTTP %d %s", e.Status, e.Code)
	}
	return fmt.Sprintf("region automation planner HTTP %d %s: %s", e.Status, e.Code, e.Message)
}

func NewRegionAutomationWebhookPlanner(cfg RegionAutomationWebhookConfig) (*RegionAutomationWebhookPlanner, error) {
	endpoint, err := parseRegionAutomationEndpoint(cfg.Endpoint, cfg.AllowInsecure)
	if err != nil {
		return nil, err
	}
	secret := strings.TrimSpace(cfg.SigningSecret)
	if len(secret) < 32 {
		return nil, fmt.Errorf("%w: REGION_AUTOMATION_SIGNING_SECRET must be at least 32 characters", ErrRegionAutomationUnavailable)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	attempts := cfg.RetryAttempts
	if attempts <= 0 {
		attempts = 3
	}
	if attempts > 6 {
		return nil, fmt.Errorf("%w: region automation retry attempts must be 1..6", ErrRegionAutomationUnavailable)
	}
	backoff := cfg.RetryBackoff
	if backoff <= 0 {
		backoff = 300 * time.Millisecond
	}
	maxResponseBytes := cfg.MaxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = 256 * 1024
	}
	if maxResponseBytes < 1024 || maxResponseBytes > 2*1024*1024 {
		return nil, fmt.Errorf("%w: region automation max response bytes must be between 1024 and 2097152", ErrRegionAutomationUnavailable)
	}
	return &RegionAutomationWebhookPlanner{
		endpoint:      endpoint,
		signingSecret: []byte(secret),
		bearerToken:   strings.TrimSpace(cfg.BearerToken),
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		retryAttempts:    attempts,
		retryBackoff:     backoff,
		maxResponseBytes: maxResponseBytes,
	}, nil
}

func NewRegionAutomationWebhookPlannerFromEnv(allowInsecure bool) (*RegionAutomationWebhookPlanner, error) {
	rawEnabled := strings.TrimSpace(os.Getenv("REGION_AUTOMATION_ENABLED"))
	if rawEnabled == "" {
		return nil, nil
	}
	enabled, err := strconv.ParseBool(rawEnabled)
	if err != nil {
		return nil, fmt.Errorf("%w: REGION_AUTOMATION_ENABLED must be true or false", ErrRegionAutomationUnavailable)
	}
	if !enabled {
		return nil, nil
	}
	timeout, err := regionAutomationDurationEnv("REGION_AUTOMATION_TIMEOUT", 20*time.Second)
	if err != nil {
		return nil, err
	}
	backoff, err := regionAutomationDurationEnv("REGION_AUTOMATION_RETRY_BACKOFF", 300*time.Millisecond)
	if err != nil {
		return nil, err
	}
	attempts, err := regionAutomationIntEnv("REGION_AUTOMATION_RETRY_ATTEMPTS", 3, 1, 6)
	if err != nil {
		return nil, err
	}
	maxResponseBytes, err := regionAutomationInt64Env("REGION_AUTOMATION_MAX_RESPONSE_BYTES", 256*1024, 1024, 2*1024*1024)
	if err != nil {
		return nil, err
	}
	return NewRegionAutomationWebhookPlanner(RegionAutomationWebhookConfig{
		Endpoint:         os.Getenv("REGION_AUTOMATION_ENDPOINT"),
		SigningSecret:    os.Getenv("REGION_AUTOMATION_SIGNING_SECRET"),
		BearerToken:      os.Getenv("REGION_AUTOMATION_BEARER_TOKEN"),
		Timeout:          timeout,
		RetryAttempts:    attempts,
		RetryBackoff:     backoff,
		MaxResponseBytes: maxResponseBytes,
		AllowInsecure:    allowInsecure,
	})
}

func (p *RegionAutomationWebhookPlanner) PlanMigration(migration model.RegionMigration, policy model.OrganizationRegionPolicy) (RegionAutomationPlanReceipt, error) {
	if migration.ID <= 0 || migration.OrganizationID <= 0 || migration.Status != model.RegionMigrationApproved ||
		migration.DecidedByUserID == nil || migration.DecidedAt == nil {
		return RegionAutomationPlanReceipt{}, fmt.Errorf("%w: migration must be explicitly approved before automation planning", ErrInvalidRegionMigration)
	}
	if !containsString(policy.AllowedRegions, migration.TargetRegion) {
		return RegionAutomationPlanReceipt{}, ErrRegionResidencyViolation
	}
	requestID, err := regionAutomationRequestID(migration, policy)
	if err != nil {
		return RegionAutomationPlanReceipt{}, err
	}
	payload := regionAutomationPlanRequest{
		SchemaVersion: 1,
		RequestID:     requestID,
		Mode:          "plan_only",
		Action:        "region_migration",
		Migration: regionAutomationMigration{
			ID: migration.ID, OrganizationID: migration.OrganizationID, Scope: migration.Scope,
			ResourceType: migration.ResourceType, ResourceID: migration.ResourceID,
			SourceRegion: migration.SourceRegion, TargetRegion: migration.TargetRegion, Reason: migration.Reason,
			RequestedByUserID: migration.RequestedByUserID, DecidedByUserID: *migration.DecidedByUserID,
			RequestedAt: migration.RequestedAt.UTC().Format(time.RFC3339Nano),
			DecidedAt:   migration.DecidedAt.UTC().Format(time.RFC3339Nano),
		},
		Guardrails: regionAutomationPlanGuardrails{
			AllowedRegions: append([]string(nil), policy.AllowedRegions...),
			ResidencyEnforced: policy.DataResidencyEnforced, RPOSeconds: policy.RPOSeconds, RTOSeconds: policy.RTOSeconds,
			ExecutionRequiresGate: true,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return RegionAutomationPlanReceipt{}, err
	}
	timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	status, responseRaw, err := p.callWithRetry(raw, requestID, timestamp)
	if err != nil {
		return RegionAutomationPlanReceipt{}, err
	}
	if status < 200 || status >= 300 {
		return RegionAutomationPlanReceipt{}, decodeRegionAutomationAPIError(status, responseRaw)
	}
	var response regionAutomationPlanResponse
	if err := json.Unmarshal(responseRaw, &response); err != nil {
		return RegionAutomationPlanReceipt{}, fmt.Errorf("%w: invalid region automation JSON response", ErrRegionAutomationUnavailable)
	}
	if response.RequestID != "" && response.RequestID != requestID {
		return RegionAutomationPlanReceipt{}, fmt.Errorf("%w: region automation request id mismatch", ErrRegionAutomationUnavailable)
	}
	response.PlanID = strings.TrimSpace(response.PlanID)
	response.Status = strings.ToLower(strings.TrimSpace(response.Status))
	if response.PlanID == "" || (response.Status != "planned" && response.Status != "accepted") {
		return RegionAutomationPlanReceipt{}, fmt.Errorf("%w: region automation response must contain a plan id and planned/accepted status", ErrRegionAutomationUnavailable)
	}
	if response.EvidenceURL != "" {
		if _, err := parseRegionAutomationEvidenceURL(response.EvidenceURL); err != nil {
			return RegionAutomationPlanReceipt{}, err
		}
	}
	return RegionAutomationPlanReceipt{
		RequestID: requestID, PlanID: response.PlanID, Status: response.Status,
		EvidenceURL: strings.TrimSpace(response.EvidenceURL), PlannedAt: time.Now().UTC(),
	}, nil
}

func (p *RegionAutomationWebhookPlanner) callWithRetry(raw []byte, requestID, timestamp string) (int, []byte, error) {
	var lastStatus int
	var lastRaw []byte
	var lastErr error
	for attempt := 0; attempt < p.retryAttempts; attempt++ {
		status, responseRaw, err := p.call(raw, requestID, timestamp)
		lastStatus, lastRaw, lastErr = status, responseRaw, err
		if err == nil && status >= 200 && status < 300 {
			return status, responseRaw, nil
		}
		if err == nil && !regionAutomationRetryableStatus(status) {
			return status, responseRaw, nil
		}
		if attempt+1 < p.retryAttempts {
			time.Sleep(p.retryBackoff * time.Duration(attempt+1))
		}
	}
	if lastErr != nil {
		return 0, nil, lastErr
	}
	return lastStatus, lastRaw, nil
}

func (p *RegionAutomationWebhookPlanner) call(raw []byte, requestID, timestamp string) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, p.endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", requestID)
	req.Header.Set("X-Region-Automation-Mode", "plan_only")
	req.Header.Set("X-Region-Automation-Timestamp", timestamp)
	req.Header.Set("X-Region-Automation-Signature", "sha256="+regionAutomationSignature(p.signingSecret, timestamp, raw))
	req.Header.Set("User-Agent", "simple-task-manager-region-automation/1")
	if p.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.bearerToken)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: region automation request failed: %v", ErrRegionAutomationUnavailable, err)
	}
	defer resp.Body.Close()
	responseRaw, err := io.ReadAll(io.LimitReader(resp.Body, p.maxResponseBytes+1))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if int64(len(responseRaw)) > p.maxResponseBytes {
		return resp.StatusCode, nil, fmt.Errorf("%w: region automation response exceeds configured byte limit", ErrRegionAutomationUnavailable)
	}
	return resp.StatusCode, responseRaw, nil
}

func regionAutomationRequestID(migration model.RegionMigration, policy model.OrganizationRegionPolicy) (string, error) {
	canonical, err := json.Marshal(struct {
		MigrationID    int64    `json:"migration_id"`
		OrganizationID int64    `json:"organization_id"`
		Scope          string   `json:"scope"`
		ResourceType   string   `json:"resource_type"`
		ResourceID     string   `json:"resource_id"`
		SourceRegion   string   `json:"source_region"`
		TargetRegion   string   `json:"target_region"`
		AllowedRegions []string `json:"allowed_regions"`
		RPOSeconds     int      `json:"rpo_seconds"`
		RTOSeconds     int      `json:"rto_seconds"`
	}{
		MigrationID: migration.ID, OrganizationID: migration.OrganizationID, Scope: migration.Scope,
		ResourceType: migration.ResourceType, ResourceID: migration.ResourceID,
		SourceRegion: migration.SourceRegion, TargetRegion: migration.TargetRegion,
		AllowedRegions: append([]string(nil), policy.AllowedRegions...),
		RPOSeconds: policy.RPOSeconds, RTOSeconds: policy.RTOSeconds,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func regionAutomationSignature(secret []byte, timestamp string, raw []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil))
}

func parseRegionAutomationEndpoint(raw string, allowInsecure bool) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" || endpoint.RawQuery != "" {
		return nil, fmt.Errorf("%w: invalid region automation endpoint", ErrRegionAutomationUnavailable)
	}
	if endpoint.Scheme != "https" && !(allowInsecure && endpoint.Scheme == "http") {
		return nil, fmt.Errorf("%w: region automation endpoint must use HTTPS", ErrRegionAutomationUnavailable)
	}
	if strings.TrimSpace(endpoint.Path) == "" || endpoint.Path == "/" {
		return nil, fmt.Errorf("%w: region automation endpoint path is required", ErrRegionAutomationUnavailable)
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/")
	return endpoint, nil
}

func parseRegionAutomationEvidenceURL(raw string) (*url.URL, error) {
	evidenceURL, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || evidenceURL.Scheme != "https" || evidenceURL.Host == "" || evidenceURL.User != nil {
		return nil, fmt.Errorf("%w: invalid region automation evidence URL", ErrRegionAutomationUnavailable)
	}
	return evidenceURL, nil
}

func decodeRegionAutomationAPIError(status int, raw []byte) error {
	apiErr := &regionAutomationAPIError{Status: status, Code: "Unknown"}
	var body struct {
		Code    string `json:"code"`
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &body) == nil {
		apiErr.Code = firstNonEmpty(strings.TrimSpace(body.Code), strings.TrimSpace(body.Error), "Unknown")
		apiErr.Message = strings.TrimSpace(body.Message)
	}
	return apiErr
}

func regionAutomationRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout ||
		status == http.StatusBadGateway || status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout || status >= 500
}

func regionAutomationDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%w: %s must be a positive duration", ErrRegionAutomationUnavailable, name)
	}
	return value, nil
}

func regionAutomationIntEnv(name string, fallback, min, max int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, fmt.Errorf("%w: %s must be between %d and %d", ErrRegionAutomationUnavailable, name, min, max)
	}
	return value, nil
}

func regionAutomationInt64Env(name string, fallback, min, max int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < min || value > max {
		return 0, fmt.Errorf("%w: %s must be between %d and %d", ErrRegionAutomationUnavailable, name, min, max)
	}
	return value, nil
}

var _ RegionAutomationPlanner = (*RegionAutomationWebhookPlanner)(nil)
