package repository

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrDeveloperAppNotFound        = errors.New("developer application not found")
	ErrDeveloperCredentialNotFound = errors.New("developer credential not found")
	ErrDeveloperQuotaExceeded      = errors.New("developer application quota exceeded")
)

type DeveloperPlatformRepository interface {
	CreateApplication(model.DeveloperApplication) (model.DeveloperApplication, error)
	ListApplications(workspaceID int64) ([]model.DeveloperApplication, error)
	GetApplication(workspaceID, appID int64) (model.DeveloperApplication, error)
	UpdateApplicationLifecycle(workspaceID, appID int64, status string, reviewerID *int64, note string, submittedAt, reviewedAt *time.Time, now time.Time) (model.DeveloperApplication, error)

	CreateCredential(model.DeveloperCredential) (model.DeveloperCredential, error)
	ListCredentials(workspaceID, appID int64) ([]model.DeveloperCredential, error)
	GetCredential(workspaceID, credentialID int64) (model.DeveloperCredential, error)
	FindCredentialByExternalID(externalID string) (model.DeveloperCredential, model.DeveloperApplication, error)
	RevokeCredential(workspaceID, credentialID int64, now time.Time) error

	ConsumeRequest(externalID string, workspaceID int64, now time.Time) (model.DeveloperCredential, model.DeveloperApplication, error)
	RecordResponse(appID, workspaceID int64, statusCode int, latencyMS int64, now time.Time) error
	Usage(workspaceID, appID int64, from, to time.Time) ([]model.DeveloperUsageDaily, error)

	CreateWebhookTest(model.DeveloperWebhookTest) (model.DeveloperWebhookTest, error)
	ListWebhookTests(workspaceID, appID int64, limit int) ([]model.DeveloperWebhookTest, error)
}

type InMemoryDeveloperPlatformRepository struct {
	mu           sync.Mutex
	apps         map[int64]model.DeveloperApplication
	credentials  map[int64]model.DeveloperCredential
	usage        map[string]model.DeveloperUsageDaily
	webhookTests map[int64]model.DeveloperWebhookTest
	nextApp      int64
	nextCred     int64
	nextWebhook  int64
}

func NewInMemoryDeveloperPlatformRepository() *InMemoryDeveloperPlatformRepository {
	return &InMemoryDeveloperPlatformRepository{
		apps:         make(map[int64]model.DeveloperApplication),
		credentials:  make(map[int64]model.DeveloperCredential),
		usage:        make(map[string]model.DeveloperUsageDaily),
		webhookTests: make(map[int64]model.DeveloperWebhookTest),
		nextApp:      1, nextCred: 1, nextWebhook: 1,
	}
}

func (r *InMemoryDeveloperPlatformRepository) CreateApplication(item model.DeveloperApplication) (model.DeveloperApplication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextApp
	r.nextApp++
	item.AllowedScopes = append([]string(nil), item.AllowedScopes...)
	r.apps[item.ID] = item
	return cloneDeveloperApp(item), nil
}

func (r *InMemoryDeveloperPlatformRepository) ListApplications(workspaceID int64) ([]model.DeveloperApplication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.DeveloperApplication, 0)
	for _, item := range r.apps {
		if item.WorkspaceID == workspaceID {
			items = append(items, cloneDeveloperApp(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryDeveloperPlatformRepository) GetApplication(workspaceID, appID int64) (model.DeveloperApplication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.apps[appID]
	if !ok || item.WorkspaceID != workspaceID {
		return model.DeveloperApplication{}, ErrDeveloperAppNotFound
	}
	return cloneDeveloperApp(item), nil
}

func (r *InMemoryDeveloperPlatformRepository) UpdateApplicationLifecycle(workspaceID, appID int64, status string, reviewerID *int64, note string, submittedAt, reviewedAt *time.Time, now time.Time) (model.DeveloperApplication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.apps[appID]
	if !ok || item.WorkspaceID != workspaceID {
		return model.DeveloperApplication{}, ErrDeveloperAppNotFound
	}
	item.Status = status
	item.ReviewedByUserID = reviewerID
	item.ReviewNote = note
	item.SubmittedAt = submittedAt
	item.ReviewedAt = reviewedAt
	item.UpdatedAt = now
	r.apps[appID] = item
	return cloneDeveloperApp(item), nil
}

func (r *InMemoryDeveloperPlatformRepository) CreateCredential(item model.DeveloperCredential) (model.DeveloperCredential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	app, ok := r.apps[item.AppID]
	if !ok || app.WorkspaceID != item.WorkspaceID {
		return model.DeveloperCredential{}, ErrDeveloperAppNotFound
	}
	for _, existing := range r.credentials {
		if existing.ExternalID == item.ExternalID {
			return model.DeveloperCredential{}, errors.New("developer credential already exists")
		}
	}
	item.ID = r.nextCred
	r.nextCred++
	item.Scopes = append([]string(nil), item.Scopes...)
	item.RedirectURIs = append([]string(nil), item.RedirectURIs...)
	r.credentials[item.ID] = item
	return cloneDeveloperCredential(item), nil
}

func (r *InMemoryDeveloperPlatformRepository) ListCredentials(workspaceID, appID int64) ([]model.DeveloperCredential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if app, ok := r.apps[appID]; !ok || app.WorkspaceID != workspaceID {
		return nil, ErrDeveloperAppNotFound
	}
	items := make([]model.DeveloperCredential, 0)
	for _, item := range r.credentials {
		if item.WorkspaceID == workspaceID && item.AppID == appID {
			items = append(items, cloneDeveloperCredential(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryDeveloperPlatformRepository) GetCredential(workspaceID, credentialID int64) (model.DeveloperCredential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.credentials[credentialID]
	if !ok || item.WorkspaceID != workspaceID {
		return model.DeveloperCredential{}, ErrDeveloperCredentialNotFound
	}
	return cloneDeveloperCredential(item), nil
}

func (r *InMemoryDeveloperPlatformRepository) FindCredentialByExternalID(externalID string) (model.DeveloperCredential, model.DeveloperApplication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.credentials {
		if item.ExternalID != externalID || item.Status != model.DeveloperCredentialActive {
			continue
		}
		app, ok := r.apps[item.AppID]
		if !ok {
			break
		}
		return cloneDeveloperCredential(item), cloneDeveloperApp(app), nil
	}
	return model.DeveloperCredential{}, model.DeveloperApplication{}, ErrDeveloperCredentialNotFound
}

func (r *InMemoryDeveloperPlatformRepository) RevokeCredential(workspaceID, credentialID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.credentials[credentialID]
	if !ok || item.WorkspaceID != workspaceID {
		return ErrDeveloperCredentialNotFound
	}
	item.Status = model.DeveloperCredentialRevoked
	item.RevokedAt = &now
	r.credentials[credentialID] = item
	return nil
}

func (r *InMemoryDeveloperPlatformRepository) ConsumeRequest(externalID string, workspaceID int64, now time.Time) (model.DeveloperCredential, model.DeveloperApplication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var credential model.DeveloperCredential
	found := false
	for _, item := range r.credentials {
		if item.ExternalID == externalID && item.WorkspaceID == workspaceID && item.Status == model.DeveloperCredentialActive {
			credential = item
			found = true
			break
		}
	}
	if !found {
		return model.DeveloperCredential{}, model.DeveloperApplication{}, ErrDeveloperCredentialNotFound
	}
	app, ok := r.apps[credential.AppID]
	if !ok || app.WorkspaceID != workspaceID {
		return model.DeveloperCredential{}, model.DeveloperApplication{}, ErrDeveloperAppNotFound
	}
	day := now.UTC().Format("2006-01-02")
	monthPrefix := now.UTC().Format("2006-01-")
	daily := int64(0)
	monthly := int64(0)
	for key, usage := range r.usage {
		if usage.AppID != app.ID {
			continue
		}
		if strings.HasSuffix(key, "|"+day) {
			daily = usage.Requests
		}
		if strings.HasPrefix(usage.Date, monthPrefix) {
			monthly += usage.Requests
		}
	}
	if daily >= app.DailyRequestLimit || monthly >= app.MonthlyRequestLimit {
		return model.DeveloperCredential{}, model.DeveloperApplication{}, ErrDeveloperQuotaExceeded
	}
	key := developerUsageKey(app.ID, day)
	usage := r.usage[key]
	usage.AppID = app.ID
	usage.WorkspaceID = workspaceID
	usage.Date = day
	usage.Requests++
	usage.LastRequestAt = now.UTC()
	r.usage[key] = usage
	return cloneDeveloperCredential(credential), cloneDeveloperApp(app), nil
}

func (r *InMemoryDeveloperPlatformRepository) RecordResponse(appID, workspaceID int64, statusCode int, latencyMS int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	day := now.UTC().Format("2006-01-02")
	key := developerUsageKey(appID, day)
	usage := r.usage[key]
	usage.AppID = appID
	usage.WorkspaceID = workspaceID
	usage.Date = day
	if statusCode >= 400 {
		usage.Errors++
	}
	if latencyMS > 0 {
		usage.TotalLatencyMS += latencyMS
	}
	if usage.LastRequestAt.IsZero() || now.After(usage.LastRequestAt) {
		usage.LastRequestAt = now.UTC()
	}
	r.usage[key] = usage
	return nil
}

func (r *InMemoryDeveloperPlatformRepository) Usage(workspaceID, appID int64, from, to time.Time) ([]model.DeveloperUsageDaily, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if app, ok := r.apps[appID]; !ok || app.WorkspaceID != workspaceID {
		return nil, ErrDeveloperAppNotFound
	}
	items := make([]model.DeveloperUsageDaily, 0)
	for _, item := range r.usage {
		if item.AppID != appID || item.WorkspaceID != workspaceID {
			continue
		}
		day, err := time.Parse("2006-01-02", item.Date)
		if err != nil || day.Before(from) || day.After(to) {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Date < items[j].Date })
	return items, nil
}

func (r *InMemoryDeveloperPlatformRepository) CreateWebhookTest(item model.DeveloperWebhookTest) (model.DeveloperWebhookTest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	app, ok := r.apps[item.AppID]
	if !ok || app.WorkspaceID != item.WorkspaceID {
		return model.DeveloperWebhookTest{}, ErrDeveloperAppNotFound
	}
	item.ID = r.nextWebhook
	r.nextWebhook++
	item.Payload = cloneDeveloperMap(item.Payload)
	r.webhookTests[item.ID] = item
	return cloneDeveloperWebhookTest(item), nil
}

func (r *InMemoryDeveloperPlatformRepository) ListWebhookTests(workspaceID, appID int64, limit int) ([]model.DeveloperWebhookTest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if app, ok := r.apps[appID]; !ok || app.WorkspaceID != workspaceID {
		return nil, ErrDeveloperAppNotFound
	}
	items := make([]model.DeveloperWebhookTest, 0)
	for _, item := range r.webhookTests {
		if item.WorkspaceID == workspaceID && item.AppID == appID {
			items = append(items, cloneDeveloperWebhookTest(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func cloneDeveloperApp(item model.DeveloperApplication) model.DeveloperApplication {
	item.AllowedScopes = append([]string(nil), item.AllowedScopes...)
	return item
}

func cloneDeveloperCredential(item model.DeveloperCredential) model.DeveloperCredential {
	item.Scopes = append([]string(nil), item.Scopes...)
	item.RedirectURIs = append([]string(nil), item.RedirectURIs...)
	return item
}

func cloneDeveloperWebhookTest(item model.DeveloperWebhookTest) model.DeveloperWebhookTest {
	item.Payload = cloneDeveloperMap(item.Payload)
	return item
}

func cloneDeveloperMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func developerUsageKey(appID int64, day string) string {
	return formatEventFabricInt(appID) + "|" + day
}
