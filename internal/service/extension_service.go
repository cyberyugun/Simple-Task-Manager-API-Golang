package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/events"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrExtensionWorkspaceForbidden   = errors.New("marketplace publishing requires workspace owner or admin")
	ErrExtensionOrganizationForbidden = errors.New("extension installation requires organization owner or admin")
	ErrInvalidExtensionPublisher     = errors.New("invalid extension publisher")
	ErrInvalidExtensionPublisherFlow = errors.New("invalid extension publisher lifecycle transition")
	ErrInvalidMarketplaceApplication = errors.New("invalid marketplace application manifest")
	ErrInvalidMarketplaceReview      = errors.New("invalid marketplace application lifecycle transition")
	ErrMarketplacePublisherUnverified = errors.New("marketplace publisher must be verified")
	ErrMarketplaceApplicationApproval = errors.New("marketplace application must be approved")
	ErrInvalidExtensionInstallation  = errors.New("invalid extension installation")
	ErrInvalidExtensionConfiguration = errors.New("invalid extension configuration")
	ErrInvalidExtensionSecret        = errors.New("invalid extension installation secret")
	ErrInvalidExtensionSubscription  = errors.New("invalid extension event subscription")
)

type ExtensionService struct {
	repo          repository.ExtensionRepository
	workspaces    repository.WorkspaceRepository
	orgs          repository.OrganizationRepository
	events        repository.EventRepository
	tokens        *auth.TokenManager
	allowInsecure bool
}

func NewExtensionService(
	repo repository.ExtensionRepository,
	workspaces repository.WorkspaceRepository,
	orgs repository.OrganizationRepository,
	eventRepo repository.EventRepository,
	tokens *auth.TokenManager,
	allowInsecure bool,
) *ExtensionService {
	return &ExtensionService{
		repo: repo, workspaces: workspaces, orgs: orgs, events: eventRepo,
		tokens: tokens, allowInsecure: allowInsecure,
	}
}

func (s *ExtensionService) MarketplaceApps(category string) ([]model.MarketplaceListing, error) {
	apps, err := s.repo.ListMarketplaceApplications(extensionNormalizeSlug(category))
	if err != nil {
		return nil, err
	}
	items := make([]model.MarketplaceListing, 0, len(apps))
	for _, app := range apps {
		publisher, err := s.repo.GetPublisher(app.PublisherWorkspaceID, app.PublisherID)
		if err != nil {
			return nil, err
		}
		if publisher.Status != model.ExtensionPublisherVerified {
			continue
		}
		items = append(items, model.MarketplaceListing{Application: app, Publisher: publisher})
	}
	return items, nil
}

func (s *ExtensionService) MarketplaceApp(appID int64) (model.MarketplaceListing, error) {
	app, err := s.repo.GetApplication(appID)
	if err != nil {
		return model.MarketplaceListing{}, err
	}
	if app.Status != model.MarketplaceAppApproved {
		return model.MarketplaceListing{}, repository.ErrMarketplaceApplicationNotFound
	}
	publisher, err := s.repo.GetPublisher(app.PublisherWorkspaceID, app.PublisherID)
	if err != nil {
		return model.MarketplaceListing{}, err
	}
	if publisher.Status != model.ExtensionPublisherVerified {
		return model.MarketplaceListing{}, repository.ErrMarketplaceApplicationNotFound
	}
	return model.MarketplaceListing{Application: app, Publisher: publisher}, nil
}

func (s *ExtensionService) Publishers(actorUserID, workspaceID int64) ([]model.ExtensionPublisher, error) {
	if err := s.requireWorkspaceAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListPublishers(workspaceID)
}

func (s *ExtensionService) CreatePublisher(actorUserID, workspaceID int64, req model.CreateExtensionPublisherRequest) (model.ExtensionPublisher, error) {
	if err := s.requireWorkspaceAdmin(actorUserID, workspaceID); err != nil {
		return model.ExtensionPublisher{}, err
	}
	name := strings.TrimSpace(req.Name)
	slug := extensionNormalizeSlug(req.Slug)
	website := strings.TrimSpace(req.WebsiteURL)
	if name == "" || len(name) > 180 || !extensionValidSlug(slug) || (website != "" && !extensionValidMetadataURL(website)) {
		return model.ExtensionPublisher{}, ErrInvalidExtensionPublisher
	}
	now := time.Now().UTC()
	item, err := s.repo.CreatePublisher(model.ExtensionPublisher{
		WorkspaceID: workspaceID, Name: name, Slug: slug, WebsiteURL: website,
		Status: model.ExtensionPublisherDraft, CreatedByUserID: actorUserID,
		CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		_ = s.auditWorkspace(workspaceID, actorUserID, "marketplace.publisher.created", "extension_publisher", strconv.FormatInt(item.ID, 10), map[string]any{"slug": slug}, now)
	}
	return item, err
}

func (s *ExtensionService) SubmitPublisher(actorUserID, workspaceID, publisherID int64) (model.ExtensionPublisher, error) {
	if err := s.requireWorkspaceAdmin(actorUserID, workspaceID); err != nil {
		return model.ExtensionPublisher{}, err
	}
	item, err := s.repo.GetPublisher(workspaceID, publisherID)
	if err != nil {
		return model.ExtensionPublisher{}, err
	}
	if item.Status != model.ExtensionPublisherDraft && item.Status != model.ExtensionPublisherRejected {
		return model.ExtensionPublisher{}, ErrInvalidExtensionPublisherFlow
	}
	now := time.Now().UTC()
	item, err = s.repo.UpdatePublisherLifecycle(workspaceID, publisherID, model.ExtensionPublisherSubmitted, nil, "", &now, nil, now)
	if err == nil {
		_ = s.auditWorkspace(workspaceID, actorUserID, "marketplace.publisher.submitted", "extension_publisher", strconv.FormatInt(publisherID, 10), nil, now)
	}
	return item, err
}

func (s *ExtensionService) ReviewPublisher(actorUserID, workspaceID, publisherID int64, req model.ReviewExtensionPublisherRequest) (model.ExtensionPublisher, error) {
	if err := s.requireWorkspaceAdmin(actorUserID, workspaceID); err != nil {
		return model.ExtensionPublisher{}, err
	}
	item, err := s.repo.GetPublisher(workspaceID, publisherID)
	if err != nil {
		return model.ExtensionPublisher{}, err
	}
	if item.Status != model.ExtensionPublisherSubmitted {
		return model.ExtensionPublisher{}, ErrInvalidExtensionPublisherFlow
	}
	decision := strings.ToLower(strings.TrimSpace(req.Decision))
	status := ""
	switch decision {
	case "verify", "approve":
		status = model.ExtensionPublisherVerified
	case "reject":
		status = model.ExtensionPublisherRejected
	default:
		return model.ExtensionPublisher{}, ErrInvalidExtensionPublisherFlow
	}
	now := time.Now().UTC()
	reviewer := actorUserID
	item, err = s.repo.UpdatePublisherLifecycle(workspaceID, publisherID, status, &reviewer, strings.TrimSpace(req.Note), item.SubmittedAt, &now, now)
	if err == nil {
		_ = s.auditWorkspace(workspaceID, actorUserID, "marketplace.publisher.reviewed", "extension_publisher", strconv.FormatInt(publisherID, 10), map[string]any{"status": status}, now)
	}
	return item, err
}

func (s *ExtensionService) WorkspaceApps(actorUserID, workspaceID int64) ([]model.MarketplaceApplication, error) {
	if err := s.requireWorkspaceAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListWorkspaceApplications(workspaceID)
}

func (s *ExtensionService) CreateApplication(actorUserID, workspaceID int64, req model.CreateMarketplaceApplicationRequest) (model.MarketplaceApplication, error) {
	if err := s.requireWorkspaceAdmin(actorUserID, workspaceID); err != nil {
		return model.MarketplaceApplication{}, err
	}
	publisher, err := s.repo.GetPublisher(workspaceID, req.PublisherID)
	if err != nil {
		return model.MarketplaceApplication{}, err
	}
	app, err := extensionNormalizeApplicationManifest(workspaceID, actorUserID, req)
	if err != nil {
		return model.MarketplaceApplication{}, err
	}
	app.PublisherID = publisher.ID
	now := time.Now().UTC()
	app.CreatedAt, app.UpdatedAt = now, now
	item, err := s.repo.CreateApplication(app)
	if err == nil {
		_ = s.auditWorkspace(workspaceID, actorUserID, "marketplace.app.created", "marketplace_application", strconv.FormatInt(item.ID, 10), map[string]any{
			"slug": item.Slug, "publisher_id": item.PublisherID, "scopes": item.RequestedScopes,
		}, now)
	}
	return item, err
}

func (s *ExtensionService) SubmitApplication(actorUserID, workspaceID, appID int64) (model.MarketplaceApplication, error) {
	if err := s.requireWorkspaceAdmin(actorUserID, workspaceID); err != nil {
		return model.MarketplaceApplication{}, err
	}
	item, err := s.repo.GetWorkspaceApplication(workspaceID, appID)
	if err != nil {
		return model.MarketplaceApplication{}, err
	}
	publisher, err := s.repo.GetPublisher(workspaceID, item.PublisherID)
	if err != nil {
		return model.MarketplaceApplication{}, err
	}
	if publisher.Status != model.ExtensionPublisherVerified {
		return model.MarketplaceApplication{}, ErrMarketplacePublisherUnverified
	}
	if item.Status != model.MarketplaceAppDraft && item.Status != model.MarketplaceAppRejected {
		return model.MarketplaceApplication{}, ErrInvalidMarketplaceReview
	}
	now := time.Now().UTC()
	item, err = s.repo.UpdateApplicationLifecycle(workspaceID, appID, model.MarketplaceAppSubmitted, nil, "", &now, nil, now)
	if err == nil {
		_ = s.auditWorkspace(workspaceID, actorUserID, "marketplace.app.submitted", "marketplace_application", strconv.FormatInt(appID, 10), nil, now)
	}
	return item, err
}

func (s *ExtensionService) ReviewApplication(actorUserID, workspaceID, appID int64, req model.ReviewMarketplaceApplicationRequest) (model.MarketplaceApplication, error) {
	if err := s.requireWorkspaceAdmin(actorUserID, workspaceID); err != nil {
		return model.MarketplaceApplication{}, err
	}
	item, err := s.repo.GetWorkspaceApplication(workspaceID, appID)
	if err != nil {
		return model.MarketplaceApplication{}, err
	}
	publisher, err := s.repo.GetPublisher(workspaceID, item.PublisherID)
	if err != nil {
		return model.MarketplaceApplication{}, err
	}
	decision := strings.ToLower(strings.TrimSpace(req.Decision))
	status := ""
	switch {
	case item.Status == model.MarketplaceAppSubmitted && decision == "approve":
		if publisher.Status != model.ExtensionPublisherVerified {
			return model.MarketplaceApplication{}, ErrMarketplacePublisherUnverified
		}
		status = model.MarketplaceAppApproved
	case item.Status == model.MarketplaceAppSubmitted && decision == "reject":
		status = model.MarketplaceAppRejected
	case item.Status == model.MarketplaceAppApproved && decision == "suspend":
		status = model.MarketplaceAppSuspended
	default:
		return model.MarketplaceApplication{}, ErrInvalidMarketplaceReview
	}
	now := time.Now().UTC()
	reviewer := actorUserID
	item, err = s.repo.UpdateApplicationLifecycle(workspaceID, appID, status, &reviewer, strings.TrimSpace(req.Note), item.SubmittedAt, &now, now)
	if err == nil {
		_ = s.auditWorkspace(workspaceID, actorUserID, "marketplace.app.reviewed", "marketplace_application", strconv.FormatInt(appID, 10), map[string]any{"status": status}, now)
	}
	return item, err
}

func (s *ExtensionService) Installations(actorUserID, organizationID int64) ([]model.ExtensionInstallation, error) {
	if _, err := s.requireOrganizationAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListInstallations(organizationID)
}

func (s *ExtensionService) Install(actorUserID, organizationID int64, req model.InstallExtensionRequest) (model.ExtensionInstallationSecret, error) {
	if _, err := s.requireOrganizationAdmin(actorUserID, organizationID); err != nil {
		return model.ExtensionInstallationSecret{}, err
	}
	if req.ApplicationID <= 0 || req.WorkspaceID <= 0 || !s.organizationHasWorkspace(organizationID, req.WorkspaceID) {
		return model.ExtensionInstallationSecret{}, ErrInvalidExtensionInstallation
	}
	app, err := s.repo.GetApplication(req.ApplicationID)
	if err != nil {
		return model.ExtensionInstallationSecret{}, err
	}
	if app.Status != model.MarketplaceAppApproved {
		return model.ExtensionInstallationSecret{}, ErrMarketplaceApplicationApproval
	}
	publisher, err := s.repo.GetPublisher(app.PublisherWorkspaceID, app.PublisherID)
	if err != nil {
		return model.ExtensionInstallationSecret{}, err
	}
	if publisher.Status != model.ExtensionPublisherVerified {
		return model.ExtensionInstallationSecret{}, ErrMarketplacePublisherUnverified
	}
	scopes, err := extensionRequestedScopes(req.GrantedScopes, app.RequestedScopes)
	if err != nil {
		return model.ExtensionInstallationSecret{}, err
	}
	config, secretRefs, err := extensionValidateInstallationConfig(app.ConfigSchema, req.Config, req.SecretRefs)
	if err != nil {
		return model.ExtensionInstallationSecret{}, err
	}
	secret, prefix, hash, err := extensionGenerateSecret()
	if err != nil {
		return model.ExtensionInstallationSecret{}, err
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateInstallation(model.ExtensionInstallation{
		OrganizationID: organizationID, ApplicationID: app.ID, WorkspaceID: req.WorkspaceID,
		Status: model.ExtensionInstallationActive, GrantedScopes: scopes, Config: config, SecretRefs: secretRefs,
		InstallSecretPrefix: prefix, DailyRequestLimit: app.DailyRequestLimit, MonthlyRequestLimit: app.MonthlyRequestLimit,
		InstalledByUserID: actorUserID, InstalledAt: now, UpdatedAt: now,
	}, hash)
	if err != nil {
		return model.ExtensionInstallationSecret{}, err
	}
	_ = s.auditOrganization(organizationID, actorUserID, "extension.installed", "extension_installation", strconv.FormatInt(item.ID, 10), map[string]any{
		"application_id": app.ID, "workspace_id": item.WorkspaceID, "scopes": item.GrantedScopes,
	}, now)
	_ = s.auditWorkspace(item.WorkspaceID, actorUserID, "extension.installed", "extension_installation", strconv.FormatInt(item.ID, 10), map[string]any{
		"organization_id": organizationID, "application_id": app.ID,
	}, now)
	_ = s.recordLifecycleEvent(item, "extension.installed", now)
	return model.ExtensionInstallationSecret{Installation: item, Secret: secret}, nil
}

func (s *ExtensionService) RotateSecret(actorUserID, organizationID, installationID int64) (model.ExtensionInstallationSecret, error) {
	if _, err := s.requireOrganizationAdmin(actorUserID, organizationID); err != nil {
		return model.ExtensionInstallationSecret{}, err
	}
	current, err := s.repo.GetInstallation(organizationID, installationID)
	if err != nil {
		return model.ExtensionInstallationSecret{}, err
	}
	if current.Status != model.ExtensionInstallationActive {
		return model.ExtensionInstallationSecret{}, ErrInvalidExtensionInstallation
	}
	secret, prefix, hash, err := extensionGenerateSecret()
	if err != nil {
		return model.ExtensionInstallationSecret{}, err
	}
	now := time.Now().UTC()
	item, err := s.repo.RotateInstallationSecret(organizationID, installationID, prefix, hash, now)
	if err != nil {
		return model.ExtensionInstallationSecret{}, err
	}
	_ = s.auditOrganization(organizationID, actorUserID, "extension.secret.rotated", "extension_installation", strconv.FormatInt(item.ID, 10), nil, now)
	return model.ExtensionInstallationSecret{Installation: item, Secret: secret}, nil
}

func (s *ExtensionService) Uninstall(actorUserID, organizationID, installationID int64) (model.ExtensionInstallation, error) {
	if _, err := s.requireOrganizationAdmin(actorUserID, organizationID); err != nil {
		return model.ExtensionInstallation{}, err
	}
	current, err := s.repo.GetInstallation(organizationID, installationID)
	if err != nil {
		return model.ExtensionInstallation{}, err
	}
	subscriptions, err := s.repo.ListSubscriptions(organizationID, installationID)
	if err != nil {
		return model.ExtensionInstallation{}, err
	}
	for _, subscription := range subscriptions {
		if err := s.repo.DeleteSubscription(organizationID, installationID, subscription.ID); err != nil &&
			!errors.Is(err, repository.ErrExtensionSubscriptionNotFound) {
			return model.ExtensionInstallation{}, err
		}
		if err := s.events.DeleteWebhook(current.WorkspaceID, subscription.WebhookSubscriptionID); err != nil &&
			!errors.Is(err, repository.ErrWebhookSubscriptionNotFound) {
			return model.ExtensionInstallation{}, err
		}
	}
	now := time.Now().UTC()
	item, err := s.repo.Uninstall(organizationID, installationID, actorUserID, now)
	if err != nil {
		return model.ExtensionInstallation{}, err
	}
	_ = s.auditOrganization(organizationID, actorUserID, "extension.uninstalled", "extension_installation", strconv.FormatInt(item.ID, 10), map[string]any{"application_id": item.ApplicationID}, now)
	_ = s.auditWorkspace(item.WorkspaceID, actorUserID, "extension.uninstalled", "extension_installation", strconv.FormatInt(item.ID, 10), map[string]any{"organization_id": organizationID}, now)
	_ = s.recordLifecycleEvent(item, "extension.uninstalled", now)
	return item, nil
}

func (s *ExtensionService) ExchangeToken(req model.ExtensionTokenRequest) (model.ExtensionTokenResult, error) {
	raw := strings.TrimSpace(req.Secret)
	if !strings.HasPrefix(raw, "stm_ext_") || len(raw) < 24 {
		return model.ExtensionTokenResult{}, ErrInvalidExtensionSecret
	}
	installation, err := s.repo.FindInstallationBySecretHash(auth.HashOpaqueToken(raw), time.Now().UTC())
	if err != nil {
		return model.ExtensionTokenResult{}, ErrInvalidExtensionSecret
	}
	if err := s.ensureRuntimeApproved(installation); err != nil {
		return model.ExtensionTokenResult{}, err
	}
	clientID := "extension:" + strconv.FormatInt(installation.ID, 10)
	token, err := s.tokens.GenerateService(clientID, installation.WorkspaceID, installation.GrantedScopes, installation.InstalledByUserID)
	if err != nil {
		return model.ExtensionTokenResult{}, err
	}
	return model.ExtensionTokenResult{
		AccessToken: token, TokenType: "Bearer", ExpiresIn: int64(s.tokens.TTL().Seconds()),
		Scopes: append([]string(nil), installation.GrantedScopes...), WorkspaceID: installation.WorkspaceID,
		InstallationID: installation.ID,
	}, nil
}

func (s *ExtensionService) Subscriptions(actorUserID, organizationID, installationID int64) ([]model.ExtensionEventSubscription, error) {
	if _, err := s.requireOrganizationAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetInstallation(organizationID, installationID); err != nil {
		return nil, err
	}
	return s.repo.ListSubscriptions(organizationID, installationID)
}

func (s *ExtensionService) CreateSubscription(actorUserID, organizationID, installationID int64, req model.CreateExtensionEventSubscriptionRequest) (model.ExtensionEventSubscriptionSecret, error) {
	if _, err := s.requireOrganizationAdmin(actorUserID, organizationID); err != nil {
		return model.ExtensionEventSubscriptionSecret{}, err
	}
	installation, err := s.repo.GetInstallation(organizationID, installationID)
	if err != nil {
		return model.ExtensionEventSubscriptionSecret{}, err
	}
	if installation.Status != model.ExtensionInstallationActive {
		return model.ExtensionEventSubscriptionSecret{}, ErrInvalidExtensionInstallation
	}
	app, err := s.repo.GetApplication(installation.ApplicationID)
	if err != nil {
		return model.ExtensionEventSubscriptionSecret{}, err
	}
	if app.Status != model.MarketplaceAppApproved {
		return model.ExtensionEventSubscriptionSecret{}, ErrMarketplaceApplicationApproval
	}
	eventTypes, err := extensionRequestedEvents(req.EventTypes, app.EventTypes)
	if err != nil {
		return model.ExtensionEventSubscriptionSecret{}, err
	}
	endpoint, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || events.ValidateWebhookURL(endpoint, s.allowInsecure) != nil {
		return model.ExtensionEventSubscriptionSecret{}, ErrInvalidExtensionSubscription
	}
	signingSecret, err := generateWebhookSecret()
	if err != nil {
		return model.ExtensionEventSubscriptionSecret{}, err
	}
	now := time.Now().UTC()
	webhook, err := s.events.CreateWebhook(installation.WorkspaceID, actorUserID, endpoint.String(), signingSecret, eventTypes, now)
	if err != nil {
		return model.ExtensionEventSubscriptionSecret{}, err
	}
	item, err := s.repo.CreateSubscription(model.ExtensionEventSubscription{
		InstallationID: installation.ID, OrganizationID: organizationID, WorkspaceID: installation.WorkspaceID,
		WebhookSubscriptionID: webhook.ID, URL: endpoint.String(), EventTypes: eventTypes,
		CreatedByUserID: actorUserID, CreatedAt: now,
	})
	if err != nil {
		_ = s.events.DeleteWebhook(installation.WorkspaceID, webhook.ID)
		return model.ExtensionEventSubscriptionSecret{}, err
	}
	_ = s.auditOrganization(organizationID, actorUserID, "extension.subscription.created", "extension_event_subscription", strconv.FormatInt(item.ID, 10), map[string]any{
		"installation_id": installation.ID, "event_types": eventTypes, "url": endpoint.String(),
	}, now)
	return model.ExtensionEventSubscriptionSecret{Subscription: item, SigningSecret: signingSecret}, nil
}

func (s *ExtensionService) DeleteSubscription(actorUserID, organizationID, installationID, subscriptionID int64) error {
	if _, err := s.requireOrganizationAdmin(actorUserID, organizationID); err != nil {
		return err
	}
	item, err := s.repo.GetSubscription(organizationID, installationID, subscriptionID)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteSubscription(organizationID, installationID, subscriptionID); err != nil {
		return err
	}
	if err := s.events.DeleteWebhook(item.WorkspaceID, item.WebhookSubscriptionID); err != nil &&
		!errors.Is(err, repository.ErrWebhookSubscriptionNotFound) {
		return err
	}
	_ = s.auditOrganization(organizationID, actorUserID, "extension.subscription.deleted", "extension_event_subscription", strconv.FormatInt(subscriptionID, 10), map[string]any{"installation_id": installationID}, time.Now().UTC())
	return nil
}

func (s *ExtensionService) Usage(actorUserID, organizationID, installationID int64, days int) (model.ExtensionUsageSummary, error) {
	if _, err := s.requireOrganizationAdmin(actorUserID, organizationID); err != nil {
		return model.ExtensionUsageSummary{}, err
	}
	if days == 0 {
		days = 30
	}
	if days < 1 || days > 366 {
		return model.ExtensionUsageSummary{}, ErrInvalidExtensionInstallation
	}
	installation, err := s.repo.GetInstallation(organizationID, installationID)
	if err != nil {
		return model.ExtensionUsageSummary{}, err
	}
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -days+1)
	items, err := s.repo.Usage(organizationID, installationID, from, now)
	if err != nil {
		return model.ExtensionUsageSummary{}, err
	}
	result := model.ExtensionUsageSummary{
		InstallationID: installationID, DailyRequestLimit: installation.DailyRequestLimit,
		MonthlyRequestLimit: installation.MonthlyRequestLimit, Daily: items,
	}
	today := now.Format("2006-01-02")
	monthPrefix := now.Format("2006-01-")
	var latency int64
	for _, item := range items {
		result.TotalRequests += item.Requests
		result.TotalErrors += item.Errors
		latency += item.TotalLatencyMS
		if item.Date == today {
			result.TodayRequests = item.Requests
		}
		if strings.HasPrefix(item.Date, monthPrefix) {
			result.MonthRequests += item.Requests
		}
	}
	if result.TotalRequests > 0 {
		result.AverageLatencyMS = float64(latency) / float64(result.TotalRequests)
	}
	return result, nil
}

func (s *ExtensionService) AuthorizeExtensionRequest(clientID string, workspaceID int64, now time.Time) model.ExtensionRequestDecision {
	decision := model.ExtensionRequestDecision{WorkspaceID: workspaceID}
	if !strings.HasPrefix(clientID, "extension:") {
		return decision
	}
	decision.IsExtension = true
	installationID, err := strconv.ParseInt(strings.TrimPrefix(clientID, "extension:"), 10, 64)
	if err != nil || installationID <= 0 || workspaceID <= 0 {
		decision.StatusCode = http.StatusForbidden
		decision.Message = "invalid extension identity"
		return decision
	}
	decision.InstallationID = installationID
	installation, err := s.repo.GetRuntimeInstallation(installationID, workspaceID)
	if err != nil {
		decision.StatusCode = http.StatusForbidden
		decision.Message = "extension installation is not active"
		return decision
	}
	if err := s.ensureRuntimeApproved(installation); err != nil {
		decision.StatusCode = http.StatusForbidden
		decision.Message = err.Error()
		return decision
	}
	if _, err := s.repo.ConsumeRequest(installationID, workspaceID, now); errors.Is(err, repository.ErrExtensionQuotaExceeded) {
		decision.StatusCode = http.StatusTooManyRequests
		decision.Message = err.Error()
		return decision
	} else if err != nil {
		decision.StatusCode = http.StatusForbidden
		decision.Message = "extension installation is not active"
		return decision
	}
	decision.Allowed = true
	return decision
}

func (s *ExtensionService) RecordExtensionResponse(installationID, workspaceID int64, statusCode int, latency time.Duration, now time.Time) error {
	installation, err := s.repo.GetRuntimeInstallation(installationID, workspaceID)
	if err != nil {
		return err
	}
	return s.repo.RecordResponse(installationID, installation.OrganizationID, workspaceID, statusCode, latency.Milliseconds(), now)
}

func (s *ExtensionService) ensureRuntimeApproved(installation model.ExtensionInstallation) error {
	if installation.Status != model.ExtensionInstallationActive {
		return ErrInvalidExtensionInstallation
	}
	app, err := s.repo.GetApplication(installation.ApplicationID)
	if err != nil {
		return err
	}
	if app.Status != model.MarketplaceAppApproved {
		return ErrMarketplaceApplicationApproval
	}
	publisher, err := s.repo.GetPublisher(app.PublisherWorkspaceID, app.PublisherID)
	if err != nil {
		return err
	}
	if publisher.Status != model.ExtensionPublisherVerified {
		return ErrMarketplacePublisherUnverified
	}
	return nil
}

func (s *ExtensionService) requireWorkspaceAdmin(userID, workspaceID int64) error {
	access, err := s.workspaces.ResolveAccess(userID, workspaceID, time.Now().UTC())
	if err != nil || (access.Role != model.WorkspaceRoleOwner && access.Role != model.WorkspaceRoleAdmin) {
		return ErrExtensionWorkspaceForbidden
	}
	return nil
}

func (s *ExtensionService) requireOrganizationAdmin(userID, organizationID int64) (model.Organization, error) {
	org, err := s.orgs.GetOrganization(organizationID)
	if err != nil {
		return model.Organization{}, err
	}
	member, err := s.orgs.GetMember(organizationID, userID)
	if err != nil || org.Status != model.OrganizationStatusActive {
		return model.Organization{}, ErrExtensionOrganizationForbidden
	}
	if member.Role != model.OrganizationRoleOwner && member.Role != model.OrganizationRoleAdmin &&
		member.Role != model.OrganizationRoleDelegatedAdmin {
		return model.Organization{}, ErrExtensionOrganizationForbidden
	}
	return org, nil
}

func (s *ExtensionService) organizationHasWorkspace(organizationID, workspaceID int64) bool {
	items, err := s.orgs.ListWorkspaces(organizationID)
	if err != nil {
		return false
	}
	for _, item := range items {
		if item.WorkspaceID == workspaceID {
			return true
		}
	}
	return false
}

func (s *ExtensionService) auditWorkspace(workspaceID, actorUserID int64, action, resourceType, resourceID string, metadata map[string]any, now time.Time) error {
	wid, uid := workspaceID, actorUserID
	return s.workspaces.RecordAudit(model.AuditEvent{
		WorkspaceID: &wid, ActorUserID: &uid, Action: action, ResourceType: resourceType,
		ResourceID: resourceID, Metadata: metadata, CreatedAt: now,
	})
}

func (s *ExtensionService) auditOrganization(organizationID, actorUserID int64, action, resourceType, resourceID string, metadata map[string]any, now time.Time) error {
	uid := actorUserID
	return s.orgs.RecordAudit(model.OrganizationAuditEvent{
		OrganizationID: organizationID, ActorUserID: &uid, Action: action, ResourceType: resourceType,
		ResourceID: resourceID, Metadata: metadata, CreatedAt: now,
	})
}

func (s *ExtensionService) recordLifecycleEvent(item model.ExtensionInstallation, eventType string, now time.Time) error {
	raw, err := json.Marshal(map[string]any{
		"installation_id": item.ID, "organization_id": item.OrganizationID,
		"application_id": item.ApplicationID, "workspace_id": item.WorkspaceID, "status": item.Status,
	})
	if err != nil {
		return err
	}
	return s.repo.RecordDomainEvent(model.DomainEvent{
		WorkspaceID: item.WorkspaceID, EventType: eventType, AggregateType: "extension_installation",
		AggregateID: strconv.FormatInt(item.ID, 10), SchemaVersion: 1, Data: raw, OccurredAt: now,
	})
}

func extensionNormalizeApplicationManifest(workspaceID, actorUserID int64, req model.CreateMarketplaceApplicationRequest) (model.MarketplaceApplication, error) {
	slug := extensionNormalizeSlug(req.Slug)
	name := strings.TrimSpace(req.Name)
	summary := strings.TrimSpace(req.Summary)
	description := strings.TrimSpace(req.Description)
	version := strings.TrimSpace(req.Version)
	homepage := strings.TrimSpace(req.HomepageURL)
	privacy := strings.TrimSpace(req.PrivacyURL)
	categories := extensionNormalizeStrings(req.Categories)
	scopes, err := extensionNormalizeScopes(req.RequestedScopes)
	if err != nil {
		return model.MarketplaceApplication{}, err
	}
	eventTypes, err := extensionNormalizeEventTypes(req.EventTypes)
	if err != nil {
		return model.MarketplaceApplication{}, err
	}
	configSchema, err := extensionNormalizeConfigSchema(req.ConfigSchema)
	if err != nil {
		return model.MarketplaceApplication{}, err
	}
	packs, err := extensionNormalizePacks(req.Packs)
	if err != nil {
		return model.MarketplaceApplication{}, err
	}
	daily := req.DailyRequestLimit
	if daily == 0 {
		daily = 1000
	}
	monthly := req.MonthlyRequestLimit
	if monthly == 0 {
		monthly = 20000
	}
	if !extensionValidSlug(slug) || name == "" || len(name) > 200 || summary == "" || len(summary) > 300 ||
		description == "" || len(description) > 10000 || version == "" || len(version) > 80 ||
		len(categories) == 0 || len(categories) > 10 || len(scopes) == 0 || daily < 1 || monthly < daily ||
		monthly > 100000000 || (homepage != "" && !extensionValidMetadataURL(homepage)) ||
		(privacy != "" && !extensionValidMetadataURL(privacy)) {
		return model.MarketplaceApplication{}, ErrInvalidMarketplaceApplication
	}
	return model.MarketplaceApplication{
		PublisherWorkspaceID: workspaceID, Slug: slug, Name: name, Summary: summary, Description: description,
		Version: version, ManifestVersion: 1, ExecutionModel: model.ExtensionExecutionRemote,
		HomepageURL: homepage, PrivacyURL: privacy, Categories: categories, RequestedScopes: scopes,
		EventTypes: eventTypes, ConfigSchema: configSchema, Packs: packs, DailyRequestLimit: daily,
		MonthlyRequestLimit: monthly, Status: model.MarketplaceAppDraft, CreatedByUserID: actorUserID,
	}, nil
}

func extensionNormalizeScopes(values []string) ([]string, error) {
	allowed := map[string]bool{
		model.ScopeTasksRead: true, model.ScopeTasksWrite: true,
		model.ScopeWorkspaceRead: true, model.ScopeAuditRead: true,
	}
	items := extensionNormalizeStrings(values)
	for _, item := range items {
		if !allowed[item] {
			return nil, ErrInvalidMarketplaceApplication
		}
	}
	return items, nil
}

func extensionRequestedScopes(requested, allowed []string) ([]string, error) {
	if len(requested) == 0 {
		return append([]string(nil), allowed...), nil
	}
	items, err := extensionNormalizeScopes(requested)
	if err != nil {
		return nil, ErrInvalidExtensionInstallation
	}
	for _, item := range items {
		if !extensionContains(allowed, item) {
			return nil, ErrInvalidExtensionInstallation
		}
	}
	return items, nil
}

func extensionNormalizeEventTypes(values []string) ([]string, error) {
	items := extensionNormalizeStrings(values)
	for _, item := range items {
		if item == "*" {
			continue
		}
		if len(item) > 160 || !strings.Contains(item, ".") || strings.ContainsAny(item, " 	
") {
			return nil, ErrInvalidMarketplaceApplication
		}
	}
	return items, nil
}

func extensionRequestedEvents(requested, allowed []string) ([]string, error) {
	items := extensionNormalizeStrings(requested)
	if len(items) == 0 {
		return nil, ErrInvalidExtensionSubscription
	}
	allowAll := extensionContains(allowed, "*")
	for _, item := range items {
		if item != "*" && (len(item) > 160 || !strings.Contains(item, ".")) {
			return nil, ErrInvalidExtensionSubscription
		}
		if !allowAll && !extensionContains(allowed, item) {
			return nil, ErrInvalidExtensionSubscription
		}
	}
	return items, nil
}

func extensionNormalizeConfigSchema(values []model.ExtensionConfigField) ([]model.ExtensionConfigField, error) {
	if len(values) > 50 {
		return nil, ErrInvalidMarketplaceApplication
	}
	seen := map[string]bool{}
	out := make([]model.ExtensionConfigField, 0, len(values))
	for _, item := range values {
		item.Key = extensionNormalizeSlug(item.Key)
		item.Label = strings.TrimSpace(item.Label)
		item.Description = strings.TrimSpace(item.Description)
		if !extensionValidSlug(item.Key) || item.Label == "" || len(item.Label) > 160 || seen[item.Key] {
			return nil, ErrInvalidMarketplaceApplication
		}
		seen[item.Key] = true
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func extensionNormalizePacks(values []model.ExtensionPack) ([]model.ExtensionPack, error) {
	if len(values) > 25 {
		return nil, ErrInvalidMarketplaceApplication
	}
	out := make([]model.ExtensionPack, 0, len(values))
	for _, item := range values {
		item.Type = strings.ToLower(strings.TrimSpace(item.Type))
		item.Name = strings.TrimSpace(item.Name)
		item.Version = strings.TrimSpace(item.Version)
		item.Description = strings.TrimSpace(item.Description)
		if (item.Type != model.ExtensionPackWorkflow && item.Type != model.ExtensionPackCompliance && item.Type != model.ExtensionPackReporting) ||
			item.Name == "" || item.Version == "" || len(item.Name) > 200 || len(item.Version) > 80 {
			return nil, ErrInvalidMarketplaceApplication
		}
		raw, err := json.Marshal(item.Definition)
		if err != nil || len(raw) > 64*1024 {
			return nil, ErrInvalidMarketplaceApplication
		}
		out = append(out, item)
	}
	return out, nil
}

func extensionValidateInstallationConfig(schema []model.ExtensionConfigField, config, secretRefs map[string]string) (map[string]string, map[string]string, error) {
	config = extensionNormalizeStringMap(config)
	secretRefs = extensionNormalizeStringMap(secretRefs)
	allowed := map[string]model.ExtensionConfigField{}
	for _, field := range schema {
		allowed[field.Key] = field
	}
	for key := range config {
		field, ok := allowed[key]
		if !ok || field.Secret {
			return nil, nil, ErrInvalidExtensionConfiguration
		}
	}
	for key, value := range secretRefs {
		field, ok := allowed[key]
		if !ok || !field.Secret || !extensionValidSecretRef(value) {
			return nil, nil, ErrInvalidExtensionConfiguration
		}
	}
	for _, field := range schema {
		if !field.Required {
			continue
		}
		if field.Secret {
			if strings.TrimSpace(secretRefs[field.Key]) == "" {
				return nil, nil, ErrInvalidExtensionConfiguration
			}
		} else if strings.TrimSpace(config[field.Key]) == "" {
			return nil, nil, ErrInvalidExtensionConfiguration
		}
	}
	return config, secretRefs, nil
}

func extensionNormalizeStringMap(input map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range input {
		key = extensionNormalizeSlug(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" && len(key) <= 100 && len(value) <= 2000 {
			out[key] = value
		}
	}
	return out
}

func extensionValidSecretRef(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) >= 5 && len(value) <= 1000 && !strings.ContainsAny(value, "
")
}

func extensionGenerateSecret() (string, string, string, error) {
	raw, _, err := auth.GenerateOpaqueToken()
	if err != nil {
		return "", "", "", err
	}
	secret := "stm_ext_" + raw
	prefix := secret
	if len(prefix) > 18 {
		prefix = prefix[:18]
	}
	return secret, prefix, auth.HashOpaqueToken(secret), nil
}

func extensionNormalizeSlug(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func extensionValidSlug(value string) bool {
	if value == "" || len(value) > 100 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func extensionValidMetadataURL(value string) bool {
	u, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func extensionNormalizeStrings(values []string) []string {
	seen := map[string]bool{}
	items := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		items = append(items, value)
	}
	sort.Strings(items)
	return items
}

func extensionContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

