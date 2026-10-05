package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

const (
	defaultRemoteAIMaxResponseBytes = int64(1024 * 1024)
	defaultRemoteAIMaxRequestBytes  = int64(256 * 1024)
)

type RemoteStructuredAIProviderConfig struct {
	Endpoint                   string
	Token                      string
	Model                      string
	SupportedClassifications   []string
	Timeout                    time.Duration
	RetryAttempts              int
	RetryBackoff               time.Duration
	MaxRequestBytes            int64
	MaxResponseBytes           int64
	MaxOutputUnits             int64
	InputCostCentsPerThousand  int64
	OutputCostCentsPerThousand int64
	AllowInsecure              bool
}

type RemoteStructuredAIProvider struct {
	endpoint                 *url.URL
	token                    string
	model                    string
	supportedClassifications []string
	client                   *http.Client
	retryAttempts            int
	retryBackoff             time.Duration
	maxRequestBytes          int64
	maxResponseBytes         int64
	maxOutputUnits           int64
	inputCostPerThousand     int64
	outputCostPerThousand    int64
}

type remoteAIRequest struct {
	RequestID      string         `json:"request_id"`
	Model          string         `json:"model"`
	Feature        string         `json:"feature"`
	Classification string         `json:"classification"`
	Input          string         `json:"input"`
	Context        map[string]any `json:"context"`
	MaxOutputUnits int64          `json:"max_output_units"`
}

type remoteAIResponse struct {
	RequestID string         `json:"request_id"`
	Model     string         `json:"model"`
	Result    map[string]any `json:"result"`
	Usage     struct {
		InputUnits  int64 `json:"input_units"`
		OutputUnits int64 `json:"output_units"`
	} `json:"usage"`
}

type remoteAIAPIError struct {
	Status  int
	Code    string
	Message string
}

func (e *remoteAIAPIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("remote AI provider HTTP %d %s", e.Status, e.Code)
	}
	return fmt.Sprintf("remote AI provider HTTP %d %s: %s", e.Status, e.Code, e.Message)
}

func NewRemoteStructuredAIProvider(cfg RemoteStructuredAIProviderConfig) (*RemoteStructuredAIProvider, error) {
	endpoint, err := parseRemoteAIEndpoint(cfg.Endpoint, cfg.AllowInsecure)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, fmt.Errorf("%w: remote AI provider token is required", ErrAIProviderUnavailable)
	}
	providerModel := strings.TrimSpace(cfg.Model)
	if providerModel == "" || len(providerModel) > 200 {
		return nil, fmt.Errorf("%w: remote AI provider model is required", ErrAIProviderUnavailable)
	}

	classifications := normalizeAIClassifications(cfg.SupportedClassifications)
	if len(classifications) == 0 {
		classifications = []string{model.DataClassificationPublic, model.DataClassificationInternal}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	attempts := cfg.RetryAttempts
	if attempts <= 0 {
		attempts = 3
	}
	if attempts > 6 {
		return nil, fmt.Errorf("%w: remote AI retry attempts must be 1..6", ErrAIProviderUnavailable)
	}
	backoff := cfg.RetryBackoff
	if backoff <= 0 {
		backoff = 300 * time.Millisecond
	}
	maxRequestBytes := cfg.MaxRequestBytes
	if maxRequestBytes <= 0 {
		maxRequestBytes = defaultRemoteAIMaxRequestBytes
	}
	if maxRequestBytes < 1024 || maxRequestBytes > 4*1024*1024 {
		return nil, fmt.Errorf("%w: remote AI max request bytes must be between 1024 and 4194304", ErrAIProviderUnavailable)
	}
	maxResponseBytes := cfg.MaxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = defaultRemoteAIMaxResponseBytes
	}
	if maxResponseBytes < 1024 || maxResponseBytes > 8*1024*1024 {
		return nil, fmt.Errorf("%w: remote AI max response bytes must be between 1024 and 8388608", ErrAIProviderUnavailable)
	}
	maxOutputUnits := cfg.MaxOutputUnits
	if maxOutputUnits <= 0 {
		maxOutputUnits = 2048
	}
	if maxOutputUnits > 100000 {
		return nil, fmt.Errorf("%w: remote AI max output units is too high", ErrAIProviderUnavailable)
	}
	if cfg.InputCostCentsPerThousand < 0 || cfg.OutputCostCentsPerThousand < 0 {
		return nil, fmt.Errorf("%w: remote AI pricing cannot be negative", ErrAIProviderUnavailable)
	}

	return &RemoteStructuredAIProvider{
		endpoint:                 endpoint,
		token:                    token,
		model:                    providerModel,
		supportedClassifications: classifications,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		retryAttempts:         attempts,
		retryBackoff:          backoff,
		maxRequestBytes:       maxRequestBytes,
		maxResponseBytes:      maxResponseBytes,
		maxOutputUnits:        maxOutputUnits,
		inputCostPerThousand:  cfg.InputCostCentsPerThousand,
		outputCostPerThousand: cfg.OutputCostCentsPerThousand,
	}, nil
}

func NewRemoteStructuredAIProviderFromEnv(allowInsecure bool) (*RemoteStructuredAIProvider, error) {
	rawEnabled := strings.TrimSpace(os.Getenv("AI_REMOTE_PROVIDER_ENABLED"))
	if rawEnabled == "" {
		return nil, nil
	}
	enabled, err := strconv.ParseBool(rawEnabled)
	if err != nil {
		return nil, fmt.Errorf("%w: AI_REMOTE_PROVIDER_ENABLED must be true or false", ErrAIProviderUnavailable)
	}
	if !enabled {
		return nil, nil
	}

	timeout, err := remoteAIDurationEnv("AI_REMOTE_PROVIDER_TIMEOUT", 30*time.Second)
	if err != nil {
		return nil, err
	}
	backoff, err := remoteAIDurationEnv("AI_REMOTE_PROVIDER_RETRY_BACKOFF", 300*time.Millisecond)
	if err != nil {
		return nil, err
	}
	attempts, err := remoteAIIntEnv("AI_REMOTE_PROVIDER_RETRY_ATTEMPTS", 3, 1, 6)
	if err != nil {
		return nil, err
	}
	maxRequestBytes, err := remoteAIInt64Env("AI_REMOTE_PROVIDER_MAX_REQUEST_BYTES", defaultRemoteAIMaxRequestBytes, 1024, 4*1024*1024)
	if err != nil {
		return nil, err
	}
	maxResponseBytes, err := remoteAIInt64Env("AI_REMOTE_PROVIDER_MAX_RESPONSE_BYTES", defaultRemoteAIMaxResponseBytes, 1024, 8*1024*1024)
	if err != nil {
		return nil, err
	}
	maxOutputUnits, err := remoteAIInt64Env("AI_REMOTE_PROVIDER_MAX_OUTPUT_UNITS", 2048, 1, 100000)
	if err != nil {
		return nil, err
	}
	inputPrice, err := remoteAIInt64Env("AI_REMOTE_PROVIDER_INPUT_COST_CENTS_PER_1K_UNITS", 1, 0, 1000000)
	if err != nil {
		return nil, err
	}
	outputPrice, err := remoteAIInt64Env("AI_REMOTE_PROVIDER_OUTPUT_COST_CENTS_PER_1K_UNITS", 2, 0, 1000000)
	if err != nil {
		return nil, err
	}

	classifications := []string{}
	for _, value := range strings.Split(os.Getenv("AI_REMOTE_PROVIDER_SUPPORTED_CLASSIFICATIONS"), ",") {
		if strings.TrimSpace(value) != "" {
			classifications = append(classifications, value)
		}
	}
	return NewRemoteStructuredAIProvider(RemoteStructuredAIProviderConfig{
		Endpoint:                   os.Getenv("AI_REMOTE_PROVIDER_ENDPOINT"),
		Token:                      os.Getenv("AI_REMOTE_PROVIDER_TOKEN"),
		Model:                      os.Getenv("AI_REMOTE_PROVIDER_MODEL"),
		SupportedClassifications:   classifications,
		Timeout:                    timeout,
		RetryAttempts:              attempts,
		RetryBackoff:               backoff,
		MaxRequestBytes:            maxRequestBytes,
		MaxResponseBytes:           maxResponseBytes,
		MaxOutputUnits:             maxOutputUnits,
		InputCostCentsPerThousand:  inputPrice,
		OutputCostCentsPerThousand: outputPrice,
		AllowInsecure:              allowInsecure,
	})
}

func (p *RemoteStructuredAIProvider) Key() string {
	return model.AIProviderRemoteStructured
}

func (p *RemoteStructuredAIProvider) DisplayName() string {
	return "Remote Structured AI"
}

func (p *RemoteStructuredAIProvider) External() bool {
	return true
}

func (p *RemoteStructuredAIProvider) SupportedClassifications() []string {
	return append([]string(nil), p.supportedClassifications...)
}

func (p *RemoteStructuredAIProvider) EstimateCostCents(req AIProviderRequest) int64 {
	inputUnits := estimateRemoteAIInputUnits(req)
	return remoteAICostCents(inputUnits, p.maxOutputUnits, p.inputCostPerThousand, p.outputCostPerThousand)
}

func (p *RemoteStructuredAIProvider) Generate(ctx context.Context, req AIProviderRequest) (AIProviderResponse, error) {
	if !validAIFeature(req.Feature) || req.Feature == model.AIFeatureSemanticSearch {
		return AIProviderResponse{}, ErrInvalidAIAssistanceRequest
	}
	if !providerSupportsClassification(p, req.Classification) {
		return AIProviderResponse{}, ErrAIClassificationBlocked
	}
	requestID, err := remoteAIRequestID(p.model, req)
	if err != nil {
		return AIProviderResponse{}, err
	}
	payload := remoteAIRequest{
		RequestID:      requestID,
		Model:          p.model,
		Feature:        req.Feature,
		Classification: req.Classification,
		Input:          req.Text,
		Context:        cloneServiceMap(req.Context),
		MaxOutputUnits: p.maxOutputUnits,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return AIProviderResponse{}, err
	}
	if int64(len(raw)) > p.maxRequestBytes {
		return AIProviderResponse{}, fmt.Errorf("%w: remote AI request exceeds configured byte limit", ErrInvalidAIAssistanceRequest)
	}

	status, responseRaw, err := p.callWithRetry(ctx, raw, requestID, req.Classification)
	if err != nil {
		return AIProviderResponse{}, err
	}
	if status < 200 || status >= 300 {
		return AIProviderResponse{}, decodeRemoteAIAPIError(status, responseRaw)
	}

	var response remoteAIResponse
	if err := json.Unmarshal(responseRaw, &response); err != nil {
		return AIProviderResponse{}, fmt.Errorf("%w: invalid remote AI JSON response", ErrInvalidAIAssistanceRequest)
	}
	if response.RequestID != "" && response.RequestID != requestID {
		return AIProviderResponse{}, fmt.Errorf("%w: remote AI response request id mismatch", ErrInvalidAIAssistanceRequest)
	}
	if strings.TrimSpace(response.Model) == "" {
		response.Model = p.model
	}
	if len(response.Result) == 0 || response.Usage.InputUnits <= 0 || response.Usage.OutputUnits <= 0 {
		return AIProviderResponse{}, fmt.Errorf("%w: remote AI response is missing result or usage", ErrInvalidAIAssistanceRequest)
	}
	if response.Usage.OutputUnits > p.maxOutputUnits {
		return AIProviderResponse{}, fmt.Errorf("%w: remote AI output exceeded configured unit limit", ErrInvalidAIAssistanceRequest)
	}
	if err := validateRemoteAIResult(req.Feature, response.Result); err != nil {
		return AIProviderResponse{}, err
	}

	return AIProviderResponse{
		Model:           strings.TrimSpace(response.Model),
		Result:          response.Result,
		InputUnits:      response.Usage.InputUnits,
		OutputUnits:     response.Usage.OutputUnits,
		ActualCostCents: remoteAICostCents(response.Usage.InputUnits, response.Usage.OutputUnits, p.inputCostPerThousand, p.outputCostPerThousand),
	}, nil
}

func (p *RemoteStructuredAIProvider) callWithRetry(ctx context.Context, raw []byte, requestID, classification string) (int, []byte, error) {
	var lastStatus int
	var lastRaw []byte
	var lastErr error
	for attempt := 0; attempt < p.retryAttempts; attempt++ {
		status, responseRaw, err := p.call(ctx, raw, requestID, classification)
		lastStatus, lastRaw, lastErr = status, responseRaw, err
		if err == nil && status >= 200 && status < 300 {
			return status, responseRaw, nil
		}
		if err == nil && !remoteAIRetryableStatus(status) {
			return status, responseRaw, nil
		}
		if attempt+1 < p.retryAttempts {
			select {
			case <-ctx.Done():
				return 0, nil, ctx.Err()
			case <-time.After(p.retryBackoff * time.Duration(attempt+1)):
			}
		}
	}
	if lastErr != nil {
		return 0, nil, lastErr
	}
	return lastStatus, lastRaw, nil
}

func (p *RemoteStructuredAIProvider) call(ctx context.Context, raw []byte, requestID, classification string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", requestID)
	req.Header.Set("X-AI-Classification", classification)
	req.Header.Set("User-Agent", "simple-task-manager-remote-ai/1")

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: remote AI request failed: %v", ErrAIProviderUnavailable, err)
	}
	defer resp.Body.Close()
	responseRaw, err := io.ReadAll(io.LimitReader(resp.Body, p.maxResponseBytes+1))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if int64(len(responseRaw)) > p.maxResponseBytes {
		return resp.StatusCode, nil, fmt.Errorf("%w: remote AI response exceeds configured byte limit", ErrInvalidAIAssistanceRequest)
	}
	return resp.StatusCode, responseRaw, nil
}

func parseRemoteAIEndpoint(raw string, allowInsecure bool) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return nil, fmt.Errorf("%w: invalid remote AI provider endpoint", ErrAIProviderUnavailable)
	}
	if endpoint.RawQuery != "" {
		return nil, fmt.Errorf("%w: remote AI provider endpoint cannot contain query parameters", ErrAIProviderUnavailable)
	}
	if endpoint.Scheme != "https" && !(allowInsecure && endpoint.Scheme == "http") {
		return nil, fmt.Errorf("%w: remote AI provider endpoint must use HTTPS", ErrAIProviderUnavailable)
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/")
	if endpoint.Path == "" {
		return nil, fmt.Errorf("%w: remote AI provider endpoint path is required", ErrAIProviderUnavailable)
	}
	return endpoint, nil
}

func remoteAIRequestID(providerModel string, req AIProviderRequest) (string, error) {
	canonical, err := json.Marshal(struct {
		Model          string         `json:"model"`
		Feature        string         `json:"feature"`
		Classification string         `json:"classification"`
		Text           string         `json:"text"`
		Context        map[string]any `json:"context"`
	}{
		Model: providerModel, Feature: req.Feature, Classification: req.Classification,
		Text: req.Text, Context: req.Context,
	})
	if err != nil {
		return "", fmt.Errorf("%w: remote AI context is not JSON-serializable", ErrInvalidAIAssistanceRequest)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func estimateRemoteAIInputUnits(req AIProviderRequest) int64 {
	contextRaw, _ := json.Marshal(req.Context)
	characters := len([]rune(req.Text)) + len([]rune(string(contextRaw)))
	units := int64((characters + 3) / 4)
	if units < 1 {
		units = 1
	}
	return units
}

func remoteAICostCents(inputUnits, outputUnits, inputPerThousand, outputPerThousand int64) int64 {
	return remoteAICeilCost(inputUnits, inputPerThousand) + remoteAICeilCost(outputUnits, outputPerThousand)
}

func remoteAICeilCost(units, centsPerThousand int64) int64 {
	if units <= 0 || centsPerThousand <= 0 {
		return 0
	}
	return (units*centsPerThousand + 999) / 1000
}

func validateRemoteAIResult(feature string, result map[string]any) error {
	requireString := func(key string) error {
		value, ok := result[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: remote AI result requires non-empty %s", ErrInvalidAIAssistanceRequest, key)
		}
		return nil
	}
	requireList := func(key string) error {
		value, ok := result[key]
		if !ok {
			return fmt.Errorf("%w: remote AI result requires %s", ErrInvalidAIAssistanceRequest, key)
		}
		switch typed := value.(type) {
		case []any:
			if typed == nil {
				return fmt.Errorf("%w: remote AI result requires %s array", ErrInvalidAIAssistanceRequest, key)
			}
		case []map[string]any:
			if typed == nil {
				return fmt.Errorf("%w: remote AI result requires %s array", ErrInvalidAIAssistanceRequest, key)
			}
		default:
			return fmt.Errorf("%w: remote AI result requires %s array", ErrInvalidAIAssistanceRequest, key)
		}
		return nil
	}

	switch feature {
	case model.AIFeatureTaskSummary, model.AIFeatureProjectSummary, model.AIFeatureIncidentSummary:
		return requireString("summary")
	case model.AIFeatureDescriptionImprovement:
		return requireString("description")
	case model.AIFeatureSubtasks:
		return requireList("subtasks")
	case model.AIFeaturePrioritySuggestion:
		if err := requireString("priority"); err != nil {
			return err
		}
		priority := strings.ToUpper(strings.TrimSpace(fmt.Sprint(result["priority"])))
		if priority != model.TaskPriorityLow && priority != model.TaskPriorityMedium && priority != model.TaskPriorityHigh && priority != model.TaskPriorityUrgent {
			return fmt.Errorf("%w: remote AI result has invalid priority", ErrInvalidAIAssistanceRequest)
		}
		return nil
	case model.AIFeatureDuplicateSuggestion:
		return requireList("candidates")
	case model.AIFeatureRiskWorkflowSuggestion:
		if err := requireList("risks"); err != nil {
			return err
		}
		return requireList("workflow_suggestions")
	case model.AIFeatureNaturalLanguageReport:
		return requireString("report")
	default:
		return ErrInvalidAIAssistanceRequest
	}
}

func decodeRemoteAIAPIError(status int, raw []byte) error {
	apiErr := &remoteAIAPIError{Status: status, Code: "Unknown"}
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

func remoteAIRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout ||
		status == http.StatusBadGateway || status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout || status >= 500
}

func remoteAIDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%w: %s must be a positive duration", ErrAIProviderUnavailable, name)
	}
	return value, nil
}

func remoteAIIntEnv(name string, fallback, min, max int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, fmt.Errorf("%w: %s must be between %d and %d", ErrAIProviderUnavailable, name, min, max)
	}
	return value, nil
}

func remoteAIInt64Env(name string, fallback, min, max int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < min || value > max {
		return 0, fmt.Errorf("%w: %s must be between %d and %d", ErrAIProviderUnavailable, name, min, max)
	}
	return value, nil
}

var _ AIProvider = (*RemoteStructuredAIProvider)(nil)
