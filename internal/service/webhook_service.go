package service

import (
	"errors"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/webhook"
)

var (
	ErrWebhookDisabled     = errors.New("webhook signing is not configured")
	ErrInvalidWebhookURL   = errors.New("webhook URL must use HTTPS, except loopback development endpoints")
	ErrInvalidWebhookEvent = errors.New("unsupported webhook event type")
)

type WebhookService struct {
	events     repository.EventRepository
	workspaces repository.WorkspaceRepository
	signingKey string
}

func NewWebhookService(events repository.EventRepository, workspaces repository.WorkspaceRepository, signingKey string) *WebhookService {
	return &WebhookService{
		events:     events,
		workspaces: workspaces,
		signingKey: strings.TrimSpace(signingKey),
	}
}

func (s *WebhookService) Create(actorID int64, access model.WorkspaceAccess, req model.CreateWebhookSubscriptionRequest) (model.WebhookSubscription, error) {
	if err := requireWorkspaceAdmin(access); err != nil {
		return model.WebhookSubscription{}, err
	}
	if len(s.signingKey) < 32 {
		return model.WebhookSubscription{}, ErrWebhookDisabled
	}
	endpoint, err := normalizeWebhookURL(req.URL)
	if err != nil {
		return model.WebhookSubscription{}, err
	}
	eventTypes, err := normalizeWebhookEvents(req.EventTypes)
	if err != nil {
		return model.WebhookSubscription{}, err
	}
	sub, err := s.events.CreateSubscription(access.ID, actorID, endpoint, eventTypes, time.Now())
	if err != nil {
		return model.WebhookSubscription{}, err
	}
	sub.SigningSecret = webhook.DeriveSigningSecret(s.signingKey, sub.ID)
	return sub, nil
}

func (s *WebhookService) List(access model.WorkspaceAccess) ([]model.WebhookSubscription, error) {
	if err := requireWorkspaceAdmin(access); err != nil {
		return nil, err
	}
	return s.events.ListSubscriptions(access.ID)
}

func (s *WebhookService) Delete(access model.WorkspaceAccess, subscriptionID int64) error {
	if err := requireWorkspaceAdmin(access); err != nil {
		return err
	}
	return s.events.DeleteSubscription(access.ID, subscriptionID)
}

func (s *WebhookService) Deliveries(access model.WorkspaceAccess, subscriptionID int64, limit int) ([]model.WebhookDelivery, error) {
	if err := requireWorkspaceAdmin(access); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	return s.events.ListDeliveries(access.ID, subscriptionID, limit)
}

func (s *WebhookService) Replay(access model.WorkspaceAccess, subscriptionID, deliveryID int64) error {
	if err := requireWorkspaceAdmin(access); err != nil {
		return err
	}
	return s.events.ReplayDelivery(access.ID, subscriptionID, deliveryID, time.Now())
}

func requireWorkspaceAdmin(access model.WorkspaceAccess) error {
	if access.Role != model.WorkspaceRoleOwner && access.Role != model.WorkspaceRoleAdmin {
		return ErrWorkspaceForbidden
	}
	return nil
}

func normalizeWebhookURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", ErrInvalidWebhookURL
	}
	host := parsed.Hostname()
	if parsed.Scheme != "https" {
		ip := net.ParseIP(host)
		loopback := strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
		if parsed.Scheme != "http" || !loopback {
			return "", ErrInvalidWebhookURL
		}
	}
	parsed.Fragment = ""
	return parsed.String(), nil
}

func normalizeWebhookEvents(values []string) ([]string, error) {
	allowed := map[string]bool{
		model.EventTaskCreated:   true,
		model.EventTaskUpdated:   true,
		model.EventTaskCompleted: true,
		model.EventTaskDeleted:   true,
	}
	seen := make(map[string]bool)
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !allowed[value] {
			return nil, ErrInvalidWebhookEvent
		}
		if !seen[value] {
			seen[value] = true
			items = append(items, value)
		}
	}
	sort.Strings(items)
	return items, nil
}
