package events

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

type Worker struct {
	repo          repository.EventRepository
	client        *http.Client
	workerID      string
	batchSize     int
	pollInterval  time.Duration
	lockTimeout   time.Duration
	allowInsecure bool
	logger        *slog.Logger
}

type WorkerOptions struct {
	WorkerID      string
	BatchSize     int
	PollInterval  time.Duration
	LockTimeout   time.Duration
	HTTPTimeout   time.Duration
	AllowInsecure bool
	Logger        *slog.Logger
}

func NewWorker(repo repository.EventRepository, options WorkerOptions) *Worker {
	if options.BatchSize <= 0 {
		options.BatchSize = 50
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 2 * time.Second
	}
	if options.LockTimeout <= 0 {
		options.LockTimeout = 2 * time.Minute
	}
	if options.HTTPTimeout <= 0 {
		options.HTTPTimeout = 10 * time.Second
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &Worker{
		repo:     repo,
		client:   newHTTPClient(options.HTTPTimeout, options.AllowInsecure),
		workerID: options.WorkerID, batchSize: options.BatchSize,
		pollInterval: options.PollInterval, lockTimeout: options.LockTimeout,
		allowInsecure: options.AllowInsecure, logger: options.Logger,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		if _, err := w.ProcessOnce(ctx); err != nil {
			w.logger.Error("event_worker_iteration_failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *Worker) ProcessOnce(ctx context.Context) (int, error) {
	now := time.Now().UTC()
	if released, err := w.repo.ReleaseStaleLocks(now.Add(-w.lockTimeout)); err != nil {
		return 0, err
	} else if released > 0 {
		w.logger.Warn("event_worker_stale_locks_released", "count", released)
	}

	events, err := w.repo.ClaimReady(w.workerID, w.batchSize, now)
	if err != nil {
		return 0, err
	}
	for _, event := range events {
		if err := w.processEvent(ctx, event); err != nil {
			dead, markErr := w.repo.MarkFailed(event.ID, err.Error(), time.Now().UTC())
			if markErr != nil {
				return 0, markErr
			}
			w.logger.Warn("event_delivery_failed",
				"event_id", event.EventKey,
				"event_type", event.EventType,
				"workspace_id", event.WorkspaceID,
				"dead_lettered", dead,
				"error", err,
			)
		}
	}
	return len(events), nil
}

func (w *Worker) processEvent(ctx context.Context, event model.DomainEvent) error {
	subscriptions, err := w.repo.PendingSubscriptions(event)
	if err != nil {
		return err
	}
	if len(subscriptions) == 0 {
		return w.repo.MarkProcessed(event.ID, time.Now().UTC())
	}

	for _, subscription := range subscriptions {
		delivery := model.WebhookDelivery{
			EventID: event.ID, SubscriptionID: subscription.ID,
			Attempt: event.Attempts + 1, Status: "failed", AttemptedAt: time.Now().UTC(),
		}
		status, responseBody, deliverErr := w.deliver(ctx, event, subscription)
		delivery.HTTPStatus = status
		delivery.ResponseBody = responseBody
		if deliverErr != nil {
			delivery.Error = deliverErr.Error()
			if err := w.repo.RecordDelivery(delivery); err != nil {
				return err
			}
			return fmt.Errorf("subscription %d: %w", subscription.ID, deliverErr)
		}
		now := time.Now().UTC()
		delivery.Status = "delivered"
		delivery.DeliveredAt = &now
		if err := w.repo.RecordDelivery(delivery); err != nil {
			return err
		}
	}

	return w.repo.MarkProcessed(event.ID, time.Now().UTC())
}

func (w *Worker) deliver(ctx context.Context, event model.DomainEvent, subscription model.WebhookSubscription) (int, string, error) {
	endpoint, err := url.Parse(subscription.URL)
	if err != nil {
		return 0, "", err
	}
	if err := ValidateWebhookURL(endpoint, w.allowInsecure); err != nil {
		return 0, "", err
	}

	body, err := json.Marshal(event)
	if err != nil {
		return 0, "", err
	}
	timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	signature := Sign(subscription.SigningSecret, timestamp, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "task-manager-webhook/1")
	req.Header.Set("X-Webhook-Id", event.EventKey)
	req.Header.Set("X-Webhook-Event", event.EventType)
	req.Header.Set("X-Webhook-Timestamp", timestamp)
	req.Header.Set("X-Webhook-Signature", "v1="+signature)

	response, err := w.client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
	text := string(raw)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, text, fmt.Errorf("webhook returned HTTP %d", response.StatusCode)
	}
	return response.StatusCode, text, nil
}

func Sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func newHTTPClient(timeout time.Duration, allowInsecure bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if !allowInsecure {
		dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if blockedWebhookIP(ip) {
					return nil, fmt.Errorf("webhook destination resolves to a private or local address")
				}
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("webhook destination did not resolve")
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return ValidateWebhookURL(req.URL, allowInsecure)
		},
	}
}

func blockedWebhookIP(ip net.IP) bool {
	return ip == nil ||
		!ip.IsGlobalUnicast() ||
		ip.IsPrivate() ||
		ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

func ValidateWebhookURL(endpoint *url.URL, allowInsecure bool) error {
	if endpoint == nil || endpoint.Hostname() == "" {
		return fmt.Errorf("webhook URL must include a host")
	}
	if endpoint.Scheme != "https" {
		if !allowInsecure || endpoint.Scheme != "http" {
			return fmt.Errorf("webhook URL must use https")
		}
	}
	host := strings.ToLower(endpoint.Hostname())
	if allowInsecure {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && blockedWebhookIP(ip) {
		return fmt.Errorf("webhook URL must not target a private or local IP")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("webhook URL must not target localhost")
	}
	return nil
}
