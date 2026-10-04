package service

import (
	"context"
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrIntegrationForbidden          = errors.New("integration action is forbidden")
	ErrInvalidIntegrationConnection  = errors.New("invalid integration connection")
	ErrInvalidIntegrationDelivery    = errors.New("invalid integration delivery")
	ErrIntegrationSignature          = errors.New("invalid integration signature")
	ErrIntegrationConnectionDisabled = errors.New("integration connection is disabled")
)

type IntegrationCredentialProvider interface {
	CredentialsForConnection(context.Context, int64) (map[string]any, error)
}

type IntegrationService struct {
	repo               repository.IntegrationRepository
	orgs               repository.OrganizationRepository
	cipher             *IntegrationCredentialCipher
	client             *http.Client
	allowInsecure      bool
	credentialProvider IntegrationCredentialProvider
}

func NewIntegrationService(repo repository.IntegrationRepository, orgs repository.OrganizationRepository, cipher *IntegrationCredentialCipher, allowInsecure bool) *IntegrationService {
	return &IntegrationService{
		repo:          repo,
		orgs:          orgs,
		cipher:        cipher,
		client:        &http.Client{Timeout: 10 * time.Second},
		allowInsecure: allowInsecure,
	}
}

func (s *IntegrationService) SetHTTPClient(client *http.Client) {
	if client != nil {
		s.client = client
	}
}

func (s *IntegrationService) SetCredentialProvider(provider IntegrationCredentialProvider) {
	s.credentialProvider = provider
}

func (s *IntegrationService) Connectors() []model.IntegrationConnector {
	return []model.IntegrationConnector{
		{Provider: model.IntegrationProviderSlack, DisplayName: "Slack", AuthTypes: []string{model.IntegrationAuthOAuth2, model.IntegrationAuthBearerToken}, SupportsInbound: true, SupportsOutbound: true, SupportsOAuth: true, SupportsRateLimits: true},
		{Provider: model.IntegrationProviderMicrosoftTeams, DisplayName: "Microsoft Teams", AuthTypes: []string{model.IntegrationAuthOAuth2, model.IntegrationAuthBearerToken, model.IntegrationAuthWebhookSecret}, SupportsInbound: true, SupportsOutbound: true, SupportsOAuth: true, SupportsRateLimits: true},
		{Provider: model.IntegrationProviderJira, DisplayName: "Jira", AuthTypes: []string{model.IntegrationAuthOAuth2, model.IntegrationAuthBearerToken, model.IntegrationAuthWebhookSecret}, SupportsInbound: true, SupportsOutbound: true, SupportsOAuth: true, SupportsRateLimits: true},
		{Provider: model.IntegrationProviderGitHub, DisplayName: "GitHub", AuthTypes: []string{model.IntegrationAuthOAuth2, model.IntegrationAuthBearerToken, model.IntegrationAuthWebhookSecret}, SupportsInbound: true, SupportsOutbound: true, SupportsOAuth: true, SupportsRateLimits: true},
		{Provider: model.IntegrationProviderGenericWebhook, DisplayName: "Generic Webhook", AuthTypes: []string{model.IntegrationAuthWebhookSecret, model.IntegrationAuthBearerToken}, SupportsInbound: true, SupportsOutbound: true, SupportsOAuth: false, SupportsRateLimits: true},
	}
}

func (s *IntegrationService) Connections(actorUserID, organizationID int64) ([]model.IntegrationConnection, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListIntegrationConnections(organizationID)
}

func (s *IntegrationService) CreateConnection(actorUserID, organizationID int64, req model.CreateIntegrationConnectionRequest) (model.IntegrationConnection, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.IntegrationConnection{}, err
	}
	if err := s.validateConnection(req.Provider, req.Name, req.AuthType, req.Config, req.Credentials); err != nil {
		return model.IntegrationConnection{}, err
	}
	raw, err := json.Marshal(req.Credentials)
	if err != nil {
		return model.IntegrationConnection{}, ErrInvalidIntegrationConnection
	}
	encrypted, err := s.cipher.Encrypt(raw)
	if err != nil {
		return model.IntegrationConnection{}, err
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateIntegrationConnection(model.IntegrationConnection{
		OrganizationID:      organizationID,
		Provider:            strings.ToLower(strings.TrimSpace(req.Provider)),
		Name:                strings.TrimSpace(req.Name),
		Status:              model.IntegrationConnectionActive,
		AuthType:            strings.ToLower(strings.TrimSpace(req.AuthType)),
		Config:              req.Config,
		HealthStatus:        model.IntegrationHealthUnknown,
		ConsecutiveFailures: 0,
		CreatedByUserID:     actorUserID,
		UpdatedByUserID:     actorUserID,
		CreatedAt:           now,
		UpdatedAt:           now,
	}, encrypted)
	if err == nil {
		s.audit(organizationID, actorUserID, "integration.connection.created", "integration_connection", fmt.Sprint(item.ID), map[string]any{"provider": item.Provider, "auth_type": item.AuthType})
	}
	return item, err
}

func (s *IntegrationService) UpdateConnection(actorUserID, organizationID, connectionID int64, req model.UpdateIntegrationConnectionRequest) (model.IntegrationConnection, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.IntegrationConnection{}, err
	}
	item, err := s.repo.GetIntegrationConnection(organizationID, connectionID)
	if err != nil {
		return model.IntegrationConnection{}, err
	}
	name := strings.TrimSpace(req.Name)
	status := strings.ToLower(strings.TrimSpace(req.Status))
	if name == "" || len(name) > 200 || (status != model.IntegrationConnectionActive && status != model.IntegrationConnectionDisabled) {
		return model.IntegrationConnection{}, ErrInvalidIntegrationConnection
	}
	if err := validateIntegrationConfig(req.Config, s.allowInsecure); err != nil {
		return model.IntegrationConnection{}, err
	}
	item.Name = name
	item.Status = status
	item.Config = req.Config
	item.UpdatedByUserID = actorUserID
	item.UpdatedAt = time.Now().UTC()
	var encrypted *string
	if req.Credentials != nil {
		if err := validateIntegrationCredentials(item.AuthType, req.Credentials); err != nil {
			return model.IntegrationConnection{}, err
		}
		raw, err := json.Marshal(req.Credentials)
		if err != nil {
			return model.IntegrationConnection{}, ErrInvalidIntegrationConnection
		}
		value, err := s.cipher.Encrypt(raw)
		if err != nil {
			return model.IntegrationConnection{}, err
		}
		encrypted = &value
	}
	updated, err := s.repo.UpdateIntegrationConnection(item, encrypted)
	if err == nil {
		s.audit(organizationID, actorUserID, "integration.connection.updated", "integration_connection", fmt.Sprint(connectionID), map[string]any{"status": status})
	}
	return updated, err
}

func (s *IntegrationService) Deliveries(actorUserID, organizationID int64) ([]model.IntegrationDelivery, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListIntegrationDeliveries(organizationID, 200)
}

func (s *IntegrationService) InboundEvents(actorUserID, organizationID int64) ([]model.IntegrationInboundEvent, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListInboundIntegrationEvents(organizationID, 200)
}

func (s *IntegrationService) QueueDelivery(actorUserID, organizationID int64, req model.CreateIntegrationDeliveryRequest) (model.IntegrationDelivery, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.IntegrationDelivery{}, err
	}
	connection, err := s.repo.GetIntegrationConnection(organizationID, req.ConnectionID)
	if err != nil {
		return model.IntegrationDelivery{}, err
	}
	if connection.Status != model.IntegrationConnectionActive {
		return model.IntegrationDelivery{}, ErrIntegrationConnectionDisabled
	}
	eventType := strings.TrimSpace(req.EventType)
	if eventType == "" || len(eventType) > 160 || req.Payload == nil {
		return model.IntegrationDelivery{}, ErrInvalidIntegrationDelivery
	}
	eventKey, err := randomIntegrationKey()
	if err != nil {
		return model.IntegrationDelivery{}, err
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateIntegrationDelivery(model.IntegrationDelivery{
		OrganizationID:  organizationID,
		ConnectionID:    req.ConnectionID,
		EventKey:        eventKey,
		EventType:       eventType,
		Payload:         req.Payload,
		Status:          model.IntegrationDeliveryPending,
		MaxAttempts:     5,
		AvailableAt:     now,
		CreatedByUserID: actorUserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err == nil {
		s.audit(organizationID, actorUserID, "integration.delivery.queued", "integration_delivery", fmt.Sprint(item.ID), map[string]any{"event_type": eventType, "connection_id": req.ConnectionID})
	}
	return item, err
}

func (s *IntegrationService) ReplayDelivery(actorUserID, organizationID, deliveryID int64) error {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return err
	}
	if _, err := s.repo.GetIntegrationDelivery(organizationID, deliveryID); err != nil {
		return err
	}
	if err := s.repo.ReplayIntegrationDelivery(organizationID, deliveryID, time.Now().UTC()); err != nil {
		return err
	}
	s.audit(organizationID, actorUserID, "integration.delivery.replayed", "integration_delivery", fmt.Sprint(deliveryID), nil)
	return nil
}

func (s *IntegrationService) AcceptInbound(connectionID int64, signature string, raw []byte) (model.IntegrationInboundEvent, bool, error) {
	secretRow, err := s.repo.GetIntegrationConnectionSecret(connectionID)
	if err != nil {
		return model.IntegrationInboundEvent{}, false, err
	}
	connection, err := s.repo.GetIntegrationConnection(secretRow.OrganizationID, connectionID)
	if err != nil {
		return model.IntegrationInboundEvent{}, false, err
	}
	if connection.Status != model.IntegrationConnectionActive {
		return model.IntegrationInboundEvent{}, false, ErrIntegrationConnectionDisabled
	}
	credentials, err := s.credentialsForConnection(context.Background(), connectionID, secretRow.EncryptedCredentials)
	if err != nil {
		return model.IntegrationInboundEvent{}, false, err
	}
	signingSecret := credentialString(credentials, "signing_secret")
	if signingSecret == "" || !validIntegrationSignature(signingSecret, signature, raw) {
		return model.IntegrationInboundEvent{}, false, ErrIntegrationSignature
	}
	var envelope model.IntegrationInboundEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return model.IntegrationInboundEvent{}, false, ErrInvalidIntegrationDelivery
	}
	envelope.EventID = strings.TrimSpace(envelope.EventID)
	envelope.EventType = strings.TrimSpace(envelope.EventType)
	if envelope.EventID == "" || envelope.EventType == "" || envelope.Payload == nil || len(envelope.EventID) > 200 || len(envelope.EventType) > 160 {
		return model.IntegrationInboundEvent{}, false, ErrInvalidIntegrationDelivery
	}
	now := time.Now().UTC()
	item, err := s.repo.RecordInboundIntegrationEvent(model.IntegrationInboundEvent{
		OrganizationID:  connection.OrganizationID,
		ConnectionID:    connectionID,
		ProviderEventID: envelope.EventID,
		EventType:       envelope.EventType,
		Payload:         envelope.Payload,
		Status:          model.IntegrationInboundAccepted,
		ReceivedAt:      now,
	})
	if errors.Is(err, repository.ErrIntegrationInboundDuplicate) {
		return model.IntegrationInboundEvent{
			OrganizationID:  connection.OrganizationID,
			ConnectionID:    connectionID,
			ProviderEventID: envelope.EventID,
			EventType:       envelope.EventType,
			Status:          model.IntegrationInboundDuplicate,
			ReceivedAt:      now,
		}, true, nil
	}
	if err != nil {
		return model.IntegrationInboundEvent{}, false, err
	}
	_ = s.repo.UpdateIntegrationHealth(connectionID, model.IntegrationHealthHealthy, 0, now, nil, nil)
	return item, false, nil
}

func (s *IntegrationService) ProcessBatch(workerID string, limit int) ([]model.IntegrationDelivery, error) {
	now := time.Now().UTC()
	_, _ = s.repo.ReleaseIntegrationLocks(now.Add(-5 * time.Minute))
	items, err := s.repo.ClaimIntegrationDeliveries(workerID, limit, now)
	if err != nil {
		return nil, err
	}
	for idx := range items {
		items[idx], err = s.dispatch(items[idx])
		if err != nil {
			return items[:idx+1], err
		}
	}
	return items, nil
}

func (s *IntegrationService) dispatch(delivery model.IntegrationDelivery) (model.IntegrationDelivery, error) {
	connection, err := s.repo.GetIntegrationConnection(delivery.OrganizationID, delivery.ConnectionID)
	if err != nil {
		return delivery, err
	}
	if connection.Status != model.IntegrationConnectionActive {
		dead, markErr := s.repo.MarkIntegrationFailed(delivery.ID, 0, ErrIntegrationConnectionDisabled.Error(), time.Now().UTC())
		if markErr != nil {
			return delivery, markErr
		}
		if dead {
			delivery.Status = model.IntegrationDeliveryDeadLetter
		} else {
			delivery.Status = model.IntegrationDeliveryRetry
		}
		return delivery, nil
	}
	targetURL := integrationConfigString(connection.Config, "target_url")
	if err := validateIntegrationTargetURL(targetURL, s.allowInsecure); err != nil {
		dead, markErr := s.repo.MarkIntegrationFailed(delivery.ID, 0, err.Error(), time.Now().UTC())
		if markErr != nil {
			return delivery, markErr
		}
		if dead {
			delivery.Status = model.IntegrationDeliveryDeadLetter
		} else {
			delivery.Status = model.IntegrationDeliveryRetry
		}
		s.updateFailureHealth(connection)
		return delivery, nil
	}
	secretRow, err := s.repo.GetIntegrationConnectionSecret(connection.ID)
	if err != nil {
		return delivery, err
	}
	credentials, err := s.credentialsForConnection(context.Background(), connection.ID, secretRow.EncryptedCredentials)
	if err != nil {
		return delivery, err
	}
	envelope := model.IntegrationInboundEnvelope{EventID: delivery.EventKey, EventType: delivery.EventType, Payload: delivery.Payload}
	body, err := json.Marshal(envelope)
	if err != nil {
		return delivery, err
	}
	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return delivery, err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret := credentialString(credentials, "signing_secret"); secret != "" {
		req.Header.Set("X-Integration-Signature", integrationSignature(secret, body))
	}
	if token := firstCredential(credentials, "access_token", "token", "bearer_token"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, requestErr := s.client.Do(req)
	now := time.Now().UTC()
	if requestErr != nil {
		dead, markErr := s.repo.MarkIntegrationFailed(delivery.ID, 0, requestErr.Error(), now)
		if markErr != nil {
			return delivery, markErr
		}
		s.updateFailureHealth(connection)
		if dead {
			delivery.Status = model.IntegrationDeliveryDeadLetter
		} else {
			delivery.Status = model.IntegrationDeliveryRetry
		}
		delivery.Attempts++
		return delivery, nil
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	remaining, reset := parseIntegrationRateLimit(resp)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := s.repo.MarkIntegrationDelivered(delivery.ID, resp.StatusCode, now); err != nil {
			return delivery, err
		}
		_ = s.repo.UpdateIntegrationHealth(connection.ID, model.IntegrationHealthHealthy, 0, now, remaining, reset)
		delivery.Status = model.IntegrationDeliveryDelivered
		delivery.LastHTTPStatus = resp.StatusCode
		delivery.DeliveredAt = &now
		return delivery, nil
	}
	dead, err := s.repo.MarkIntegrationFailed(delivery.ID, resp.StatusCode, fmt.Sprintf("integration endpoint returned HTTP %d", resp.StatusCode), now)
	if err != nil {
		return delivery, err
	}
	s.updateFailureHealth(connection)
	if dead {
		delivery.Status = model.IntegrationDeliveryDeadLetter
	} else {
		delivery.Status = model.IntegrationDeliveryRetry
	}
	delivery.Attempts++
	delivery.LastHTTPStatus = resp.StatusCode
	return delivery, nil
}

func (s *IntegrationService) updateFailureHealth(connection model.IntegrationConnection) {
	now := time.Now().UTC()
	failures := connection.ConsecutiveFailures + 1
	_ = s.repo.UpdateIntegrationHealth(connection.ID, model.IntegrationHealthDegraded, failures, now, connection.RateLimitRemaining, connection.RateLimitResetAt)
}

func (s *IntegrationService) credentialsForConnection(ctx context.Context, connectionID int64, encryptedFallback string) (map[string]any, error) {
	if s.credentialProvider != nil {
		return s.credentialProvider.CredentialsForConnection(ctx, connectionID)
	}
	return s.decryptCredentials(encryptedFallback)
}

func (s *IntegrationService) decryptCredentials(encrypted string) (map[string]any, error) {
	raw, err := s.cipher.Decrypt(encrypted)
	if err != nil {
		return nil, err
	}
	var credentials map[string]any
	if err := json.Unmarshal(raw, &credentials); err != nil {
		return nil, err
	}
	return credentials, nil
}

func (s *IntegrationService) validateConnection(provider, name, authType string, config, credentials map[string]any) error {
	provider = strings.ToLower(strings.TrimSpace(provider))
	authType = strings.ToLower(strings.TrimSpace(authType))
	if strings.TrimSpace(name) == "" || len(strings.TrimSpace(name)) > 200 || !validIntegrationProvider(provider) || !validIntegrationAuth(authType) {
		return ErrInvalidIntegrationConnection
	}
	if err := validateIntegrationConfig(config, s.allowInsecure); err != nil {
		return err
	}
	return validateIntegrationCredentials(authType, credentials)
}

func validateIntegrationConfig(config map[string]any, allowInsecure bool) error {
	if config == nil {
		return ErrInvalidIntegrationConnection
	}
	if target := integrationConfigString(config, "target_url"); target != "" {
		return validateIntegrationTargetURL(target, allowInsecure)
	}
	return nil
}

func validateIntegrationCredentials(authType string, credentials map[string]any) error {
	if credentials == nil {
		return ErrInvalidIntegrationConnection
	}
	switch authType {
	case model.IntegrationAuthWebhookSecret:
		if len(credentialString(credentials, "signing_secret")) < 16 {
			return ErrInvalidIntegrationConnection
		}
	case model.IntegrationAuthBearerToken:
		if firstCredential(credentials, "access_token", "token", "bearer_token") == "" && credentialString(credentials, "signing_secret") == "" {
			return ErrInvalidIntegrationConnection
		}
	case model.IntegrationAuthOAuth2:
		if credentialString(credentials, "access_token") == "" && credentialString(credentials, "client_secret") == "" {
			return ErrInvalidIntegrationConnection
		}
	default:
		return ErrInvalidIntegrationConnection
	}
	return nil
}

func validateIntegrationTargetURL(raw string, allowInsecure bool) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && !(allowInsecure && parsed.Scheme == "http")) {
		return ErrInvalidIntegrationConnection
	}
	return nil
}

func validIntegrationProvider(provider string) bool {
	switch provider {
	case model.IntegrationProviderSlack, model.IntegrationProviderMicrosoftTeams, model.IntegrationProviderJira, model.IntegrationProviderGitHub, model.IntegrationProviderGenericWebhook:
		return true
	default:
		return false
	}
}

func validIntegrationAuth(authType string) bool {
	switch authType {
	case model.IntegrationAuthOAuth2, model.IntegrationAuthBearerToken, model.IntegrationAuthWebhookSecret:
		return true
	default:
		return false
	}
}

func integrationConfigString(config map[string]any, key string) string {
	value, ok := config[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func credentialString(credentials map[string]any, key string) string {
	value, ok := credentials[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func firstCredential(credentials map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := credentialString(credentials, key); value != "" {
			return value
		}
	}
	return ""
}

func integrationSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func validIntegrationSignature(secret, provided string, body []byte) bool {
	expected := integrationSignature(secret, body)
	return hmac.Equal([]byte(expected), []byte(strings.TrimSpace(provided)))
}

func randomIntegrationKey() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "int_" + hex.EncodeToString(raw), nil
}

func parseIntegrationRateLimit(resp *http.Response) (*int64, *time.Time) {
	var remaining *int64
	if raw := strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining")); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
			remaining = &value
		}
	}
	var reset *time.Time
	if raw := strings.TrimSpace(resp.Header.Get("X-RateLimit-Reset")); raw != "" {
		if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
			value := time.Unix(seconds, 0).UTC()
			reset = &value
		}
	}
	return remaining, reset
}

func (s *IntegrationService) requireAdmin(userID, organizationID int64) (model.OrganizationMember, error) {
	member, err := s.orgs.GetMember(organizationID, userID)
	if err != nil {
		return model.OrganizationMember{}, ErrIntegrationForbidden
	}
	switch member.Role {
	case model.OrganizationRoleOwner, model.OrganizationRoleAdmin, model.OrganizationRoleDelegatedAdmin:
		return member, nil
	default:
		return model.OrganizationMember{}, ErrIntegrationForbidden
	}
}

func (s *IntegrationService) audit(organizationID, actorUserID int64, action, resourceType, resourceID string, metadata map[string]any) {
	actor := actorUserID
	_ = s.orgs.RecordAudit(model.OrganizationAuditEvent{
		OrganizationID: organizationID, ActorUserID: &actor, Action: action,
		ResourceType: resourceType, ResourceID: resourceID, Metadata: metadata, CreatedAt: time.Now().UTC(),
	})
}
