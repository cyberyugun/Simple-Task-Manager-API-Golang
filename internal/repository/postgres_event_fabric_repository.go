package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresEventFabricRepository struct {
	db *sql.DB
}

func NewPostgresEventFabricRepository(db *sql.DB) *PostgresEventFabricRepository {
	return &PostgresEventFabricRepository{db: db}
}

func (r *PostgresEventFabricRepository) CreateSchema(item model.EventSchemaVersion) (model.EventSchemaVersion, error) {
	raw, err := json.Marshal(item.Schema)
	if err != nil {
		return model.EventSchemaVersion{}, err
	}
	var schemaRaw []byte
	err = r.db.QueryRow(`
		INSERT INTO event_schema_versions (
			workspace_id,event_type,version,compatibility,status,owner_name,description,schema,
			created_by_user_id,created_at,deprecated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11)
		RETURNING id,workspace_id,event_type,version,compatibility,status,owner_name,description,schema,
			created_by_user_id,created_at,deprecated_at
	`, item.WorkspaceID, item.EventType, item.Version, item.Compatibility, item.Status, item.Owner,
		item.Description, string(raw), item.CreatedByUserID, item.CreatedAt, item.DeprecatedAt).Scan(
		&item.ID, &item.WorkspaceID, &item.EventType, &item.Version, &item.Compatibility, &item.Status,
		&item.Owner, &item.Description, &schemaRaw, &item.CreatedByUserID, &item.CreatedAt, &item.DeprecatedAt,
	)
	if err != nil {
		return model.EventSchemaVersion{}, err
	}
	if err := json.Unmarshal(schemaRaw, &item.Schema); err != nil {
		return model.EventSchemaVersion{}, err
	}
	return item, nil
}

func (r *PostgresEventFabricRepository) ListSchemas(workspaceID int64, eventType string) ([]model.EventSchemaVersion, error) {
	rows, err := r.db.Query(`
		SELECT id,workspace_id,event_type,version,compatibility,status,owner_name,description,schema,
			created_by_user_id,created_at,deprecated_at
		FROM event_schema_versions
		WHERE workspace_id=$1 AND ($2='' OR event_type=$2)
		ORDER BY event_type,version DESC
	`, workspaceID, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.EventSchemaVersion, 0)
	for rows.Next() {
		item, err := scanEventSchema(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresEventFabricRepository) LatestSchema(workspaceID int64, eventType string) (model.EventSchemaVersion, error) {
	item, err := scanEventSchema(r.db.QueryRow(`
		SELECT id,workspace_id,event_type,version,compatibility,status,owner_name,description,schema,
			created_by_user_id,created_at,deprecated_at
		FROM event_schema_versions
		WHERE workspace_id=$1 AND event_type=$2 AND status='active'
		ORDER BY version DESC LIMIT 1
	`, workspaceID, eventType))
	if errors.Is(err, sql.ErrNoRows) {
		return model.EventSchemaVersion{}, ErrEventSchemaNotFound
	}
	return item, err
}

func (r *PostgresEventFabricRepository) DeprecateSchema(workspaceID, schemaID int64, now time.Time) error {
	res, err := r.db.Exec(`
		UPDATE event_schema_versions
		SET status='deprecated', deprecated_at=$3
		WHERE workspace_id=$1 AND id=$2
	`, workspaceID, schemaID, now)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrEventSchemaNotFound
	}
	return nil
}

func (r *PostgresEventFabricRepository) CreateSubscription(item model.EventFabricSubscription) (model.EventFabricSubscription, error) {
	raw, err := json.Marshal(item.EventTypes)
	if err != nil {
		return model.EventFabricSubscription{}, err
	}
	var eventTypesRaw []byte
	err = r.db.QueryRow(`
		INSERT INTO event_fabric_subscriptions (
			workspace_id,name,consumer_key,event_types,adapter,status,max_attempts,retention_days,
			created_by_user_id,created_at,updated_at
		) VALUES ($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id,workspace_id,name,consumer_key,event_types,adapter,status,max_attempts,retention_days,
			created_by_user_id,created_at,updated_at
	`, item.WorkspaceID, item.Name, item.ConsumerKey, string(raw), item.Adapter, item.Status, item.MaxAttempts,
		item.RetentionDays, item.CreatedByUserID, item.CreatedAt, item.UpdatedAt).Scan(
		&item.ID, &item.WorkspaceID, &item.Name, &item.ConsumerKey, &eventTypesRaw, &item.Adapter, &item.Status,
		&item.MaxAttempts, &item.RetentionDays, &item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.EventFabricSubscription{}, err
	}
	if err := json.Unmarshal(eventTypesRaw, &item.EventTypes); err != nil {
		return model.EventFabricSubscription{}, err
	}
	return item, nil
}

func (r *PostgresEventFabricRepository) ListSubscriptions(workspaceID int64) ([]model.EventFabricSubscription, error) {
	rows, err := r.db.Query(`
		SELECT id,workspace_id,name,consumer_key,event_types,adapter,status,max_attempts,retention_days,
			created_by_user_id,created_at,updated_at
		FROM event_fabric_subscriptions
		WHERE workspace_id=$1
		ORDER BY id
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.EventFabricSubscription, 0)
	for rows.Next() {
		item, err := scanEventFabricSubscription(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresEventFabricRepository) GetSubscription(workspaceID, subscriptionID int64) (model.EventFabricSubscription, error) {
	item, err := scanEventFabricSubscription(r.db.QueryRow(`
		SELECT id,workspace_id,name,consumer_key,event_types,adapter,status,max_attempts,retention_days,
			created_by_user_id,created_at,updated_at
		FROM event_fabric_subscriptions
		WHERE workspace_id=$1 AND id=$2
	`, workspaceID, subscriptionID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.EventFabricSubscription{}, ErrEventFabricSubscriptionNotFound
	}
	return item, err
}

func (r *PostgresEventFabricRepository) UpdateSubscriptionStatus(workspaceID, subscriptionID int64, status string, now time.Time) (model.EventFabricSubscription, error) {
	var raw []byte
	var item model.EventFabricSubscription
	err := r.db.QueryRow(`
		UPDATE event_fabric_subscriptions
		SET status=$3,updated_at=$4
		WHERE workspace_id=$1 AND id=$2
		RETURNING id,workspace_id,name,consumer_key,event_types,adapter,status,max_attempts,retention_days,
			created_by_user_id,created_at,updated_at
	`, workspaceID, subscriptionID, status, now).Scan(
		&item.ID, &item.WorkspaceID, &item.Name, &item.ConsumerKey, &raw, &item.Adapter, &item.Status,
		&item.MaxAttempts, &item.RetentionDays, &item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.EventFabricSubscription{}, ErrEventFabricSubscriptionNotFound
	}
	if err != nil {
		return model.EventFabricSubscription{}, err
	}
	if err := json.Unmarshal(raw, &item.EventTypes); err != nil {
		return model.EventFabricSubscription{}, err
	}
	return item, nil
}

func (r *PostgresEventFabricRepository) CreateRoute(item model.EventRoute) (model.EventRoute, error) {
	err := r.db.QueryRow(`
		INSERT INTO event_routes (
			workspace_id,name,event_pattern,subscription_id,active,created_by_user_id,created_at,updated_at
		)
		SELECT $1,$2,$3,$4,$5,$6,$7,$8
		WHERE EXISTS (
			SELECT 1 FROM event_fabric_subscriptions WHERE id=$4 AND workspace_id=$1
		)
		RETURNING id,workspace_id,name,event_pattern,subscription_id,active,created_by_user_id,created_at,updated_at
	`, item.WorkspaceID, item.Name, item.EventPattern, item.SubscriptionID, item.Active,
		item.CreatedByUserID, item.CreatedAt, item.UpdatedAt).Scan(
		&item.ID, &item.WorkspaceID, &item.Name, &item.EventPattern, &item.SubscriptionID, &item.Active,
		&item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.EventRoute{}, ErrEventFabricSubscriptionNotFound
	}
	return item, err
}

func (r *PostgresEventFabricRepository) ListRoutes(workspaceID int64) ([]model.EventRoute, error) {
	rows, err := r.db.Query(`
		SELECT id,workspace_id,name,event_pattern,subscription_id,active,created_by_user_id,created_at,updated_at
		FROM event_routes WHERE workspace_id=$1 ORDER BY id
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.EventRoute, 0)
	for rows.Next() {
		var item model.EventRoute
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.EventPattern, &item.SubscriptionID,
			&item.Active, &item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresEventFabricRepository) MatchingRoutes(workspaceID int64, eventType string) ([]model.EventRoute, error) {
	rows, err := r.db.Query(`
		SELECT id,workspace_id,name,event_pattern,subscription_id,active,created_by_user_id,created_at,updated_at
		FROM event_routes
		WHERE workspace_id=$1 AND active=TRUE
		  AND (
			event_pattern='*' OR event_pattern=$2 OR
			(right(event_pattern,2)='.*' AND $2 LIKE left(event_pattern,length(event_pattern)-1) || '%')
		  )
		ORDER BY id
	`, workspaceID, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.EventRoute, 0)
	for rows.Next() {
		var item model.EventRoute
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.EventPattern, &item.SubscriptionID,
			&item.Active, &item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresEventFabricRepository) CreateDelivery(item model.EventFabricDelivery) (model.EventFabricDelivery, bool, error) {
	raw, err := json.Marshal(item.Payload)
	if err != nil {
		return model.EventFabricDelivery{}, false, err
	}
	var payloadRaw []byte
	err = r.db.QueryRow(`
		INSERT INTO event_fabric_deliveries (
			workspace_id,subscription_id,outbox_event_id,event_key,event_type,schema_version,payload,
			correlation_id,causation_id,occurred_at,status,attempts,max_attempts,available_at,locked_at,locked_by,
			last_error,delivered_at,dead_lettered_at,created_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		ON CONFLICT (subscription_id,outbox_event_id) DO NOTHING
		RETURNING id,workspace_id,subscription_id,outbox_event_id,event_key,event_type,schema_version,payload,
			correlation_id,causation_id,status,attempts,max_attempts,available_at,locked_at,locked_by,
			last_error,delivered_at,dead_lettered_at,created_at,updated_at
	`, item.WorkspaceID, item.SubscriptionID, item.OutboxEventID, item.EventKey, item.EventType,
		item.SchemaVersion, string(raw), item.CorrelationID, item.CausationID, item.OccurredAt, item.Status, item.Attempts,
		item.MaxAttempts, item.AvailableAt, item.LockedAt, item.LockedBy, item.LastError, item.DeliveredAt,
		item.DeadLetteredAt, item.CreatedAt, item.UpdatedAt).Scan(
		&item.ID, &item.WorkspaceID, &item.SubscriptionID, &item.OutboxEventID, &item.EventKey, &item.EventType,
		&item.SchemaVersion, &payloadRaw, &item.CorrelationID, &item.CausationID, &item.OccurredAt, &item.Status, &item.Attempts,
		&item.MaxAttempts, &item.AvailableAt, &item.LockedAt, &item.LockedBy, &item.LastError, &item.DeliveredAt,
		&item.DeadLetteredAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		existing, getErr := r.getDeliveryBySubscriptionEvent(item.SubscriptionID, item.OutboxEventID)
		return existing, false, getErr
	}
	if err != nil {
		return model.EventFabricDelivery{}, false, err
	}
	if err := json.Unmarshal(payloadRaw, &item.Payload); err != nil {
		return model.EventFabricDelivery{}, false, err
	}
	return item, true, nil
}

func (r *PostgresEventFabricRepository) ClaimDeliveries(workerID string, limit int, now time.Time) ([]model.EventFabricDelivery, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`
		SELECT d.id,d.workspace_id,d.subscription_id,d.outbox_event_id,d.event_key,d.event_type,
			d.schema_version,d.payload,d.correlation_id,d.causation_id,d.occurred_at,d.status,d.attempts,d.max_attempts,
			d.available_at,d.locked_at,d.locked_by,d.last_error,d.delivered_at,d.dead_lettered_at,d.created_at,d.updated_at
		FROM event_fabric_deliveries d
		JOIN event_fabric_subscriptions s ON s.id=d.subscription_id
		WHERE d.status IN ('pending','retry') AND d.dead_lettered_at IS NULL AND d.locked_at IS NULL
		  AND d.available_at <= $1 AND s.status='active'
		ORDER BY d.available_at,d.id
		FOR UPDATE OF d SKIP LOCKED
		LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, err
	}
	items := make([]model.EventFabricDelivery, 0)
	ids := make([]int64, 0)
	for rows.Next() {
		item, err := scanEventFabricDelivery(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
		ids = append(ids, item.ID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE event_fabric_deliveries SET locked_at=$2,locked_by=$3 WHERE id=$1`, id, now, workerID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for i := range items {
		t := now
		items[i].LockedAt = &t
		items[i].LockedBy = workerID
	}
	return items, nil
}

func (r *PostgresEventFabricRepository) MarkDeliverySucceeded(deliveryID int64, now time.Time) error {
	res, err := r.db.Exec(`
		UPDATE event_fabric_deliveries
		SET status='delivered',delivered_at=$2,locked_at=NULL,locked_by='',last_error='',updated_at=$2
		WHERE id=$1
	`, deliveryID, now)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrEventFabricDeliveryNotFound
	}
	return nil
}

func (r *PostgresEventFabricRepository) MarkDeliveryFailed(deliveryID int64, failure string, now time.Time) (bool, error) {
	var dead bool
	err := r.db.QueryRow(`
		UPDATE event_fabric_deliveries
		SET attempts=attempts+1,
			last_error=LEFT($2,2000),
			locked_at=NULL,
			locked_by='',
			status=CASE WHEN attempts+1>=max_attempts THEN 'dead_letter' ELSE 'retry' END,
			dead_lettered_at=CASE WHEN attempts+1>=max_attempts THEN $3 ELSE NULL END,
			available_at=$3 + (LEAST(POWER(2,attempts),300)::text || ' seconds')::interval,
			updated_at=$3
		WHERE id=$1
		RETURNING status='dead_letter'
	`, deliveryID, failure, now).Scan(&dead)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrEventFabricDeliveryNotFound
	}
	return dead, err
}

func (r *PostgresEventFabricRepository) ReleaseDeliveryLocks(before time.Time) (int64, error) {
	res, err := r.db.Exec(`
		UPDATE event_fabric_deliveries
		SET locked_at=NULL,locked_by=''
		WHERE status IN ('pending','retry') AND locked_at IS NOT NULL AND locked_at < $1
	`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *PostgresEventFabricRepository) GetDelivery(workspaceID, deliveryID int64) (model.EventFabricDelivery, error) {
	item, err := scanEventFabricDelivery(r.db.QueryRow(`
		SELECT id,workspace_id,subscription_id,outbox_event_id,event_key,event_type,schema_version,payload,
			correlation_id,causation_id,status,attempts,max_attempts,available_at,locked_at,locked_by,
			last_error,delivered_at,dead_lettered_at,created_at,updated_at
		FROM event_fabric_deliveries WHERE workspace_id=$1 AND id=$2
	`, workspaceID, deliveryID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.EventFabricDelivery{}, ErrEventFabricDeliveryNotFound
	}
	return item, err
}

func (r *PostgresEventFabricRepository) ListDeliveries(workspaceID, subscriptionID int64, status string, limit int) ([]model.EventFabricDelivery, error) {
	rows, err := r.db.Query(`
		SELECT id,workspace_id,subscription_id,outbox_event_id,event_key,event_type,schema_version,payload,
			correlation_id,causation_id,status,attempts,max_attempts,available_at,locked_at,locked_by,
			last_error,delivered_at,dead_lettered_at,created_at,updated_at
		FROM event_fabric_deliveries
		WHERE workspace_id=$1
		  AND ($2=0 OR subscription_id=$2)
		  AND ($3='' OR status=$3)
		ORDER BY id DESC LIMIT $4
	`, workspaceID, subscriptionID, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.EventFabricDelivery, 0)
	for rows.Next() {
		item, err := scanEventFabricDelivery(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresEventFabricRepository) ReplayDelivery(workspaceID, deliveryID int64, now time.Time) error {
	res, err := r.db.Exec(`
		UPDATE event_fabric_deliveries
		SET status='pending',attempts=0,available_at=$3,locked_at=NULL,locked_by='',last_error='',
			delivered_at=NULL,dead_lettered_at=NULL,updated_at=$3
		WHERE workspace_id=$1 AND id=$2
	`, workspaceID, deliveryID, now)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrEventFabricDeliveryNotFound
	}
	return nil
}

func (r *PostgresEventFabricRepository) ReplayRange(workspaceID, subscriptionID, fromEventID, toEventID int64, now time.Time) (int64, error) {
	res, err := r.db.Exec(`
		INSERT INTO event_fabric_deliveries (
			workspace_id,subscription_id,outbox_event_id,event_key,event_type,schema_version,payload,
			correlation_id,causation_id,occurred_at,status,attempts,max_attempts,available_at,created_at,updated_at
		)
		SELECT e.workspace_id,s.id,e.id,e.event_key,e.event_type,e.schema_version,e.payload,
			COALESCE(NULLIF(e.correlation_id,''),e.event_key),COALESCE(e.causation_id,''),e.occurred_at,
			'pending',0,s.max_attempts,$5,$5,$5
		FROM outbox_events e
		JOIN event_fabric_subscriptions s ON s.id=$2 AND s.workspace_id=$1
		WHERE e.workspace_id=$1
		  AND ($3=0 OR e.id >= $3)
		  AND ($4=0 OR e.id <= $4)
		  AND (s.event_types ? e.event_type OR s.event_types ? '*')
		ON CONFLICT (subscription_id,outbox_event_id)
		DO UPDATE SET status='pending',attempts=0,available_at=EXCLUDED.available_at,locked_at=NULL,locked_by='',
			last_error='',delivered_at=NULL,dead_lettered_at=NULL,updated_at=EXCLUDED.updated_at
	`, workspaceID, subscriptionID, fromEventID, toEventID, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *PostgresEventFabricRepository) PurgeDelivered(before time.Time, limit int) (int64, error) {
	res, err := r.db.Exec(`
		WITH doomed AS (
			SELECT d.id
			FROM event_fabric_deliveries d
			JOIN event_fabric_subscriptions s ON s.id=d.subscription_id
			WHERE d.status='delivered' AND d.delivered_at IS NOT NULL
			  AND d.delivered_at < LEAST($1, NOW() - (s.retention_days::text || ' days')::interval)
			ORDER BY d.id
			LIMIT $2
		)
		DELETE FROM event_fabric_deliveries d
		USING doomed WHERE d.id=doomed.id
	`, before, limit)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *PostgresEventFabricRepository) UpsertOffset(item model.EventConsumerOffset) error {
	_, err := r.db.Exec(`
		INSERT INTO event_consumer_offsets (
			subscription_id,workspace_id,last_event_id,last_event_key,last_occurred_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (subscription_id) DO UPDATE SET
			workspace_id=EXCLUDED.workspace_id,
			last_event_id=EXCLUDED.last_event_id,
			last_event_key=EXCLUDED.last_event_key,
			last_occurred_at=EXCLUDED.last_occurred_at,
			updated_at=EXCLUDED.updated_at
		WHERE EXCLUDED.last_event_id >= event_consumer_offsets.last_event_id
	`, item.SubscriptionID, item.WorkspaceID, item.LastEventID, item.LastEventKey, item.LastOccurredAt, item.UpdatedAt)
	return err
}

func (r *PostgresEventFabricRepository) GetOffset(workspaceID, subscriptionID int64) (model.EventConsumerOffset, error) {
	var item model.EventConsumerOffset
	err := r.db.QueryRow(`
		SELECT subscription_id,workspace_id,last_event_id,last_event_key,last_occurred_at,updated_at
		FROM event_consumer_offsets WHERE workspace_id=$1 AND subscription_id=$2
	`, workspaceID, subscriptionID).Scan(
		&item.SubscriptionID, &item.WorkspaceID, &item.LastEventID, &item.LastEventKey,
		&item.LastOccurredAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.EventConsumerOffset{SubscriptionID: subscriptionID, WorkspaceID: workspaceID}, nil
	}
	return item, err
}

func (r *PostgresEventFabricRepository) getDeliveryBySubscriptionEvent(subscriptionID, eventID int64) (model.EventFabricDelivery, error) {
	item, err := scanEventFabricDelivery(r.db.QueryRow(`
		SELECT id,workspace_id,subscription_id,outbox_event_id,event_key,event_type,schema_version,payload,
			correlation_id,causation_id,status,attempts,max_attempts,available_at,locked_at,locked_by,
			last_error,delivered_at,dead_lettered_at,created_at,updated_at
		FROM event_fabric_deliveries WHERE subscription_id=$1 AND outbox_event_id=$2
	`, subscriptionID, eventID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.EventFabricDelivery{}, ErrEventFabricDeliveryNotFound
	}
	return item, err
}

type eventFabricScanner interface {
	Scan(dest ...any) error
}

func scanEventSchema(scanner eventFabricScanner) (model.EventSchemaVersion, error) {
	var item model.EventSchemaVersion
	var raw []byte
	err := scanner.Scan(
		&item.ID, &item.WorkspaceID, &item.EventType, &item.Version, &item.Compatibility, &item.Status,
		&item.Owner, &item.Description, &raw, &item.CreatedByUserID, &item.CreatedAt, &item.DeprecatedAt,
	)
	if err != nil {
		return model.EventSchemaVersion{}, err
	}
	if err := json.Unmarshal(raw, &item.Schema); err != nil {
		return model.EventSchemaVersion{}, err
	}
	return item, nil
}

func scanEventFabricSubscription(scanner eventFabricScanner) (model.EventFabricSubscription, error) {
	var item model.EventFabricSubscription
	var raw []byte
	err := scanner.Scan(
		&item.ID, &item.WorkspaceID, &item.Name, &item.ConsumerKey, &raw, &item.Adapter, &item.Status,
		&item.MaxAttempts, &item.RetentionDays, &item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.EventFabricSubscription{}, err
	}
	if err := json.Unmarshal(raw, &item.EventTypes); err != nil {
		return model.EventFabricSubscription{}, err
	}
	return item, nil
}

func scanEventFabricDelivery(scanner eventFabricScanner) (model.EventFabricDelivery, error) {
	var item model.EventFabricDelivery
	var raw []byte
	err := scanner.Scan(
		&item.ID, &item.WorkspaceID, &item.SubscriptionID, &item.OutboxEventID, &item.EventKey,
		&item.EventType, &item.SchemaVersion, &raw, &item.CorrelationID, &item.CausationID, &item.OccurredAt, &item.Status,
		&item.Attempts, &item.MaxAttempts, &item.AvailableAt, &item.LockedAt, &item.LockedBy,
		&item.LastError, &item.DeliveredAt, &item.DeadLetteredAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.EventFabricDelivery{}, err
	}
	if err := json.Unmarshal(raw, &item.Payload); err != nil {
		return model.EventFabricDelivery{}, err
	}
	return item, nil
}
