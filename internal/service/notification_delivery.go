package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	eventdelivery "go-simple-task-api/internal/events"
	"go-simple-task-api/internal/model"
)

func (s *NotificationService) ProcessDeliveries(ctx context.Context, workerID string, limit int) ([]model.NotificationDelivery, error) {
	if limit <= 0 {
		limit = 50
	}
	now := time.Now().UTC()
	if _, err := s.repo.ReleaseStaleDeliveryLocks(now.Add(-2 * time.Minute)); err != nil {
		return nil, err
	}
	items, err := s.repo.ClaimDeliveries(workerID, limit, now)
	if err != nil {
		return nil, err
	}
	processed := make([]model.NotificationDelivery, 0, len(items))
	for _, detail := range items {
		preference, err := s.repo.GetPreference(detail.Delivery.UserID)
		if err != nil {
			failed, markErr := s.repo.MarkDeliveryFailed(detail.Delivery.ID, err.Error(), time.Now().UTC())
			if markErr != nil {
				return processed, markErr
			}
			processed = append(processed, failed)
			continue
		}
		subject, body, err := s.render(
			detail.Notification.OrganizationID,
			detail.Notification.TemplateKey,
			preference.Locale,
			detail.Delivery.Channel,
			detail.Notification.Data,
		)
		if err == nil {
			err = s.deliverNotification(ctx, detail, subject, body)
		}
		if err != nil {
			failed, markErr := s.repo.MarkDeliveryFailed(detail.Delivery.ID, err.Error(), time.Now().UTC())
			if markErr != nil {
				return processed, markErr
			}
			processed = append(processed, failed)
			continue
		}
		sent, err := s.repo.MarkDeliverySent(detail.Delivery.ID, time.Now().UTC())
		if err != nil {
			return processed, err
		}
		processed = append(processed, sent)
	}
	return processed, nil
}

func (s *NotificationService) deliverNotification(ctx context.Context, detail model.NotificationDeliveryDetail, subject, body string) error {
	switch detail.Delivery.Channel {
	case model.NotificationChannelEmail:
		return s.deliverProvider(ctx, s.config.EmailProviderURL, map[string]any{
			"channel":         model.NotificationChannelEmail,
			"to":              detail.Delivery.Destination,
			"subject":         subject,
			"body":            body,
			"notification_id": detail.Notification.ID,
			"event_type":      detail.Notification.EventType,
			"data":            detail.Notification.Data,
		})
	case model.NotificationChannelPush:
		return s.deliverProvider(ctx, s.config.PushProviderURL, map[string]any{
			"channel":         model.NotificationChannelPush,
			"token":           detail.Delivery.Destination,
			"title":           subject,
			"body":            body,
			"notification_id": detail.Notification.ID,
			"event_type":      detail.Notification.EventType,
			"data":            detail.Notification.Data,
		})
	case model.NotificationChannelWebhook:
		return s.deliverNotificationWebhook(ctx, detail, subject, body)
	default:
		return fmt.Errorf("unsupported notification delivery channel %q", detail.Delivery.Channel)
	}
}

func (s *NotificationService) deliverProvider(ctx context.Context, endpoint string, payload map[string]any) error {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return fmt.Errorf("notification provider is not configured")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return fmt.Errorf("invalid notification provider URL")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "task-manager-notifications/1")
	if token := strings.TrimSpace(s.config.ProviderToken); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: s.config.HTTPTimeout}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		rawBody, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return fmt.Errorf("notification provider returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(rawBody)))
	}
	return nil
}

func (s *NotificationService) deliverNotificationWebhook(ctx context.Context, detail model.NotificationDeliveryDetail, subject, body string) error {
	if detail.Delivery.EndpointID == nil {
		return fmt.Errorf("notification webhook endpoint is missing")
	}
	endpoint, err := s.repo.GetEndpoint(detail.Delivery.UserID, *detail.Delivery.EndpointID)
	if err != nil {
		return err
	}
	parsed, err := url.Parse(endpoint.Address)
	if err != nil {
		return err
	}
	if err := eventdelivery.ValidateWebhookURL(parsed, s.config.AllowInsecure); err != nil {
		return err
	}
	payload := map[string]any{
		"id":         detail.Notification.ID,
		"event_type": detail.Notification.EventType,
		"title":      subject,
		"body":       body,
		"data":       detail.Notification.Data,
		"created_at": detail.Notification.CreatedAt,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	timestamp := fmt.Sprint(time.Now().UTC().Unix())
	signature := eventdelivery.Sign(endpoint.Secret, timestamp, raw)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "task-manager-notification-webhook/1")
	request.Header.Set("X-Notification-Id", fmt.Sprint(detail.Notification.ID))
	request.Header.Set("X-Notification-Event", detail.Notification.EventType)
	request.Header.Set("X-Notification-Timestamp", timestamp)
	request.Header.Set("X-Notification-Signature", "v1="+signature)

	client := eventdelivery.NewWebhookHTTPClient(s.config.HTTPTimeout, s.config.AllowInsecure)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		rawBody, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return fmt.Errorf("notification webhook returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(rawBody)))
	}
	return nil
}

func (s *NotificationService) ProcessScheduled(now time.Time) (map[string]int, error) {
	result := map[string]int{"reminders": 0, "approvals": 0, "digests": 0}
	reminders, err := s.GenerateReminders(now, 500)
	if err != nil {
		return result, err
	}
	result["reminders"] = reminders
	approvals, err := s.GenerateApprovalNotifications(500)
	if err != nil {
		return result, err
	}
	result["approvals"] = approvals
	digests, err := s.GenerateDigests(now)
	if err != nil {
		return result, err
	}
	result["digests"] = digests
	return result, nil
}
