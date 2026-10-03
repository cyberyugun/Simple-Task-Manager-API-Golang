package service

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"go-simple-task-api/internal/events"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrInvalidWebhookURL    = errors.New("invalid webhook URL")
	ErrInvalidWebhookEvents = errors.New("webhook event_types must contain supported events")
)

type WebhookService struct {
	workspaces    repository.WorkspaceRepository
	events        repository.EventRepository
	allowInsecure bool
}

func NewWebhookService(workspaces repository.WorkspaceRepository, eventRepo repository.EventRepository, allowInsecure bool) *WebhookService {
	return &WebhookService{workspaces: workspaces, events: eventRepo, allowInsecure: allowInsecure}
}

func (s *WebhookService) Create(actorID, workspaceID int64, req model.CreateWebhookSubscriptionRequest) (model.WebhookSubscription, error) {
	if _, err := s.requireAdmin(actorID, workspaceID); err != nil {
		return model.WebhookSubscription{}, err
	}
	endpoint, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || events.ValidateWebhookURL(endpoint, s.allowInsecure) != nil {
		return model.WebhookSubscription{}, ErrInvalidWebhookURL
	}
	eventTypes, err := normalizeWebhookEventTypes(req.EventTypes)
	if err != nil {
		return model.WebhookSubscription{}, err
	}
	secret, err := generateWebhookSecret()
	if err != nil {
		return model.WebhookSubscription{}, err
	}
	now := time.Now().UTC()
	item, err := s.events.CreateWebhook(workspaceID, actorID, endpoint.String(), secret, eventTypes, now)
	if err != nil {
		return model.WebhookSubscription{}, err
	}
	if err := s.audit(workspaceID, actorID, "webhook.created", item.ID, map[string]any{
		"url": endpoint.String(), "event_types": eventTypes,
	}, now); err != nil {
		return model.WebhookSubscription{}, err
	}
	return item, nil
}

func (s *WebhookService) List(actorID, workspaceID int64) ([]model.WebhookSubscription, error) {
	if _, err := s.requireAdmin(actorID, workspaceID); err != nil {
		return nil, err
	}
	return s.events.ListWebhooks(workspaceID)
}

func (s *WebhookService) Delete(actorID, workspaceID, subscriptionID int64) error {
	if _, err := s.requireAdmin(actorID, workspaceID); err != nil {
		return err
	}
	if err := s.events.DeleteWebhook(workspaceID, subscriptionID); err != nil {
		return err
	}
	return s.audit(workspaceID, actorID, "webhook.deleted", subscriptionID, nil, time.Now().UTC())
}

func (s *WebhookService) requireAdmin(userID, workspaceID int64) (model.WorkspaceAccess, error) {
	access, err := s.workspaces.ResolveAccess(userID, workspaceID, time.Now())
	if err != nil {
		return model.WorkspaceAccess{}, err
	}
	if access.Role != model.WorkspaceRoleOwner && access.Role != model.WorkspaceRoleAdmin {
		return model.WorkspaceAccess{}, ErrWorkspaceForbidden
	}
	return access, nil
}

func (s *WebhookService) audit(workspaceID, actorID int64, action string, subscriptionID int64, metadata map[string]any, now time.Time) error {
	wid := workspaceID
	uid := actorID
	return s.workspaces.RecordAudit(model.AuditEvent{
		WorkspaceID: &wid, ActorUserID: &uid, Action: action,
		ResourceType: "webhook_subscription", ResourceID: fmt.Sprintf("%d", subscriptionID),
		Metadata: metadata, CreatedAt: now,
	})
}

func normalizeWebhookEventTypes(values []string) ([]string, error) {
	allowed := map[string]bool{
		"*":                    true,
		model.EventTaskCreated: true,
		model.EventTaskUpdated: true,
		model.EventTaskDeleted: true,
	}
	seen := make(map[string]bool)
	items := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if !allowed[value] {
			return nil, ErrInvalidWebhookEvents
		}
		if !seen[value] {
			seen[value] = true
			items = append(items, value)
		}
	}
	if len(items) == 0 {
		return nil, ErrInvalidWebhookEvents
	}
	sort.Strings(items)
	return items, nil
}

func generateWebhookSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "whsec_" + base64.RawURLEncoding.EncodeToString(raw), nil
}
