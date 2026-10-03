package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrWebhookSubscriptionNotFound = errors.New("webhook subscription not found")
	ErrOutboxEventNotFound          = errors.New("outbox event not found")
)

type EventRepository interface {
	CreateSubscription(subscription model.WebhookSubscription, secret string) (model.WebhookSubscription, error)
	ListSubscriptions(workspaceID int64) ([]model.WebhookSubscription, error)
	DeleteSubscription(workspaceID, subscriptionID int64) error
	ClaimOutbox(limit int, now time.Time) ([]model.DomainEvent, error)
	FanOutEvent(event model.DomainEvent, now time.Time) error
	MarkOutboxProcessed(eventID string, now time.Time) error
	RetryOutbox(eventID string, attempts int, availableAt time.Time, lastError string, dead bool) error
	ClaimDeliveries(limit int, now time.Time) ([]model.WebhookDelivery, error)
	MarkDeliveryDelivered(id int64, status int, now time.Time) error
	RetryDelivery(id int64, attempts int, availableAt time.Time, status int, lastError string, dead bool) error
	ReplayDead(workspaceID int64, now time.Time) (int64, error)
	Stats(workspaceID int64) (model.OutboxStats, error)
}

type PostgresEventRepository struct {
	db *sql.DB
}

func NewPostgresEventRepository(db *sql.DB) *PostgresEventRepository {
	return &PostgresEventRepository{db: db}
}

func (r *PostgresEventRepository) CreateSubscription(subscription model.WebhookSubscription, secret string) (model.WebhookSubscription, error) {
	eventTypes, err := json.Marshal(subscription.EventTypes)
	if err != nil {
		return model.WebhookSubscription{}, err
	}
	var raw []byte
	err = r.db.QueryRow(`
		INSERT INTO webhook_subscriptions (
			workspace_id, url, signing_secret, event_types, active,
			created_by_user_id, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4::jsonb, TRUE, $5, $6, $6)
		RETURNING id, workspace_id, url, event_types, active,
		          created_by_user_id, created_at, updated_at
	`,
		subscription.WorkspaceID,
		subscription.URL,
		secret,
		string(eventTypes),
		subscription.CreatedByUserID,
		subscription.CreatedAt,
	).Scan(
		&subscription.ID,
		&subscription.WorkspaceID,
		&subscription.URL,
		&raw,
		&subscription.Active,
		&subscription.CreatedByUserID,
		&subscription.CreatedAt,
		&subscription.UpdatedAt,
	)
	if err != nil {
		return model.WebhookSubscription{}, err
	}
	if err := json.Unmarshal(raw, &subscription.EventTypes); err != nil {
		return model.WebhookSubscription{}, err
	}
	return subscription, nil
}

func (r *PostgresEventRepository) ListSubscriptions(workspaceID int64) ([]model.WebhookSubscription, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, url, event_types, active,
		       created_by_user_id, created_at, updated_at
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
		var item model.WebhookSubscription
		var raw []byte
		if err := rows.Scan(
			&item.ID,
			&item.WorkspaceID,
			&item.URL,
			&raw,
			&item.Active,
			&item.CreatedByUserID,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.EventTypes); err != nil {
			return nil, err
		}
		items = append(items, item)
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
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrWebhookSubscriptionNotFound
	}
	return nil
}

func (r *PostgresEventRepository) ClaimOutbox(limit int, now time.Time) ([]model.DomainEvent, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT id, event_id, workspace_id, aggregate_type, aggregate_id,
		       event_type, schema_version, payload, status, attempts,
		       available_at, created_at
		FROM outbox_events
		WHERE status = 'pending'
		  AND available_at <= $1
		ORDER BY id
		FOR UPDATE SKIP LOCKED
		LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.DomainEvent, 0)
	for rows.Next() {
		item, err := scanDomainEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, item := range items {
		if _, err := tx.Exec(`
			UPDATE outbox_events
			SET status = 'processing', locked_at = $2, attempts = attempts + 1
			WHERE id = $1
		`, item.ID, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Status = model.OutboxStatusProcessing
		items[i].Attempts++
	}
	return items, nil
}

func (r *PostgresEventRepository) FanOutEvent(event model.DomainEvent, now time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT id, event_types
		FROM webhook_subscriptions
		WHERE workspace_id = $1 AND active = TRUE
	`, event.WorkspaceID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type subscription struct {
		id         int64
		eventTypes []string
	}
	subs := make([]subscription, 0)
	for rows.Next() {
		var sub subscription
		var raw []byte
		if err := rows.Scan(&sub.id, &raw); err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &sub.eventTypes); err != nil {
			return err
		}
		if matchesEvent(sub.eventTypes, event.EventType) {
			subs = append(subs, sub)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, sub := range subs {
		if _, err := tx.Exec(`
			INSERT INTO webhook_deliveries (
				subscription_id, event_id, status, attempts, available_at, created_at
			)
			VALUES ($1, $2, 'pending', 0, $3, $3)
			ON CONFLICT (subscription_id, event_id) DO NOTHING
		`, sub.id, event.EventID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *PostgresEventRepository) MarkOutboxProcessed(eventID string, now time.Time) error {
	result, err := r.db.Exec(`
		UPDATE outbox_events
		SET status = 'processed', processed_at = $2, locked_at = NULL, last_error = ''
		WHERE event_id = $1
	`, eventID, now)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrOutboxEventNotFound
	}
	return nil
}

func (r *PostgresEventRepository) RetryOutbox(eventID string, attempts int, availableAt time.Time, lastError string, dead bool) error {
	status := model.OutboxStatusPending
	if dead {
		status = model.OutboxStatusDead
	}
	_, err := r.db.Exec(`
		UPDATE outbox_events
		SET status = $2, available_at = $3, locked_at = NULL, last_error = $4
		WHERE event_id = $1
	`, eventID, status, availableAt, truncateError(lastError))
	return err
}

func (r *PostgresEventRepository) ClaimDeliveries(limit int, now time.Time) ([]model.WebhookDelivery, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT d.id, d.subscription_id, d.event_id, s.url, s.signing_secret,
		       s.event_types, e.id, e.event_id, e.workspace_id, e.aggregate_type,
		       e.aggregate_id, e.event_type, e.schema_version, e.payload,
		       e.status, e.attempts, e.available_at, e.created_at, d.attempts
		FROM webhook_deliveries d
		JOIN webhook_subscriptions s ON s.id = d.subscription_id
		JOIN outbox_events e ON e.event_id = d.event_id
		WHERE d.status = 'pending'
		  AND d.available_at <= $1
		  AND s.active = TRUE
		ORDER BY d.id
		FOR UPDATE OF d SKIP LOCKED
		LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.WebhookDelivery, 0)
	for rows.Next() {
		var item model.WebhookDelivery
		var eventTypesRaw []byte
		var payloadRaw []byte
		if err := rows.Scan(
			&item.ID,
			&item.SubscriptionID,
			&item.EventID,
			&item.URL,
			&item.SigningSecret,
			&eventTypesRaw,
			&item.Event.ID,
			&item.Event.EventID,
			&item.Event.WorkspaceID,
			&item.Event.AggregateType,
			&item.Event.AggregateID,
			&item.Event.EventType,
			&item.Event.SchemaVersion,
			&payloadRaw,
			&item.Event.Status,
			&item.Event.Attempts,
			&item.Event.AvailableAt,
			&item.Event.CreatedAt,
			&item.Attempts,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(eventTypesRaw, &item.EventTypes); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payloadRaw, &item.Event.Payload); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, item := range items {
		if _, err := tx.Exec(`
			UPDATE webhook_deliveries
			SET status = 'processing', locked_at = $2, attempts = attempts + 1
			WHERE id = $1
		`, item.ID, now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Attempts++
	}
	return items, nil
}

func (r *PostgresEventRepository) MarkDeliveryDelivered(id int64, status int, now time.Time) error {
	_, err := r.db.Exec(`
		UPDATE webhook_deliveries
		SET status = 'delivered', delivered_at = $2, response_status = $3,
		    locked_at = NULL, last_error = ''
		WHERE id = $1
	`, id, now, status)
	return err
}

func (r *PostgresEventRepository) RetryDelivery(id int64, attempts int, availableAt time.Time, status int, lastError string, dead bool) error {
	state := model.DeliveryStatusPending
	if dead {
		state = model.DeliveryStatusDead
	}
	_, err := r.db.Exec(`
		UPDATE webhook_deliveries
		SET status = $2, available_at = $3, response_status = NULLIF($4, 0),
		    locked_at = NULL, last_error = $5
		WHERE id = $1
	`, id, state, availableAt, status, truncateError(lastError))
	return err
}

func (r *PostgresEventRepository) ReplayDead(workspaceID int64, now time.Time) (int64, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		UPDATE outbox_events
		SET status = 'pending', attempts = 0, available_at = $2,
		    locked_at = NULL, processed_at = NULL, last_error = ''
		WHERE workspace_id = $1 AND status = 'dead'
	`, workspaceID, now)
	if err != nil {
		return 0, err
	}
	outboxCount, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	result, err = tx.Exec(`
		UPDATE webhook_deliveries d
		SET status = 'pending', attempts = 0, available_at = $2,
		    locked_at = NULL, delivered_at = NULL, response_status = NULL, last_error = ''
		FROM outbox_events e
		WHERE d.event_id = e.event_id
		  AND e.workspace_id = $1
		  AND d.status = 'dead'
	`, workspaceID, now)
	if err != nil {
		return 0, err
	}
	deliveryCount, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return outboxCount + deliveryCount, nil
}

func (r *PostgresEventRepository) Stats(workspaceID int64) (model.OutboxStats, error) {
	var stats model.OutboxStats
	err := r.db.QueryRow(`
		SELECT
		  COUNT(*) FILTER (WHERE status = 'pending'),
		  COUNT(*) FILTER (WHERE status = 'dead')
		FROM outbox_events
		WHERE workspace_id = $1
	`, workspaceID).Scan(&stats.PendingEvents, &stats.DeadEvents)
	if err != nil {
		return model.OutboxStats{}, err
	}
	err = r.db.QueryRow(`
		SELECT
		  COUNT(*) FILTER (WHERE d.status = 'pending'),
		  COUNT(*) FILTER (WHERE d.status = 'dead')
		FROM webhook_deliveries d
		JOIN outbox_events e ON e.event_id = d.event_id
		WHERE e.workspace_id = $1
	`, workspaceID).Scan(&stats.PendingDeliveries, &stats.DeadDeliveries)
	return stats, err
}

type sqlScanner interface {
	Scan(dest ...any) error
}

func scanDomainEvent(scanner sqlScanner) (model.DomainEvent, error) {
	var event model.DomainEvent
	var raw []byte
	err := scanner.Scan(
		&event.ID,
		&event.EventID,
		&event.WorkspaceID,
		&event.AggregateType,
		&event.AggregateID,
		&event.EventType,
		&event.SchemaVersion,
		&raw,
		&event.Status,
		&event.Attempts,
		&event.AvailableAt,
		&event.CreatedAt,
	)
	if err != nil {
		return model.DomainEvent{}, err
	}
	if err := json.Unmarshal(raw, &event.Payload); err != nil {
		return model.DomainEvent{}, err
	}
	return event, nil
}

func matchesEvent(patterns []string, eventType string) bool {
	for _, pattern := range patterns {
		if pattern == "*" || pattern == eventType {
			return true
		}
		if len(pattern) > 1 && pattern[len(pattern)-1] == '*' {
			prefix := pattern[:len(pattern)-1]
			if len(eventType) >= len(prefix) && eventType[:len(prefix)] == prefix {
				return true
			}
		}
	}
	return false
}

func truncateError(value string) string {
	const max = 2000
	if len(value) > max {
		return value[:max]
	}
	return value
}
