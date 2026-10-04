package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/events"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrDeveloperPlatformForbidden   = errors.New("developer platform administration requires workspace owner or admin")
	ErrInvalidDeveloperApplication  = errors.New("invalid developer application")
	ErrInvalidDeveloperLifecycle    = errors.New("invalid developer application lifecycle transition")
	ErrInvalidDeveloperCredential   = errors.New("invalid developer credential")
	ErrDeveloperAppApprovalRequired = errors.New("production credentials require an approved developer application")
	ErrDeveloperSandboxDisabled     = errors.New("developer application sandbox is disabled")
	ErrDeveloperSandboxOnly         = errors.New("sandbox credential cannot access production API routes")
	ErrInvalidDeveloperWebhookTest  = errors.New("invalid developer webhook test")
)

type DeveloperPlatformService struct {
	repo          repository.DeveloperPlatformRepository
	workspaces    repository.WorkspaceRepository
	identity      *EnterpriseIdentityService
	allowInsecure bool
	httpClient    *http.Client
	openAPISpec   []byte
}

func NewDeveloperPlatformService(
	repo repository.DeveloperPlatformRepository,
	workspaces repository.WorkspaceRepository,
	identity *EnterpriseIdentityService,
	allowInsecure bool,
	openAPISpec []byte,
) *DeveloperPlatformService {
	return &DeveloperPlatformService{
		repo: repo, workspaces: workspaces, identity: identity, allowInsecure: allowInsecure,
		httpClient:  events.NewWebhookHTTPClient(10*time.Second, allowInsecure),
		openAPISpec: append([]byte(nil), openAPISpec...),
	}
}

func (s *DeveloperPlatformService) Applications(actorUserID, workspaceID int64) ([]model.DeveloperApplication, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListApplications(workspaceID)
}

func (s *DeveloperPlatformService) Application(actorUserID, workspaceID, appID int64) (model.DeveloperApplication, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.DeveloperApplication{}, err
	}
	return s.repo.GetApplication(workspaceID, appID)
}

func (s *DeveloperPlatformService) CreateApplication(actorUserID, workspaceID int64, req model.CreateDeveloperApplicationRequest) (model.DeveloperApplication, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.DeveloperApplication{}, err
	}
	name := strings.TrimSpace(req.Name)
	scopes, err := developerNormalizeScopes(req.AllowedScopes)
	if name == "" || len(name) > 200 || err != nil || len(scopes) == 0 {
		return model.DeveloperApplication{}, ErrInvalidDeveloperApplication
	}
	daily := req.DailyRequestLimit
	if daily == 0 {
		daily = 1000
	}
	monthly := req.MonthlyRequestLimit
	if monthly == 0 {
		monthly = 20000
	}
	if daily < 1 || monthly < daily || monthly > 100000000 {
		return model.DeveloperApplication{}, ErrInvalidDeveloperApplication
	}
	sandboxEnabled := true
	if req.SandboxEnabled != nil {
		sandboxEnabled = *req.SandboxEnabled
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateApplication(model.DeveloperApplication{
		WorkspaceID: workspaceID, Name: name, Description: strings.TrimSpace(req.Description),
		Status: model.DeveloperAppStatusDraft, AllowedScopes: scopes, DailyRequestLimit: daily,
		MonthlyRequestLimit: monthly, SandboxEnabled: sandboxEnabled, CreatedByUserID: actorUserID,
		CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		_ = s.audit(workspaceID, actorUserID, "developer.app.created", "developer_application", strconv.FormatInt(item.ID, 10),
			map[string]any{"name": item.Name, "scopes": item.AllowedScopes, "daily_quota": daily, "monthly_quota": monthly}, now)
	}
	return item, err
}

func (s *DeveloperPlatformService) SubmitApplication(actorUserID, workspaceID, appID int64) (model.DeveloperApplication, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.DeveloperApplication{}, err
	}
	app, err := s.repo.GetApplication(workspaceID, appID)
	if err != nil {
		return model.DeveloperApplication{}, err
	}
	if app.Status != model.DeveloperAppStatusDraft && app.Status != model.DeveloperAppStatusRejected {
		return model.DeveloperApplication{}, ErrInvalidDeveloperLifecycle
	}
	now := time.Now().UTC()
	item, err := s.repo.UpdateApplicationLifecycle(workspaceID, appID, model.DeveloperAppStatusSubmitted, nil, "", &now, nil, now)
	if err == nil {
		_ = s.audit(workspaceID, actorUserID, "developer.app.submitted", "developer_application", strconv.FormatInt(appID, 10), nil, now)
	}
	return item, err
}

func (s *DeveloperPlatformService) ReviewApplication(actorUserID, workspaceID, appID int64, req model.ReviewDeveloperApplicationRequest) (model.DeveloperApplication, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.DeveloperApplication{}, err
	}
	app, err := s.repo.GetApplication(workspaceID, appID)
	if err != nil {
		return model.DeveloperApplication{}, err
	}
	decision := strings.ToLower(strings.TrimSpace(req.Decision))
	var status string
	switch decision {
	case "approve":
		if app.Status != model.DeveloperAppStatusSubmitted {
			return model.DeveloperApplication{}, ErrInvalidDeveloperLifecycle
		}
		status = model.DeveloperAppStatusApproved
	case "reject":
		if app.Status != model.DeveloperAppStatusSubmitted {
			return model.DeveloperApplication{}, ErrInvalidDeveloperLifecycle
		}
		status = model.DeveloperAppStatusRejected
	case "suspend":
		if app.Status != model.DeveloperAppStatusApproved {
			return model.DeveloperApplication{}, ErrInvalidDeveloperLifecycle
		}
		status = model.DeveloperAppStatusSuspended
	default:
		return model.DeveloperApplication{}, ErrInvalidDeveloperLifecycle
	}
	now := time.Now().UTC()
	reviewer := actorUserID
	item, err := s.repo.UpdateApplicationLifecycle(workspaceID, appID, status, &reviewer, strings.TrimSpace(req.Note), app.SubmittedAt, &now, now)
	if err == nil {
		_ = s.audit(workspaceID, actorUserID, "developer.app.reviewed", "developer_application", strconv.FormatInt(appID, 10),
			map[string]any{"status": status, "note": strings.TrimSpace(req.Note)}, now)
	}
	return item, err
}

func (s *DeveloperPlatformService) Credentials(actorUserID, workspaceID, appID int64) ([]model.DeveloperCredential, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListCredentials(workspaceID, appID)
}

func (s *DeveloperPlatformService) CreateCredential(actorUserID, workspaceID, appID int64, req model.CreateDeveloperCredentialRequest) (model.DeveloperCredentialSecret, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.DeveloperCredentialSecret{}, err
	}
	app, err := s.repo.GetApplication(workspaceID, appID)
	if err != nil {
		return model.DeveloperCredentialSecret{}, err
	}
	return s.createCredential(actorUserID, app, req, nil)
}

func (s *DeveloperPlatformService) RotateCredential(actorUserID, workspaceID, appID, credentialID int64) (model.DeveloperCredentialSecret, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.DeveloperCredentialSecret{}, err
	}
	app, err := s.repo.GetApplication(workspaceID, appID)
	if err != nil {
		return model.DeveloperCredentialSecret{}, err
	}
	old, err := s.repo.GetCredential(workspaceID, credentialID)
	if err != nil || old.AppID != appID || old.Status != model.DeveloperCredentialActive {
		if err != nil {
			return model.DeveloperCredentialSecret{}, err
		}
		return model.DeveloperCredentialSecret{}, ErrInvalidDeveloperCredential
	}
	req := model.CreateDeveloperCredentialRequest{
		Kind: old.Kind, Environment: old.Environment, Name: app.Name + " rotated",
		Scopes: append([]string(nil), old.Scopes...), RedirectURIs: append([]string(nil), old.RedirectURIs...),
	}
	result, err := s.createCredential(actorUserID, app, req, &old.ID)
	if err != nil {
		return model.DeveloperCredentialSecret{}, err
	}
	if err := s.revokeUnderlying(actorUserID, workspaceID, old); err != nil {
		_ = s.revokeUnderlying(actorUserID, workspaceID, result.Credential)
		_ = s.repo.RevokeCredential(workspaceID, result.Credential.ID, time.Now().UTC())
		return model.DeveloperCredentialSecret{}, err
	}
	now := time.Now().UTC()
	if err := s.repo.RevokeCredential(workspaceID, old.ID, now); err != nil {
		return model.DeveloperCredentialSecret{}, err
	}
	_ = s.audit(workspaceID, actorUserID, "developer.credential.rotated", "developer_credential", strconv.FormatInt(old.ID, 10),
		map[string]any{"replacement_id": result.Credential.ID, "kind": old.Kind, "environment": old.Environment}, now)
	return result, nil
}

func (s *DeveloperPlatformService) RevokeCredential(actorUserID, workspaceID, appID, credentialID int64) error {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return err
	}
	credential, err := s.repo.GetCredential(workspaceID, credentialID)
	if err != nil {
		return err
	}
	if credential.AppID != appID {
		return repository.ErrDeveloperCredentialNotFound
	}
	if credential.Status == model.DeveloperCredentialRevoked {
		return nil
	}
	if err := s.revokeUnderlying(actorUserID, workspaceID, credential); err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := s.repo.RevokeCredential(workspaceID, credentialID, now); err != nil {
		return err
	}
	return s.audit(workspaceID, actorUserID, "developer.credential.revoked", "developer_credential", strconv.FormatInt(credentialID, 10), nil, now)
}

func (s *DeveloperPlatformService) createCredential(actorUserID int64, app model.DeveloperApplication, req model.CreateDeveloperCredentialRequest, rotatedFrom *int64) (model.DeveloperCredentialSecret, error) {
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	environment := strings.ToLower(strings.TrimSpace(req.Environment))
	if environment == "" {
		environment = model.DeveloperEnvironmentSandbox
	}
	if kind != model.DeveloperCredentialOAuthClient && kind != model.DeveloperCredentialAPIKey {
		return model.DeveloperCredentialSecret{}, ErrInvalidDeveloperCredential
	}
	if environment != model.DeveloperEnvironmentSandbox && environment != model.DeveloperEnvironmentProduction {
		return model.DeveloperCredentialSecret{}, ErrInvalidDeveloperCredential
	}
	if environment == model.DeveloperEnvironmentProduction && app.Status != model.DeveloperAppStatusApproved {
		return model.DeveloperCredentialSecret{}, ErrDeveloperAppApprovalRequired
	}
	if environment == model.DeveloperEnvironmentSandbox {
		if !app.SandboxEnabled {
			return model.DeveloperCredentialSecret{}, ErrDeveloperSandboxDisabled
		}
		if app.Status == model.DeveloperAppStatusSuspended {
			return model.DeveloperCredentialSecret{}, ErrInvalidDeveloperCredential
		}
	}
	scopes, err := developerRequestedScopes(req.Scopes, app.AllowedScopes)
	if err != nil || len(scopes) == 0 {
		return model.DeveloperCredentialSecret{}, ErrInvalidDeveloperCredential
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = app.Name + " " + environment
	}
	now := time.Now().UTC()
	credential := model.DeveloperCredential{
		AppID: app.ID, WorkspaceID: app.WorkspaceID, Kind: kind, Environment: environment,
		Scopes: scopes, Status: model.DeveloperCredentialActive, RotatedFromID: rotatedFrom,
		CreatedByUserID: actorUserID, CreatedAt: now,
	}
	result := model.DeveloperCredentialSecret{}
	switch kind {
	case model.DeveloperCredentialOAuthClient:
		redirects := developerNormalizeStrings(req.RedirectURIs)
		if len(redirects) == 0 {
			return model.DeveloperCredentialSecret{}, ErrInvalidDeveloperCredential
		}
		oauth, err := s.identity.CreateOAuthClient(actorUserID, app.WorkspaceID, model.CreateOAuthClientRequest{
			Name: name, RedirectURIs: redirects, AllowedScopes: scopes,
		})
		if err != nil {
			return model.DeveloperCredentialSecret{}, err
		}
		credential.ExternalID = oauth.Client.ClientID
		credential.KeyPrefix = developerPrefix(oauth.Client.ClientID)
		credential.RedirectURIs = redirects
		result.ClientID = oauth.Client.ClientID
		result.ClientSecret = oauth.ClientSecret
	case model.DeveloperCredentialAPIKey:
		key, err := s.identity.CreateAPIKey(actorUserID, app.WorkspaceID, model.CreateAPIKeyRequest{
			Name: name, Scopes: scopes, ExpiresIn: req.ExpiresIn,
		})
		if err != nil {
			return model.DeveloperCredentialSecret{}, err
		}
		credential.ExternalID = fmt.Sprintf("apikey:%d", key.APIKey.ID)
		credential.KeyPrefix = key.APIKey.KeyPrefix
		result.APIKey = key.Secret
	}
	created, err := s.repo.CreateCredential(credential)
	if err != nil {
		_ = s.revokeUnderlying(actorUserID, app.WorkspaceID, credential)
		return model.DeveloperCredentialSecret{}, err
	}
	result.Credential = created
	_ = s.audit(app.WorkspaceID, actorUserID, "developer.credential.created", "developer_credential", strconv.FormatInt(created.ID, 10),
		map[string]any{"app_id": app.ID, "kind": kind, "environment": environment, "scopes": scopes}, now)
	return result, nil
}

func (s *DeveloperPlatformService) revokeUnderlying(actorUserID, workspaceID int64, credential model.DeveloperCredential) error {
	switch credential.Kind {
	case model.DeveloperCredentialOAuthClient:
		return s.identity.RevokeOAuthClient(actorUserID, workspaceID, credential.ExternalID)
	case model.DeveloperCredentialAPIKey:
		raw := strings.TrimPrefix(credential.ExternalID, "apikey:")
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return ErrInvalidDeveloperCredential
		}
		return s.identity.RevokeAPIKey(actorUserID, workspaceID, id)
	default:
		return ErrInvalidDeveloperCredential
	}
}

func (s *DeveloperPlatformService) Analytics(actorUserID, workspaceID, appID int64, days int) (model.DeveloperUsageSummary, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.DeveloperUsageSummary{}, err
	}
	app, err := s.repo.GetApplication(workspaceID, appID)
	if err != nil {
		return model.DeveloperUsageSummary{}, err
	}
	if days <= 0 {
		days = 30
	}
	if days > 366 {
		days = 366
	}
	now := time.Now().UTC()
	rangeStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(days - 1))
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	from := rangeStart
	if monthStart.Before(from) {
		from = monthStart
	}
	items, err := s.repo.Usage(workspaceID, appID, from, now)
	if err != nil {
		return model.DeveloperUsageSummary{}, err
	}
	summary := model.DeveloperUsageSummary{
		AppID: appID, DailyRequestLimit: app.DailyRequestLimit, MonthlyRequestLimit: app.MonthlyRequestLimit,
		Daily: items,
	}
	today := now.Format("2006-01-02")
	monthPrefix := now.Format("2006-01-")
	var latency int64
	for _, item := range items {
		summary.TotalRequests += item.Requests
		summary.TotalErrors += item.Errors
		latency += item.TotalLatencyMS
		if item.Date == today {
			summary.TodayRequests = item.Requests
		}
		if strings.HasPrefix(item.Date, monthPrefix) {
			summary.MonthRequests += item.Requests
		}
	}
	if summary.TotalRequests > 0 {
		summary.AverageLatencyMS = float64(latency) / float64(summary.TotalRequests)
	}
	return summary, nil
}

func (s *DeveloperPlatformService) WebhookTests(actorUserID, workspaceID, appID int64) ([]model.DeveloperWebhookTest, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListWebhookTests(workspaceID, appID, 100)
}

func (s *DeveloperPlatformService) TestWebhook(ctx context.Context, actorUserID, workspaceID, appID int64, req model.DeveloperWebhookTestRequest) (model.DeveloperWebhookTest, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.DeveloperWebhookTest{}, err
	}
	if _, err := s.repo.GetApplication(workspaceID, appID); err != nil {
		return model.DeveloperWebhookTest{}, err
	}
	endpoint, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || events.ValidateWebhookURL(endpoint, s.allowInsecure) != nil {
		return model.DeveloperWebhookTest{}, ErrInvalidDeveloperWebhookTest
	}
	eventType := strings.ToLower(strings.TrimSpace(req.EventType))
	if eventType == "" || len(eventType) > 160 {
		return model.DeveloperWebhookTest{}, ErrInvalidDeveloperWebhookTest
	}
	dryRun := true
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}
	payload := req.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	now := time.Now().UTC()
	item := model.DeveloperWebhookTest{
		AppID: appID, WorkspaceID: workspaceID, URL: endpoint.String(), EventType: eventType,
		Payload: payload, DryRun: dryRun, Status: "validated", CreatedByUserID: actorUserID, CreatedAt: now,
	}
	if !dryRun {
		body, marshalErr := json.Marshal(map[string]any{
			"id": fmt.Sprintf("developer-test-%d", now.UnixNano()), "type": eventType,
			"data": payload, "occurred_at": now,
		})
		if marshalErr != nil {
			return model.DeveloperWebhookTest{}, marshalErr
		}
		httpReq, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
		if requestErr != nil {
			return model.DeveloperWebhookTest{}, requestErr
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("User-Agent", "task-manager-developer-console/1")
		httpReq.Header.Set("X-Webhook-Test", "true")
		httpReq.Header.Set("X-Webhook-Event", eventType)
		resp, sendErr := s.httpClient.Do(httpReq)
		if sendErr != nil {
			item.Status = "failed"
			item.Error = sendErr.Error()
		} else {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			_ = resp.Body.Close()
			item.HTTPStatus = resp.StatusCode
			item.ResponsePreview = string(raw)
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				item.Status = "delivered"
			} else {
				item.Status = "failed"
				item.Error = fmt.Sprintf("webhook returned HTTP %d", resp.StatusCode)
			}
		}
	}
	created, err := s.repo.CreateWebhookTest(item)
	if err == nil {
		_ = s.audit(workspaceID, actorUserID, "developer.webhook.tested", "developer_application", strconv.FormatInt(appID, 10),
			map[string]any{"url": endpoint.String(), "event_type": eventType, "dry_run": dryRun, "status": created.Status}, now)
	}
	return created, err
}

func (s *DeveloperPlatformService) SandboxContext(clientID string, workspaceID int64) (model.DeveloperSandboxContract, error) {
	credential, app, err := s.repo.FindCredentialByExternalID(strings.TrimSpace(clientID))
	if err != nil {
		return model.DeveloperSandboxContract{}, err
	}
	if credential.WorkspaceID != workspaceID || credential.Environment != model.DeveloperEnvironmentSandbox ||
		!app.SandboxEnabled || app.Status == model.DeveloperAppStatusSuspended {
		return model.DeveloperSandboxContract{}, ErrDeveloperSandboxOnly
	}
	return model.DeveloperSandboxContract{
		AppID: app.ID, WorkspaceID: workspaceID, Isolated: true, BasePath: "/api/developer/sandbox",
		AllowedScopes: append([]string(nil), credential.Scopes...),
		Notes: []string{
			"sandbox credentials are isolated from production workspace APIs",
			"use the sandbox echo endpoint to validate authentication, scopes, quotas and request shape",
			"webhook console dry-run validates endpoint policy without transmitting data",
		},
	}, nil
}

func (s *DeveloperPlatformService) SearchDocs(query string) []model.DeveloperDocEntry {
	query = strings.ToLower(strings.TrimSpace(query))
	entries := buildDeveloperDocs(s.openAPISpec)
	if query == "" {
		if len(entries) > 50 {
			return entries[:50]
		}
		return entries
	}
	result := make([]model.DeveloperDocEntry, 0)
	for _, entry := range entries {
		haystack := strings.ToLower(entry.Path + " " + entry.Method + " " + entry.Summary + " " + entry.Description)
		if strings.Contains(haystack, query) {
			result = append(result, entry)
			if len(result) >= 50 {
				break
			}
		}
	}
	return result
}

func (s *DeveloperPlatformService) SDKs() []model.DeveloperSDK {
	return []model.DeveloperSDK{
		{Language: "TypeScript", Slug: "typescript", Path: "generated/sdk/typescript"},
		{Language: "Go", Slug: "go", Path: "generated/sdk/go"},
		{Language: "Python", Slug: "python", Path: "generated/sdk/python"},
		{Language: "Java", Slug: "java", Path: "generated/sdk/java"},
		{Language: "C#", Slug: "csharp", Path: "generated/sdk/csharp"},
	}
}

func (s *DeveloperPlatformService) AuthorizeDeveloperRequest(clientID string, workspaceID int64, path string, now time.Time) model.DeveloperRequestDecision {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return model.DeveloperRequestDecision{Allowed: true}
	}
	credential, app, err := s.repo.FindCredentialByExternalID(clientID)
	if errors.Is(err, repository.ErrDeveloperCredentialNotFound) {
		return model.DeveloperRequestDecision{Allowed: true}
	}
	if err != nil {
		return model.DeveloperRequestDecision{IsDeveloper: true, Allowed: false, StatusCode: http.StatusInternalServerError, Message: "developer credential lookup failed"}
	}
	decision := model.DeveloperRequestDecision{
		IsDeveloper: true, AppID: app.ID, WorkspaceID: app.WorkspaceID, Environment: credential.Environment,
	}
	if credential.WorkspaceID != workspaceID {
		decision.StatusCode = http.StatusForbidden
		decision.Message = "developer credential workspace mismatch"
		return decision
	}
	if app.Status == model.DeveloperAppStatusSuspended || app.Status == model.DeveloperAppStatusRejected {
		decision.StatusCode = http.StatusForbidden
		decision.Message = "developer application is not active"
		return decision
	}
	if credential.Environment == model.DeveloperEnvironmentProduction && app.Status != model.DeveloperAppStatusApproved {
		decision.StatusCode = http.StatusForbidden
		decision.Message = ErrDeveloperAppApprovalRequired.Error()
		return decision
	}
	if credential.Environment == model.DeveloperEnvironmentSandbox {
		if !app.SandboxEnabled {
			decision.StatusCode = http.StatusForbidden
			decision.Message = ErrDeveloperSandboxDisabled.Error()
			return decision
		}
		if !strings.HasPrefix(path, "/api/developer/sandbox") {
			decision.StatusCode = http.StatusForbidden
			decision.Message = ErrDeveloperSandboxOnly.Error()
			return decision
		}
	}
	_, _, err = s.repo.ConsumeRequest(clientID, workspaceID, now)
	if errors.Is(err, repository.ErrDeveloperQuotaExceeded) {
		decision.StatusCode = http.StatusTooManyRequests
		decision.Message = err.Error()
		return decision
	}
	if err != nil {
		decision.StatusCode = http.StatusInternalServerError
		decision.Message = "developer quota check failed"
		return decision
	}
	decision.Allowed = true
	return decision
}

func (s *DeveloperPlatformService) RecordDeveloperResponse(appID, workspaceID int64, statusCode int, latency time.Duration, now time.Time) error {
	return s.repo.RecordResponse(appID, workspaceID, statusCode, latency.Milliseconds(), now)
}

func (s *DeveloperPlatformService) requireAdmin(userID, workspaceID int64) error {
	access, err := s.workspaces.ResolveAccess(userID, workspaceID, time.Now())
	if err != nil || (access.Role != model.WorkspaceRoleOwner && access.Role != model.WorkspaceRoleAdmin) {
		return ErrDeveloperPlatformForbidden
	}
	return nil
}

func (s *DeveloperPlatformService) audit(workspaceID, actorID int64, action, resourceType, resourceID string, metadata map[string]any, now time.Time) error {
	wid, uid := workspaceID, actorID
	return s.workspaces.RecordAudit(model.AuditEvent{
		WorkspaceID: &wid, ActorUserID: &uid, Action: action, ResourceType: resourceType,
		ResourceID: resourceID, Metadata: metadata, CreatedAt: now,
	})
}

func developerNormalizeScopes(values []string) ([]string, error) {
	allowed := map[string]bool{
		model.ScopeTasksRead: true, model.ScopeTasksWrite: true, model.ScopeWorkspaceRead: true,
		model.ScopeWorkspaceAdmin: true, model.ScopeAuditRead: true, model.ScopeIdentityAdmin: true,
	}
	items := developerNormalizeStrings(values)
	for _, item := range items {
		if !allowed[item] {
			return nil, ErrInvalidDeveloperCredential
		}
	}
	sort.Strings(items)
	return items, nil
}

func developerRequestedScopes(requested, allowed []string) ([]string, error) {
	if len(requested) == 0 {
		return append([]string(nil), allowed...), nil
	}
	items, err := developerNormalizeScopes(requested)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if !developerContains(allowed, item) {
			return nil, ErrInvalidDeveloperCredential
		}
	}
	return items, nil
}

func developerNormalizeStrings(values []string) []string {
	seen := make(map[string]struct{})
	items := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	return items
}

func developerContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func developerPrefix(value string) string {
	if len(value) <= 14 {
		return value
	}
	return value[:14]
}

func buildDeveloperDocs(spec []byte) []model.DeveloperDocEntry {
	lines := strings.Split(string(spec), "\n")
	items := make([]model.DeveloperDocEntry, 0)
	currentPath := ""
	currentIndex := -1
	methods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "  /") && strings.HasSuffix(trimmed, ":") {
			currentPath = strings.TrimSuffix(trimmed, ":")
			currentIndex = -1
			continue
		}
		if currentPath == "" {
			continue
		}
		if strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "      ") && strings.HasSuffix(trimmed, ":") {
			method := strings.TrimSuffix(trimmed, ":")
			if methods[method] {
				items = append(items, model.DeveloperDocEntry{Path: currentPath, Method: strings.ToUpper(method)})
				currentIndex = len(items) - 1
			}
			continue
		}
		if currentIndex >= 0 && strings.HasPrefix(trimmed, "summary:") {
			items[currentIndex].Summary = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "summary:")), "\"'")
		}
	}
	return items
}
