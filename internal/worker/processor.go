package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/webhook"
)

type Processor struct {
	repo                 repository.EventRepository
	client               *http.Client
	signingKey           string
	workerID             string
	batchSize            int
	lockTTL              time.Duration
	allowPrivateNetworks bool
	logger               *slog.Logger

	fanoutTotal      atomic.Uint64
	deliveredTotal   atomic.Uint64
	retriedTotal     atomic.Uint64
	deadLetterTotal  atomic.Uint64
	processingErrors atomic.Uint64
}

type ProcessorOptions struct {
	HTTPClient           *http.Client
	SigningKey           string
	WorkerID             string
	BatchSize            int
	LockTTL              time.Duration
	AllowPrivateNetworks bool
	Logger               *slog.Logger
}

type RunStats struct {
	FannedOut  int
	Claimed    int
	Delivered  int
	Retried    int
	DeadLetter int
}

func NewProcessor(repo repository.EventRepository, options ProcessorOptions) *Processor {
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	batchSize := options.BatchSize
	if batchSize <= 0 {
		batchSize = 50
	}
	lockTTL := options.LockTTL
	if lockTTL <= 0 {
		lockTTL = 2 * time.Minute
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Processor{
		repo:                 repo,
		client:               client,
		signingKey:           options.SigningKey,
		workerID:             options.WorkerID,
		batchSize:            batchSize,
		lockTTL:              lockTTL,
		allowPrivateNetworks: options.AllowPrivateNetworks,
		logger:               logger,
	}
}

func (p *Processor) RunOnce(ctx context.Context) (RunStats, error) {
	now := time.Now().UTC()
	fannedOut, err := p.repo.FanoutOutbox(p.batchSize, now)
	if err != nil {
		p.processingErrors.Add(1)
		return RunStats{}, fmt.Errorf("fanout outbox: %w", err)
	}
	p.fanoutTotal.Add(uint64(fannedOut))

	deliveries, err := p.repo.ClaimDeliveries(p.workerID, p.batchSize, p.lockTTL, now)
	if err != nil {
		p.processingErrors.Add(1)
		return RunStats{FannedOut: fannedOut}, fmt.Errorf("claim deliveries: %w", err)
	}

	stats := RunStats{FannedOut: fannedOut, Claimed: len(deliveries)}
	for _, delivery := range deliveries {
		outcome := p.deliver(ctx, delivery)
		switch outcome {
		case deliverySucceeded:
			stats.Delivered++
			p.deliveredTotal.Add(1)
		case deliveryDeadLettered:
			stats.DeadLetter++
			p.deadLetterTotal.Add(1)
		default:
			stats.Retried++
			p.retriedTotal.Add(1)
		}
	}
	return stats, nil
}

type deliveryOutcome int

const (
	deliveryRetried deliveryOutcome = iota
	deliverySucceeded
	deliveryDeadLettered
)

func (p *Processor) deliver(ctx context.Context, delivery model.WebhookDelivery) deliveryOutcome {
	now := time.Now().UTC()
	if err := p.validateTarget(delivery.URL); err != nil {
		return p.fail(delivery, 0, err.Error(), now)
	}

	envelope := model.EventEnvelope{
		EventID:       delivery.EventID,
		EventType:     delivery.EventType,
		SchemaVersion: delivery.SchemaVersion,
		WorkspaceID:   delivery.WorkspaceID,
		AggregateType: delivery.AggregateType,
		AggregateID:   delivery.AggregateID,
		OccurredAt:    delivery.OccurredAt,
		Data:          delivery.Payload,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return p.fail(delivery, 0, "encode event: "+err.Error(), now)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.URL, bytes.NewReader(body))
	if err != nil {
		return p.fail(delivery, 0, "build request: "+err.Error(), now)
	}

	timestamp := now.Unix()
	secret := webhook.DeriveSigningSecret(p.signingKey, delivery.SubscriptionID)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "task-manager-webhook-worker/1.0")
	req.Header.Set("X-Webhook-Delivery-ID", webhook.DeliveryID(delivery.ID))
	req.Header.Set("X-Webhook-Event-ID", delivery.EventID)
	req.Header.Set("X-Webhook-Event", delivery.EventType)
	req.Header.Set("X-Webhook-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-Webhook-Signature", webhook.Signature(secret, timestamp, body))
	req.Header.Set("Idempotency-Key", delivery.EventID)

	res, err := p.client.Do(req)
	if err != nil {
		return p.fail(delivery, 0, "request failed: "+err.Error(), now)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		if err := p.repo.MarkDeliverySuccess(delivery.ID, p.workerID, res.StatusCode, now); err != nil {
			p.processingErrors.Add(1)
			p.logger.Error("webhook_delivery_state_failed", "delivery_id", delivery.ID, "error", err)
			return deliveryRetried
		}
		p.logger.Info(
			"webhook_delivered",
			"delivery_id", delivery.ID,
			"event_id", delivery.EventID,
			"event_type", delivery.EventType,
			"status", res.StatusCode,
			"attempt", delivery.AttemptCount,
		)
		return deliverySucceeded
	}

	return p.fail(delivery, res.StatusCode, fmt.Sprintf("unexpected HTTP status %d", res.StatusCode), now)
}

func (p *Processor) fail(delivery model.WebhookDelivery, status int, message string, now time.Time) deliveryOutcome {
	dead := delivery.AttemptCount >= delivery.MaxAttempts
	next := now.Add(retryDelay(delivery.AttemptCount))
	if dead {
		next = now
	}
	if err := p.repo.MarkDeliveryFailure(
		delivery.ID,
		p.workerID,
		status,
		truncate(message, 1000),
		next,
		dead,
		now,
	); err != nil {
		p.processingErrors.Add(1)
		p.logger.Error("webhook_delivery_state_failed", "delivery_id", delivery.ID, "error", err)
		return deliveryRetried
	}
	if dead {
		p.logger.Error(
			"webhook_dead_lettered",
			"delivery_id", delivery.ID,
			"event_id", delivery.EventID,
			"attempts", delivery.AttemptCount,
			"error", message,
		)
		return deliveryDeadLettered
	}
	p.logger.Warn(
		"webhook_retry_scheduled",
		"delivery_id", delivery.ID,
		"event_id", delivery.EventID,
		"attempt", delivery.AttemptCount,
		"next_attempt_at", next,
		"error", message,
	)
	return deliveryRetried
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	exponent := math.Min(float64(attempt-1), 10)
	delay := time.Duration(math.Pow(2, exponent)) * time.Second
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

func (p *Processor) validateTarget(raw string) error {
	if p.allowPrivateNetworks {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	host := parsed.Hostname()
	if host == "" {
		return errors.New("webhook target has no host")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve webhook host: %w", err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("webhook target resolves to a private or local address")
		}
	}
	return nil
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func (p *Processor) Metrics() map[string]uint64 {
	return map[string]uint64{
		"fanout_total":       p.fanoutTotal.Load(),
		"delivered_total":    p.deliveredTotal.Load(),
		"retried_total":      p.retriedTotal.Load(),
		"dead_letter_total":  p.deadLetterTotal.Load(),
		"processing_errors":  p.processingErrors.Load(),
	}
}
