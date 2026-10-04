package repository

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrExtensionPublisherNotFound      = errors.New("extension publisher not found")
	ErrMarketplaceApplicationNotFound  = errors.New("marketplace application not found")
	ErrExtensionInstallationNotFound   = errors.New("extension installation not found")
	ErrExtensionSubscriptionNotFound   = errors.New("extension event subscription not found")
	ErrExtensionQuotaExceeded          = errors.New("extension installation quota exceeded")
	ErrExtensionInstallationExists     = errors.New("extension application is already installed for this workspace")
)

type ExtensionRepository interface {
	CreatePublisher(item model.ExtensionPublisher) (model.ExtensionPublisher, error)
	ListPublishers(workspaceID int64) ([]model.ExtensionPublisher, error)
	GetPublisher(workspaceID, publisherID int64) (model.ExtensionPublisher, error)
	UpdatePublisherLifecycle(workspaceID, publisherID int64, status string, reviewerID *int64, note string, submittedAt, reviewedAt *time.Time, now time.Time) (model.ExtensionPublisher, error)

	CreateApplication(item model.MarketplaceApplication) (model.MarketplaceApplication, error)
	ListWorkspaceApplications(workspaceID int64) ([]model.MarketplaceApplication, error)
	GetWorkspaceApplication(workspaceID, appID int64) (model.MarketplaceApplication, error)
	GetApplication(appID int64) (model.MarketplaceApplication, error)
	ListMarketplaceApplications(category string) ([]model.MarketplaceApplication, error)
	UpdateApplicationLifecycle(workspaceID, appID int64, status string, reviewerID *int64, note string, submittedAt, reviewedAt *time.Time, now time.Time) (model.MarketplaceApplication, error)

	CreateInstallation(item model.ExtensionInstallation, secretHash string) (model.ExtensionInstallation, error)
	ListInstallations(organizationID int64) ([]model.ExtensionInstallation, error)
	GetInstallation(organizationID, installationID int64) (model.ExtensionInstallation, error)
	FindInstallationBySecretHash(secretHash string, now time.Time) (model.ExtensionInstallation, error)
	RotateInstallationSecret(organizationID, installationID int64, prefix, secretHash string, now time.Time) (model.ExtensionInstallation, error)
	Uninstall(organizationID, installationID, actorUserID int64, now time.Time) (model.ExtensionInstallation, error)

	CreateSubscription(item model.ExtensionEventSubscription) (model.ExtensionEventSubscription, error)
	ListSubscriptions(organizationID, installationID int64) ([]model.ExtensionEventSubscription, error)
	GetSubscription(organizationID, installationID, subscriptionID int64) (model.ExtensionEventSubscription, error)
	DeleteSubscription(organizationID, installationID, subscriptionID int64) error

	ConsumeRequest(installationID, workspaceID int64, now time.Time) (model.ExtensionInstallation, error)
	RecordResponse(installationID, organizationID, workspaceID int64, statusCode int, latencyMS int64, now time.Time) error
	Usage(organizationID, installationID int64, from, to time.Time) ([]model.ExtensionUsageDaily, error)

	RecordDomainEvent(event model.DomainEvent) error
}

type InMemoryExtensionRepository struct {
	mu sync.Mutex

	publishers    map[int64]model.ExtensionPublisher
	applications  map[int64]model.MarketplaceApplication
	installations map[int64]model.ExtensionInstallation
	secretHashes  map[string]int64
	subscriptions map[int64]model.ExtensionEventSubscription
	usage         map[string]model.ExtensionUsageDaily
	events        []model.DomainEvent

	nextPublisher    int64
	nextApplication  int64
	nextInstallation int64
	nextSubscription int64
	nextEvent        int64
}

func NewInMemoryExtensionRepository() *InMemoryExtensionRepository {
	return &InMemoryExtensionRepository{
		publishers:       map[int64]model.ExtensionPublisher{},
		applications:     map[int64]model.MarketplaceApplication{},
		installations:    map[int64]model.ExtensionInstallation{},
		secretHashes:     map[string]int64{},
		subscriptions:    map[int64]model.ExtensionEventSubscription{},
		usage:            map[string]model.ExtensionUsageDaily{},
		nextPublisher:    1,
		nextApplication:  1,
		nextInstallation: 1,
		nextSubscription: 1,
		nextEvent:        1,
	}
}

func (r *InMemoryExtensionRepository) CreatePublisher(item model.ExtensionPublisher) (model.ExtensionPublisher, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.publishers {
		if strings.EqualFold(existing.Slug, item.Slug) {
			return model.ExtensionPublisher{}, errors.New("extension publisher slug already exists")
		}
	}
	item.ID = r.nextPublisher
	r.nextPublisher++
	r.publishers[item.ID] = item
	return item, nil
}

func (r *InMemoryExtensionRepository) ListPublishers(workspaceID int64) ([]model.ExtensionPublisher, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.ExtensionPublisher{}
	for _, item := range r.publishers {
		if item.WorkspaceID == workspaceID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryExtensionRepository) GetPublisher(workspaceID, publisherID int64) (model.ExtensionPublisher, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.publishers[publisherID]
	if !ok || item.WorkspaceID != workspaceID {
		return model.ExtensionPublisher{}, ErrExtensionPublisherNotFound
	}
	return item, nil
}

func (r *InMemoryExtensionRepository) UpdatePublisherLifecycle(workspaceID, publisherID int64, status string, reviewerID *int64, note string, submittedAt, reviewedAt *time.Time, now time.Time) (model.ExtensionPublisher, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.publishers[publisherID]
	if !ok || item.WorkspaceID != workspaceID {
		return model.ExtensionPublisher{}, ErrExtensionPublisherNotFound
	}
	item.Status = status
	item.ReviewedByUserID = reviewerID
	item.ReviewNote = note
	item.SubmittedAt = submittedAt
	item.ReviewedAt = reviewedAt
	item.UpdatedAt = now
	r.publishers[publisherID] = item
	return item, nil
}

func (r *InMemoryExtensionRepository) CreateApplication(item model.MarketplaceApplication) (model.MarketplaceApplication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	publisher, ok := r.publishers[item.PublisherID]
	if !ok || publisher.WorkspaceID != item.PublisherWorkspaceID {
		return model.MarketplaceApplication{}, ErrExtensionPublisherNotFound
	}
	for _, existing := range r.applications {
		if strings.EqualFold(existing.Slug, item.Slug) {
			return model.MarketplaceApplication{}, errors.New("marketplace application slug already exists")
		}
	}
	item.ID = r.nextApplication
	r.nextApplication++
	item = cloneMarketplaceApplication(item)
	r.applications[item.ID] = item
	return cloneMarketplaceApplication(item), nil
}

func (r *InMemoryExtensionRepository) ListWorkspaceApplications(workspaceID int64) ([]model.MarketplaceApplication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.MarketplaceApplication{}
	for _, item := range r.applications {
		if item.PublisherWorkspaceID == workspaceID {
			items = append(items, cloneMarketplaceApplication(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryExtensionRepository) GetWorkspaceApplication(workspaceID, appID int64) (model.MarketplaceApplication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.applications[appID]
	if !ok || item.PublisherWorkspaceID != workspaceID {
		return model.MarketplaceApplication{}, ErrMarketplaceApplicationNotFound
	}
	return cloneMarketplaceApplication(item), nil
}

func (r *InMemoryExtensionRepository) GetApplication(appID int64) (model.MarketplaceApplication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.applications[appID]
	if !ok {
		return model.MarketplaceApplication{}, ErrMarketplaceApplicationNotFound
	}
	return cloneMarketplaceApplication(item), nil
}

func (r *InMemoryExtensionRepository) ListMarketplaceApplications(category string) ([]model.MarketplaceApplication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	category = strings.ToLower(strings.TrimSpace(category))
	items := []model.MarketplaceApplication{}
	for _, item := range r.applications {
		if item.Status != model.MarketplaceAppApproved {
			continue
		}
		if category != "" && !extensionStringContains(item.Categories, category) {
			continue
		}
		items = append(items, cloneMarketplaceApplication(item))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].ID < items[j].ID
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items, nil
}

func (r *InMemoryExtensionRepository) UpdateApplicationLifecycle(workspaceID, appID int64, status string, reviewerID *int64, note string, submittedAt, reviewedAt *time.Time, now time.Time) (model.MarketplaceApplication, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.applications[appID]
	if !ok || item.PublisherWorkspaceID != workspaceID {
		return model.MarketplaceApplication{}, ErrMarketplaceApplicationNotFound
	}
	item.Status = status
	item.ReviewedByUserID = reviewerID
	item.ReviewNote = note
	item.SubmittedAt = submittedAt
	item.ReviewedAt = reviewedAt
	item.UpdatedAt = now
	r.applications[appID] = cloneMarketplaceApplication(item)
	return cloneMarketplaceApplication(item), nil
}

func (r *InMemoryExtensionRepository) CreateInstallation(item model.ExtensionInstallation, secretHash string) (model.ExtensionInstallation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.installations {
		if existing.OrganizationID == item.OrganizationID && existing.ApplicationID == item.ApplicationID &&
			existing.WorkspaceID == item.WorkspaceID && existing.Status == model.ExtensionInstallationActive {
			return model.ExtensionInstallation{}, ErrExtensionInstallationExists
		}
	}
	if _, exists := r.secretHashes[secretHash]; exists {
		return model.ExtensionInstallation{}, errors.New("extension installation secret already exists")
	}
	item.ID = r.nextInstallation
	r.nextInstallation++
	item = cloneExtensionInstallation(item)
	r.installations[item.ID] = item
	r.secretHashes[secretHash] = item.ID
	return cloneExtensionInstallation(item), nil
}

func (r *InMemoryExtensionRepository) ListInstallations(organizationID int64) ([]model.ExtensionInstallation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.ExtensionInstallation{}
	for _, item := range r.installations {
		if item.OrganizationID == organizationID {
			items = append(items, cloneExtensionInstallation(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryExtensionRepository) GetInstallation(organizationID, installationID int64) (model.ExtensionInstallation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.installations[installationID]
	if !ok || item.OrganizationID != organizationID {
		return model.ExtensionInstallation{}, ErrExtensionInstallationNotFound
	}
	return cloneExtensionInstallation(item), nil
}

func (r *InMemoryExtensionRepository) FindInstallationBySecretHash(secretHash string, now time.Time) (model.ExtensionInstallation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.secretHashes[secretHash]
	if !ok {
		return model.ExtensionInstallation{}, ErrExtensionInstallationNotFound
	}
	item, ok := r.installations[id]
	if !ok || item.Status != model.ExtensionInstallationActive {
		return model.ExtensionInstallation{}, ErrExtensionInstallationNotFound
	}
	return cloneExtensionInstallation(item), nil
}

func (r *InMemoryExtensionRepository) RotateInstallationSecret(organizationID, installationID int64, prefix, secretHash string, now time.Time) (model.ExtensionInstallation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.installations[installationID]
	if !ok || item.OrganizationID != organizationID || item.Status != model.ExtensionInstallationActive {
		return model.ExtensionInstallation{}, ErrExtensionInstallationNotFound
	}
	for hash, id := range r.secretHashes {
		if id == installationID {
			delete(r.secretHashes, hash)
		}
	}
	item.InstallSecretPrefix = prefix
	item.UpdatedAt = now
	r.installations[installationID] = item
	r.secretHashes[secretHash] = installationID
	return cloneExtensionInstallation(item), nil
}

func (r *InMemoryExtensionRepository) Uninstall(organizationID, installationID, actorUserID int64, now time.Time) (model.ExtensionInstallation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.installations[installationID]
	if !ok || item.OrganizationID != organizationID {
		return model.ExtensionInstallation{}, ErrExtensionInstallationNotFound
	}
	if item.Status == model.ExtensionInstallationUninstalled {
		return cloneExtensionInstallation(item), nil
	}
	item.Status = model.ExtensionInstallationUninstalled
	item.UninstalledByUserID = &actorUserID
	item.UninstalledAt = &now
	item.UpdatedAt = now
	r.installations[installationID] = item
	for hash, id := range r.secretHashes {
		if id == installationID {
			delete(r.secretHashes, hash)
		}
	}
	return cloneExtensionInstallation(item), nil
}

func (r *InMemoryExtensionRepository) CreateSubscription(item model.ExtensionEventSubscription) (model.ExtensionEventSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	installation, ok := r.installations[item.InstallationID]
	if !ok || installation.OrganizationID != item.OrganizationID || installation.Status != model.ExtensionInstallationActive {
		return model.ExtensionEventSubscription{}, ErrExtensionInstallationNotFound
	}
	item.ID = r.nextSubscription
	r.nextSubscription++
	item.EventTypes = append([]string(nil), item.EventTypes...)
	r.subscriptions[item.ID] = item
	return cloneExtensionSubscription(item), nil
}

func (r *InMemoryExtensionRepository) ListSubscriptions(organizationID, installationID int64) ([]model.ExtensionEventSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.ExtensionEventSubscription{}
	for _, item := range r.subscriptions {
		if item.OrganizationID == organizationID && item.InstallationID == installationID {
			items = append(items, cloneExtensionSubscription(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryExtensionRepository) GetSubscription(organizationID, installationID, subscriptionID int64) (model.ExtensionEventSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.subscriptions[subscriptionID]
	if !ok || item.OrganizationID != organizationID || item.InstallationID != installationID {
		return model.ExtensionEventSubscription{}, ErrExtensionSubscriptionNotFound
	}
	return cloneExtensionSubscription(item), nil
}

func (r *InMemoryExtensionRepository) DeleteSubscription(organizationID, installationID, subscriptionID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.subscriptions[subscriptionID]
	if !ok || item.OrganizationID != organizationID || item.InstallationID != installationID {
		return ErrExtensionSubscriptionNotFound
	}
	delete(r.subscriptions, subscriptionID)
	return nil
}

func (r *InMemoryExtensionRepository) ConsumeRequest(installationID, workspaceID int64, now time.Time) (model.ExtensionInstallation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.installations[installationID]
	if !ok || item.WorkspaceID != workspaceID || item.Status != model.ExtensionInstallationActive {
		return model.ExtensionInstallation{}, ErrExtensionInstallationNotFound
	}
	day := now.UTC().Format("2006-01-02")
	monthPrefix := now.UTC().Format("2006-01-")
	var daily, monthly int64
	for _, usage := range r.usage {
		if usage.InstallationID != installationID {
			continue
		}
		if usage.Date == day {
			daily = usage.Requests
		}
		if strings.HasPrefix(usage.Date, monthPrefix) {
			monthly += usage.Requests
		}
	}
	if daily >= item.DailyRequestLimit || monthly >= item.MonthlyRequestLimit {
		return model.ExtensionInstallation{}, ErrExtensionQuotaExceeded
	}
	key := extensionUsageKey(installationID, day)
	usage := r.usage[key]
	usage.InstallationID = installationID
	usage.OrganizationID = item.OrganizationID
	usage.WorkspaceID = workspaceID
	usage.Date = day
	usage.Requests++
	usage.LastRequestAt = now.UTC()
	r.usage[key] = usage
	return cloneExtensionInstallation(item), nil
}

func (r *InMemoryExtensionRepository) RecordResponse(installationID, organizationID, workspaceID int64, statusCode int, latencyMS int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	day := now.UTC().Format("2006-01-02")
	key := extensionUsageKey(installationID, day)
	usage := r.usage[key]
	usage.InstallationID = installationID
	usage.OrganizationID = organizationID
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

func (r *InMemoryExtensionRepository) Usage(organizationID, installationID int64, from, to time.Time) ([]model.ExtensionUsageDaily, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	installation, ok := r.installations[installationID]
	if !ok || installation.OrganizationID != organizationID {
		return nil, ErrExtensionInstallationNotFound
	}
	items := []model.ExtensionUsageDaily{}
	for _, item := range r.usage {
		if item.OrganizationID != organizationID || item.InstallationID != installationID {
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

func (r *InMemoryExtensionRepository) RecordDomainEvent(event model.DomainEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event.ID = r.nextEvent
	r.nextEvent++
	r.events = append(r.events, event)
	return nil
}

func cloneMarketplaceApplication(item model.MarketplaceApplication) model.MarketplaceApplication {
	item.Categories = append([]string(nil), item.Categories...)
	item.RequestedScopes = append([]string(nil), item.RequestedScopes...)
	item.EventTypes = append([]string(nil), item.EventTypes...)
	item.ConfigSchema = append([]model.ExtensionConfigField(nil), item.ConfigSchema...)
	item.Packs = append([]model.ExtensionPack(nil), item.Packs...)
	for i := range item.Packs {
		item.Packs[i].Definition = cloneExtensionMap(item.Packs[i].Definition)
	}
	return item
}

func cloneExtensionInstallation(item model.ExtensionInstallation) model.ExtensionInstallation {
	item.GrantedScopes = append([]string(nil), item.GrantedScopes...)
	item.Config = cloneExtensionStringMap(item.Config)
	item.SecretRefs = cloneExtensionStringMap(item.SecretRefs)
	return item
}

func cloneExtensionSubscription(item model.ExtensionEventSubscription) model.ExtensionEventSubscription {
	item.EventTypes = append([]string(nil), item.EventTypes...)
	return item
}

func cloneExtensionMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	raw, _ := json.Marshal(value)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func cloneExtensionStringMap(value map[string]string) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(value))
	for k, v := range value {
		out[k] = v
	}
	return out
}

func extensionUsageKey(installationID int64, day string) string {
	return formatEventFabricInt(installationID) + "|" + day
}

func extensionStringContains(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
