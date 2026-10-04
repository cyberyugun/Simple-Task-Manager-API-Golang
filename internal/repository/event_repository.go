package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"go-simple-task-api/internal/model"
)

var (
	ErrWebhookSubscriptionNotFound = errors.New("webhook subscription not found")
	ErrIdempotencyConflict         = errors.New("idempotency key was already used with a different request")
	ErrIdempotencyInProgress       = errors.New("idempotent request is already in progress")
	ErrEventNotFound               = errors.New("event not found")
)

type EventRepository interface {
	ClaimReady(workerID string, limit int, now time.Time) ([]model.DomainEvent, error)
	PendingSubscriptions(event model.DomainEvent) ([]model.WebhookSubscription, error)
	RecordDelivery(delivery model.WebhookDelivery) error
	MarkProcessed(eventID int64, now time.Time) error
	MarkFailed(eventID int64, failure string, now time.Time) (bool, error)
	ReleaseStaleLocks(before time.Time) (int64, error)
	Replay(eventKey string, now time.Time) error

	CreateWebhook(workspaceID, actorUserID int64, url, secret string, eventTypes []string, now time.Time) (model.WebhookSubscription, error)
	ListWebhooks(workspaceID int64) ([]model.WebhookSubscription, error)
	DeleteWebhook(workspaceID, subscriptionID int64) error

	BeginIdempotency(workspaceID int64, key, method, path, requestHash string, now, expiresAt time.Time) (model.IdempotencyRecord, bool, error)
	CompleteIdempotency(workspaceID int64, key string, status int, contentType string, body []byte, now time.Time) error
	AbortIdempotency(workspaceID int64, key string) error
}

type PostgresEventRepository struct {
	db *sql.DB
}

func NewPostgresEventRepository(db *sql.DB) *PostgresEventRepository {
	return &PostgresEventRepository{db: db}
}

func (r *PostgresEventRepository) ClaimReady(workerID string, limit int, now time.Time) ([]model.DomainEvent, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT id, event_key, workspace_id, event_type, aggregate_type, aggregate_id,
		       schema_version, COALESCE(NULLIF(correlation_id,''),event_key), COALESCE(causation_id,''), payload,
		       occurred_at, attempts, max_attempts
		FROM outbox_events
		WHERE processed_at IS NULL
		  AND dead_lettered_at IS NULL
		  AND available_at <= $1
		  AND locked_at IS NULL
		ORDER BY available_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]model.DomainEvent, 0)
	ids := make([]int64, 0)
	for rows.Next() {
		var event model.DomainEvent
		var raw []byte
		if err := rows.Scan(
			&event.ID, &event.EventKey, &event.WorkspaceID, &event.EventType,
			&event.AggregateType, &event.AggregateID, &event.SchemaVersion,
			&event.CorrelationID, &event.CausationID, &raw, &event.OccurredAt, &event.Attempts, &event.MaxAttempts,
		); err != nil {
			return nil, err
		}
		event.Data = append(event.Data[:0], raw...)
		events = append(events, event)
		ids = append(ids, event.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := rows.Close(); err != nil {
		return nil, err
	}

	for _, id := range ids {
		if _, err := tx.Exec(`
			UPDATE outbox_events
			SET locked_at = $2, locked_by = $3
			WHERE id = $1
		`, id, now, workerID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

func (r *PostgresEventRepository) PendingSubscriptions(event model.DomainEvent) ([]model.WebhookSubscription, error) {
	rows, err := r.db.Query(`
		SELECT s.id, s.workspace_id, s.created_by_user_id, s.url, s.signing_secret,
		       s.event_types, s.active, s.created_at, s.updated_at
		FROM webhook_subscriptions s
		LEFT JOIN webhook_deliveries d
		  ON d.subscription_id = s.id
		 AND d.outbox_event_id = $1
		 AND d.status = 'delivered'
		WHERE s.workspace_id = $2
		  AND s.active = TRUE
		  AND (s.event_types ? $3 OR s.event_types ? '*')
		  AND d.id IS NULL
		ORDER BY s.id
	`, event.ID, event.WorkspaceID, event.EventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.WebhookSubscription, 0)
	for rows.Next() {
		item, err := scanWebhook(rows, true)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresEventRepository) RecordDelivery(delivery model.WebhookDelivery) error {
	var deliveredAt any
	if delivery.DeliveredAt != nil {
		deliveredAt = *delivery.DeliveredAt
	}
	_, err := r.db.Exec(`
		INSERT INTO webhook_deliveries (
			outbox_event_id, subscription_id, attempt, status, http_status,
			response_body, error, attempted_at, delivered_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $8)
		ON CONFLICT (outbox_event_id, subscription_id)
		DO UPDATE SET
			attempt = EXCLUDED.attempt,
			status = EXCLUDED.status,
			http_status = EXCLUDED.http_status,
			response_body = EXCLUDED.response_body,
			error = EXCLUDED.error,
			attempted_at = EXCLUDED.attempted_at,
			delivered_at = EXCLUDED.delivered_at,
			updated_at = EXCLUDED.updated_at
	`,
		delivery.EventID, delivery.SubscriptionID, delivery.Attempt, delivery.Status,
		delivery.HTTPStatus, delivery.ResponseBody, delivery.Error, delivery.AttemptedAt, deliveredAt,
	)
	return err
}

func (r *PostgresEventRepository) MarkProcessed(eventID int64, now time.Time) error {
	result, err := r.db.Exec(`
		UPDATE outbox_events
		SET processed_at = $2, locked_at = NULL, locked_by = NULL, last_error = NULL
		WHERE id = $1
	`, eventID, now)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrEventNotFound
	}
	return nil
}

func (r *PostgresEventRepository) MarkFailed(eventID int64, failure string, now time.Time) (bool, error) {
	var deadLettered bool
	err := r.db.QueryRow(`
		UPDATE outbox_events
		SET attempts = attempts + 1,
		    last_error = LEFT($2, 2000),
		    locked_at = NULL,
		    locked_by = NULL,
		    available_at = $3 + (LEAST(POWER(2, attempts), 300)::text || ' seconds')::interval,
		    dead_lettered_at = CASE
		      WHEN attempts + 1 >= max_attempts THEN $3
		      ELSE NULL
		    END
		WHERE id = $1
		RETURNING dead_lettered_at IS NOT NULL
	`, eventID, failure, now).Scan(&deadLettered)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrEventNotFound
	}
	return deadLettered, err
}

func (r *PostgresEventRepository) ReleaseStaleLocks(before time.Time) (int64, error) {
	result, err := r.db.Exec(`
		UPDATE outbox_events
		SET locked_at = NULL, locked_by = NULL
		WHERE processed_at IS NULL
		  AND dead_lettered_at IS NULL
		  AND locked_at < $1
	`, before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *PostgresEventRepository) Replay(eventKey string, now time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var eventID int64
	if err := tx.QueryRow(`
		SELECT id
		FROM outbox_events
		WHERE event_key = $1
		FOR UPDATE
	`, eventKey).Scan(&eventID); errors.Is(err, sql.ErrNoRows) {
		return ErrEventNotFound
	} else if err != nil {
		return err
	}

	if _, err := tx.Exec(`DELETE FROM webhook_deliveries WHERE outbox_event_id = $1`, eventID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE outbox_events
		SET attempts = 0,
		    available_at = $2,
		    locked_at = NULL,
		    locked_by = NULL,
		    processed_at = NULL,
		    dead_lettered_at = NULL,
		    last_error = NULL
		WHERE id = $1
	`, eventID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PostgresEventRepository) CreateWebhook(workspaceID, actorUserID int64, url, secret string, eventTypes []string, now time.Time) (model.WebhookSubscription, error) {
	rawEventTypes, err := json.Marshal(eventTypes)
	if err != nil {
		return model.WebhookSubscription{}, err
	}

	item, err := scanWebhook(r.db.QueryRow(`
		INSERT INTO webhook_subscriptions (
			workspace_id, created_by_user_id, url, signing_secret, event_types, active, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, TRUE, $6, $6)
		RETURNING id, workspace_id, created_by_user_id, url, event_types, active, created_at, updated_at
	`, workspaceID, actorUserID, url, secret, string(rawEventTypes), now), false)
	if err != nil {
		return model.WebhookSubscription{}, err
	}
	item.SigningSecret = secret
	return item, nil
}

func (r *PostgresEventRepository) ListWebhooks(workspaceID int64) ([]model.WebhookSubscription, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, created_by_user_id, url, event_types, active, created_at, updated_at
		FROM webhook_subscriptions
		WHERE workspace_id = $1
		ORDER BY id
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.WebhookSubscription, 0)
	for rows.Next() {
		item, err := scanWebhook(rows, false)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresEventRepository) DeleteWebhook(workspaceID, subscriptionID int64) error {
	result, err := r.db.Exec(`
		DELETE FROM webhook_subscriptions
		WHERE id = $1 AND workspace_id = $2
	`, subscriptionID, workspaceID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrWebhookSubscriptionNotFound
	}
	return nil
}

func (r *PostgresEventRepository) BeginIdempotency(workspaceID int64, key, method, path, requestHash string, now, expiresAt time.Time) (model.IdempotencyRecord, bool, error) {
	if _, err := r.db.Exec(`
		DELETE FROM idempotency_records
		WHERE workspace_id = $1 AND idempotency_key = $2 AND expires_at <= $3
	`, workspaceID, key, now); err != nil {
		return model.IdempotencyRecord{}, false, err
	}

	result, err := r.db.Exec(`
		INSERT INTO idempotency_records (
			workspace_id, idempotency_key, method, path, request_hash,
			state, created_at, updated_at, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, 'pending', $6, $6, $7)
		ON CONFLICT (workspace_id, idempotency_key) DO NOTHING
	`, workspaceID, key, method, path, requestHash, now, expiresAt)
	if err != nil {
		return model.IdempotencyRecord{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return model.IdempotencyRecord{}, false, err
	}
	if rows == 1 {
		return model.IdempotencyRecord{
			WorkspaceID: workspaceID, Key: key, Method: method, Path: path,
			RequestHash: requestHash, State: "pending", ExpiresAt: expiresAt,
		}, true, nil
	}

	var record model.IdempotencyRecord
	var body []byte
	err = r.db.QueryRow(`
		SELECT workspace_id, idempotency_key, method, path, request_hash, state,
		       COALESCE(response_status, 0), response_content_type,
		       COALESCE(response_body, ''::bytea), expires_at
		FROM idempotency_records
		WHERE workspace_id = $1 AND idempotency_key = $2
	`, workspaceID, key).Scan(
		&record.WorkspaceID, &record.Key, &record.Method, &record.Path,
		&record.RequestHash, &record.State, &record.ResponseStatus,
		&record.ResponseContentType, &body, &record.ExpiresAt,
	)
	if err != nil {
		return model.IdempotencyRecord{}, false, err
	}
	record.ResponseBody = body
	if record.Method != method || record.Path != path || record.RequestHash != requestHash {
		return model.IdempotencyRecord{}, false, ErrIdempotencyConflict
	}
	if record.State == "pending" {
		return model.IdempotencyRecord{}, false, ErrIdempotencyInProgress
	}
	return record, false, nil
}

func (r *PostgresEventRepository) CompleteIdempotency(workspaceID int64, key string, status int, contentType string, body []byte, now time.Time) error {
	result, err := r.db.Exec(`
		UPDATE idempotency_records
		SET state = 'completed',
		    response_status = $3,
		    response_content_type = $4,
		    response_body = $5,
		    updated_at = $6
		WHERE workspace_id = $1 AND idempotency_key = $2 AND state = 'pending'
	`, workspaceID, key, status, contentType, body, now)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrIdempotencyInProgress
	}
	return nil
}

func (r *PostgresEventRepository) AbortIdempotency(workspaceID int64, key string) error {
	_, err := r.db.Exec(`
		DELETE FROM idempotency_records
		WHERE workspace_id = $1 AND idempotency_key = $2 AND state = 'pending'
	`, workspaceID, key)
	return err
}

type webhookScanner interface {
	Scan(dest ...any) error
}

func scanWebhook(scanner webhookScanner, includeSecret bool) (model.WebhookSubscription, error) {
	var item model.WebhookSubscription
	var creator sql.NullInt64
	var rawEventTypes []byte

	var err error
	if includeSecret {
		err = scanner.Scan(
			&item.ID, &item.WorkspaceID, &creator, &item.URL, &item.SigningSecret,
			&rawEventTypes, &item.Active, &item.CreatedAt, &item.UpdatedAt,
		)
	} else {
		err = scanner.Scan(
			&item.ID, &item.WorkspaceID, &creator, &item.URL,
			&rawEventTypes, &item.Active, &item.CreatedAt, &item.UpdatedAt,
		)
	}
	if err != nil {
		return model.WebhookSubscription{}, err
	}
	if creator.Valid {
		creatorID := creator.Int64
		item.CreatedByUserID = &creatorID
	}
	if err := json.Unmarshal(rawEventTypes, &item.EventTypes); err != nil {
		return model.WebhookSubscription{}, err
	}
	return item, nil
}

func insertOutbox(tx *sql.Tx, workspaceID int64, eventType, aggregateType, aggregateID string, payload any, occurredAt time.Time) error {
	eventKey, err := randomEventKey()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO outbox_events (
			event_key, workspace_id, event_type, aggregate_type, aggregate_id,
			schema_version, correlation_id, causation_id, payload, occurred_at, available_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, 1, $1, '', $6::jsonb, $7, $7, $7)
	`, eventKey, workspaceID, eventType, aggregateType, aggregateID, string(raw), occurredAt)
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
