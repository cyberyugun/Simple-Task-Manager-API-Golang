package service

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrAsyncUnavailable       = errors.New("asynchronous processing requires PostgreSQL")
	ErrWebhookForbidden       = errors.New("webhook administration requires owner or admin role")
	ErrInvalidWebhookURL      = errors.New("webhook URL must use HTTPS")
	ErrInvalidWebhookSecret   = errors.New("webhook secret must be at least 32 characters")
	ErrInvalidWebhookEvent    = errors.New("unsupported webhook event type")
)

type EventService struct {
	repo repository.EventRepository
}

func NewEventService(repo repository.EventRepository) *EventService {
	return &EventService{repo: repo}
}

func (s *EventService) CreateSubscription(access model.WorkspaceAccess, userID int64, req model.CreateWebhookSubscriptionRequest) (model.WebhookSubscription, error) {
	if err := requireWebhookAdmin(access); err != nil {
		return model.WebhookSubscription{}, err
	}
	if s.repo == nil {
		return model.WebhookSubscription{}, ErrAsyncUnavailable
	}
	rawURL := strings.TrimSpace(req.URL)
	if err := validateWebhookURL(rawURL); err != nil {
		return model.WebhookSubscription{}, err
	}
	secret := strings.TrimSpace(req.Secret)
	if len(secret) < 32 {
		return model.WebhookSubscription{}, ErrInvalidWebhookSecret
	}
	eventTypes, err := normalizeEventTypes(req.EventTypes)
	if err != nil {
		return model.WebhookSubscription{}, err
	}
	now := time.Now()
	return s.repo.CreateSubscription(model.WebhookSubscription{
		WorkspaceID:     access.ID,
		URL:             rawURL,
		EventTypes:      eventTypes,
		Active:          true,
		CreatedByUserID: userID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, secret)
}

func (s *EventService) ListSubscriptions(access model.WorkspaceAccess) ([]model.WebhookSubscription, error) {
	if err := requireWebhookAdmin(access); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, ErrAsyncUnavailable
	}
	return s.repo.ListSubscriptions(access.ID)
}

func (s *EventService) DeleteSubscription(access model.WorkspaceAccess, subscriptionID int64) error {
	if err := requireWebhookAdmin(access); err != nil {
		return err
	}
	if s.repo == nil {
		return ErrAsyncUnavailable
	}
	return s.repo.DeleteSubscription(access.ID, subscriptionID)
}

func (s *EventService) Stats(access model.WorkspaceAccess) (model.OutboxStats, error) {
	if err := requireWebhookAdmin(access); err != nil {
		return model.OutboxStats{}, err
	}
	if s.repo == nil {
		return model.OutboxStats{}, ErrAsyncUnavailable
	}
	return s.repo.Stats(access.ID)
}

func (s *EventService) ReplayDead(access model.WorkspaceAccess) (int64, error) {
	if err := requireWebhookAdmin(access); err != nil {
		return 0, err
	}
	if s.repo == nil {
		return 0, ErrAsyncUnavailable
	}
	return s.repo.ReplayDead(access.ID, time.Now())
}

func requireWebhookAdmin(access model.WorkspaceAccess) error {
	if access.Role != model.WorkspaceRoleOwner && access.Role != model.WorkspaceRoleAdmin {
		return ErrWebhookForbidden
	}
	return nil
}

func normalizeEventTypes(values []string) ([]string, error) {
	if len(values) == 0 {
		return []string{"task.*"}, nil
	}
	allowed := map[string]bool{
		"task.*":         true,
		"task.created":   true,
		"task.updated":   true,
		"task.completed": true,
		"task.deleted":   true,
	}
	seen := make(map[string]bool)
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !allowed[value] {
			return nil, ErrInvalidWebhookEvent
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out, nil
}

func validateWebhookURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Scheme != "https" {
		return ErrInvalidWebhookURL
	}
	return nil
}
