package worker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

type Config struct {
	BatchSize      int
	PollInterval   time.Duration
	RequestTimeout time.Duration
	MaxAttempts    int
	BaseBackoff    time.Duration
}

type Processor struct {
	repo   repository.EventRepository
	client *http.Client
	cfg    Config
	logger *slog.Logger
}

func New(repo repository.EventRepository, cfg Config, logger *slog.Logger) *Processor {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 25
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 10 * time.Second
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 8
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = time.Second
	}
	return &Processor{
		repo: repo,
		client: &http.Client{
			Timeout: cfg.RequestTimeout,
		},
		cfg:    cfg,
		logger: logger,
	}
}

func (p *Processor) Run(ctx context.Context) error {
	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()

	for {
		if err := p.Once(ctx); err != nil && !errors.Is(err, context.Canceled) {
			p.logger.Error("worker_iteration_failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (p *Processor) Once(ctx context.Context) error {
	now := time.Now()
	events, err := p.repo.ClaimOutbox(p.cfg.BatchSize, now)
	if err != nil {
		return fmt.Errorf("claim outbox: %w", err)
	}
	for _, event := range events {
		if err := p.repo.FanOutEvent(event, now); err != nil {
			dead := event.Attempts >= p.cfg.MaxAttempts
			_ = p.repo.RetryOutbox(event.EventID, event.Attempts, now.Add(p.backoff(event.Attempts)), err.Error(), dead)
			continue
		}
		if err := p.repo.MarkOutboxProcessed(event.EventID, now); err != nil {
			return fmt.Errorf("mark outbox processed: %w", err)
		}
	}

	deliveries, err := p.repo.ClaimDeliveries(p.cfg.BatchSize, now)
	if err != nil {
		return fmt.Errorf("claim deliveries: %w", err)
	}
	for _, delivery := range deliveries {
		status, err := p.deliver(ctx, delivery)
		if err == nil {
			if err := p.repo.MarkDeliveryDelivered(delivery.ID, status, time.Now()); err != nil {
				return fmt.Errorf("mark delivery delivered: %w", err)
			}
			continue
		}
		dead := delivery.Attempts >= p.cfg.MaxAttempts
		if err := p.repo.RetryDelivery(
			delivery.ID,
			delivery.Attempts,
			time.Now().Add(p.backoff(delivery.Attempts)),
			status,
			err.Error(),
			dead,
		); err != nil {
			return fmt.Errorf("retry delivery: %w", err)
		}
	}
	return nil
}

func (p *Processor) deliver(ctx context.Context, delivery model.WebhookDelivery) (int, error) {
	body, err := json.Marshal(delivery.Event)
	if err != nil {
		return 0, err
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := sign(delivery.SigningSecret, timestamp, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "task-manager-webhook/1")
	req.Header.Set("X-Webhook-Event", delivery.Event.EventType)
	req.Header.Set("X-Webhook-Event-ID", delivery.Event.EventID)
	req.Header.Set("X-Webhook-Schema-Version", strconv.Itoa(delivery.Event.SchemaVersion))
	req.Header.Set("X-Webhook-Timestamp", timestamp)
	req.Header.Set("X-Webhook-Signature", "sha256="+signature)
	req.Header.Set("Idempotency-Key", delivery.Event.EventID)

	res, err := p.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return res.StatusCode, fmt.Errorf("webhook returned HTTP %d", res.StatusCode)
	}
	return res.StatusCode, nil
}

func (p *Processor) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 8 {
		shift = 8
	}
	return p.cfg.BaseBackoff * time.Duration(1<<shift)
}

func sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
