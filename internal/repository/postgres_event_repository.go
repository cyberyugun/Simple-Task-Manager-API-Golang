package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresEventRepository struct {
	db *sql.DB
}

func NewPostgresEventRepository(db *sql.DB) *PostgresEventRepository {
	return &PostgresEventRepository{db: db}
}

func (r *PostgresEventRepository) CreateSubscription(workspaceID, actorID int64, url string, eventTypes []string, now time.Time) (model.WebhookSubscription, error) {
	raw, err := marshalEventTypes(eventTypes)
	if err != nil {
		return model.WebhookSubscription{}, err
	}
	var sub model.WebhookSubscription
	var rawTypes []byte
	err = r.db.QueryRow(`
		INSERT INTO webhook_subscriptions (
			workspace_id, url, event_types, active, created_by_user_id, created_at, updated_at
		)
		VALUES ($1, $2, $3::jsonb, TRUE, $4, $5, $5)
		RETURNING id, workspace_id, url, event_types, active, created_by_user_id, created_at, updated_at
	`, workspaceID, url, string(raw), actorID, now).Scan(
		&sub.ID,
		&sub.WorkspaceID,
		&sub.URL,
		&rawTypes,
		&sub.Active,
		&sub.CreatedByUserID,
		&sub.CreatedAt,
		&sub.UpdatedAt,
	)
	if err != nil {
		return model.WebhookSubscription{}, err
	}
	if err := json.Unmarshal(rawTypes, &sub.EventTypes); err != nil {
		return model.WebhookSubscription{}, err
	}
	return sub, nil
}

func (r *PostgresEventRepository) ListSubscriptions(workspaceID int64) ([]model.WebhookSubscription, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, url, event_types, active, created_by_user_id, created_at, updated_at
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
		var sub model.WebhookSubscription
		var rawTypes []byte
		if err := rows.Scan(
			&sub.ID,
			&sub.WorkspaceID,
			&sub.URL,
			&rawTypes,
			&sub.Active,
			&sub.CreatedByUserID,
			&sub.CreatedAt,
			&sub.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rawTypes, &sub.EventTypes); err != nil {
			return nil, err
		}
		items = append(items, sub)
	}
	return items, rows.Err()
}

func (r *PostgresEventRepository) DeleteSubscription(workspaceID, subscriptionID int64) error {
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

func (r *PostgresEventRepository) ListDeliveries(workspaceID, subscriptionID int64, limit int) ([]model.WebhookDelivery, error) {
	var exists bool
	if err := r.db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM webhook_subscriptions
			WHERE id = $1 AND workspace_id = $2
		)
	`, subscriptionID, workspaceID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrWebhookSubscriptionNotFound
	}

	rows, err := r.db.Query(`
		SELECT d.id, d.subscription_id, d.event_id, e.event_type,
		       d.attempt_count, d.max_attempts, d.next_attempt_at,
		       d.delivered_at, d.dead_lettered_at, d.last_status, COALESCE(d.last_error, ''),
		       d.created_at, d.updated_at
		FROM webhook_deliveries d
		JOIN outbox_events e ON e.event_id = d.event_id
		WHERE d.subscription_id = $1
		ORDER BY d.id DESC
		LIMIT $2
	`, subscriptionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.WebhookDelivery, 0)
	for rows.Next() {
		var item model.WebhookDelivery
		if err := rows.Scan(
			&item.ID,
			&item.SubscriptionID,
			&item.EventID,
			&item.EventType,
			&item.AttemptCount,
			&item.MaxAttempts,
			&item.NextAttemptAt,
			&item.DeliveredAt,
			&item.DeadLetteredAt,
			&item.LastStatus,
			&item.LastError,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresEventRepository) ReplayDelivery(workspaceID, subscriptionID, deliveryID int64, now time.Time) error {
	result, err := r.db.Exec(`
		UPDATE webhook_deliveries d
		SET attempt_count = 0,
		    next_attempt_at = $4,
		    locked_at = NULL,
		    locked_by = NULL,
		    delivered_at = NULL,
		    dead_lettered_at = NULL,
		    last_status = NULL,
		    last_error = NULL,
		    updated_at = $4
		FROM webhook_subscriptions s
		WHERE d.id = $1
		  AND d.subscription_id = $2
		  AND s.id = d.subscription_id
		  AND s.workspace_id = $3
	`, deliveryID, subscriptionID, workspaceID, now)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrWebhookDeliveryNotFound
	}
	return nil
}

func (r *PostgresEventRepository) FanoutOutbox(limit int, now time.Time) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT event_id, workspace_id, event_type
		FROM outbox_events
		WHERE published_at IS NULL
		  AND available_at <= $1
		ORDER BY id
		FOR UPDATE SKIP LOCKED
		LIMIT $2
	`, now, limit)
	if err != nil {
		return 0, err
	}

	type pending struct {
		eventID     string
		workspaceID int64
		eventType   string
	}
	events := make([]pending, 0)
	for rows.Next() {
		var item pending
		if err := rows.Scan(&item.eventID, &item.workspaceID, &item.eventType); err != nil {
			rows.Close()
			return 0, err
		}
		events = append(events, item)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, event := range events {
		if _, err := tx.Exec(`
			INSERT INTO webhook_deliveries (
				subscription_id, event_id, attempt_count, max_attempts,
				next_attempt_at, created_at, updated_at
			)
			SELECT s.id, $1, 0, 8, $4, $4, $4
			FROM webhook_subscriptions s
			WHERE s.workspace_id = $2
			  AND s.active = TRUE
			  AND (
			    s.event_types = '[]'::jsonb
			    OR s.event_types ? $3
			  )
			ON CONFLICT (subscription_id, event_id) DO NOTHING
		`, event.eventID, event.workspaceID, event.eventType, now); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`
			UPDATE outbox_events
			SET published_at = $2, locked_at = NULL, locked_by = NULL, last_error = NULL
			WHERE event_id = $1
		`, event.eventID, now); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(events), nil
}

func (r *PostgresEventRepository) ClaimDeliveries(workerID string, limit int, lockTTL time.Duration, now time.Time) ([]model.WebhookDelivery, error) {
	lockBefore := now.Add(-lockTTL)
	rows, err := r.db.Query(`
		WITH candidates AS (
			SELECT d.id
			FROM webhook_deliveries d
			WHERE d.delivered_at IS NULL
			  AND d.dead_lettered_at IS NULL
			  AND d.next_attempt_at <= $1
			  AND (d.locked_at IS NULL OR d.locked_at < $2)
			ORDER BY d.next_attempt_at, d.id
			FOR UPDATE SKIP LOCKED
			LIMIT $3
		)
		UPDATE webhook_deliveries d
		SET attempt_count = d.attempt_count + 1,
		    locked_at = $1,
		    locked_by = $4,
		    updated_at = $1
		FROM candidates c, webhook_subscriptions s, outbox_events e
		WHERE d.id = c.id
		  AND s.id = d.subscription_id
		  AND e.event_id = d.event_id
		RETURNING d.id, d.subscription_id, d.event_id,
		          d.attempt_count, d.max_attempts, d.next_attempt_at,
		          d.delivered_at, d.dead_lettered_at, d.last_status, COALESCE(d.last_error, ''),
		          d.created_at, d.updated_at,
		          s.url, s.workspace_id,
		          e.event_type, e.aggregate_type, e.aggregate_id, e.schema_version,
		          e.payload, e.occurred_at
	`, now, lockBefore, limit, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.WebhookDelivery, 0)
	for rows.Next() {
		var item model.WebhookDelivery
		var rawPayload []byte
		if err := rows.Scan(
			&item.ID,
			&item.SubscriptionID,
			&item.EventID,
			&item.AttemptCount,
			&item.MaxAttempts,
			&item.NextAttemptAt,
			&item.DeliveredAt,
			&item.DeadLetteredAt,
			&item.LastStatus,
			&item.LastError,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.URL,
			&item.WorkspaceID,
			&item.EventType,
			&item.AggregateType,
			&item.AggregateID,
			&item.SchemaVersion,
			&rawPayload,
			&item.OccurredAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rawPayload, &item.Payload); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresEventRepository) MarkDeliverySuccess(deliveryID int64, workerID string, status int, deliveredAt time.Time) error {
	result, err := r.db.Exec(`
		UPDATE webhook_deliveries
		SET delivered_at = $3,
		    last_status = $4,
		    last_error = NULL,
		    locked_at = NULL,
		    locked_by = NULL,
		    updated_at = $3
		WHERE id = $1 AND locked_by = $2
	`, deliveryID, workerID, deliveredAt, status)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrWebhookDeliveryNotFound
	}
	return nil
}

func (r *PostgresEventRepository) MarkDeliveryFailure(deliveryID int64, workerID string, status int, lastError string, nextAttemptAt time.Time, deadLetter bool, now time.Time) error {
	var deadAt any
	if deadLetter {
		deadAt = now
	}
	var statusValue any
	if status > 0 {
		statusValue = status
	}
	result, err := r.db.Exec(`
		UPDATE webhook_deliveries
		SET next_attempt_at = $3,
		    dead_lettered_at = $4,
		    last_status = $5,
		    last_error = $6,
		    locked_at = NULL,
		    locked_by = NULL,
		    updated_at = $7
		WHERE id = $1 AND locked_by = $2
	`, deliveryID, workerID, nextAttemptAt, deadAt, statusValue, lastError, now)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrWebhookDeliveryNotFound
	}
	return nil
}

var _ EventRepository = (*PostgresEventRepository)(nil)
var _ EventRepository = (*InMemoryEventRepository)(nil)

func scanOptionalStatus(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	status := int(value.Int64)
	return &status
}

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
