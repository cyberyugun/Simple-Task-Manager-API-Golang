package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresIntegrationRepository struct {
	db *sql.DB
}

func NewPostgresIntegrationRepository(db *sql.DB) *PostgresIntegrationRepository {
	return &PostgresIntegrationRepository{db: db}
}

func (r *PostgresIntegrationRepository) CreateIntegrationConnection(connection model.IntegrationConnection, encryptedCredentials string) (model.IntegrationConnection, error) {
	raw, err := json.Marshal(connection.Config)
	if err != nil {
		return model.IntegrationConnection{}, err
	}
	var item model.IntegrationConnection
	var configRaw []byte
	err = r.db.QueryRow(`
		INSERT INTO integration_connections (
			organization_id, provider, name, status, auth_type, config, encrypted_credentials,
			health_status, consecutive_failures, created_by_user_id, updated_by_user_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id, organization_id, provider, name, status, auth_type, config,
			health_status, last_health_checked_at, consecutive_failures,
			rate_limit_remaining, rate_limit_reset_at, created_by_user_id, updated_by_user_id,
			created_at, updated_at
	`, connection.OrganizationID, connection.Provider, connection.Name, connection.Status,
		connection.AuthType, string(raw), encryptedCredentials, connection.HealthStatus,
		connection.ConsecutiveFailures, connection.CreatedByUserID, connection.UpdatedByUserID,
		connection.CreatedAt, connection.UpdatedAt).Scan(
		&item.ID, &item.OrganizationID, &item.Provider, &item.Name, &item.Status, &item.AuthType, &configRaw,
		&item.HealthStatus, &item.LastHealthCheckedAt, &item.ConsecutiveFailures,
		&item.RateLimitRemaining, &item.RateLimitResetAt, &item.CreatedByUserID, &item.UpdatedByUserID,
		&item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return model.IntegrationConnection{}, err
	}
	if err := json.Unmarshal(configRaw, &item.Config); err != nil {
		return model.IntegrationConnection{}, err
	}
	return item, nil
}

func (r *PostgresIntegrationRepository) UpdateIntegrationConnection(connection model.IntegrationConnection, encryptedCredentials *string) (model.IntegrationConnection, error) {
	raw, err := json.Marshal(connection.Config)
	if err != nil {
		return model.IntegrationConnection{}, err
	}
	var item model.IntegrationConnection
	var configRaw []byte
	err = r.db.QueryRow(`
		UPDATE integration_connections SET
			name=$3,status=$4,config=$5::jsonb,
			encrypted_credentials=COALESCE($6,encrypted_credentials),
			updated_by_user_id=$7,updated_at=$8
		WHERE organization_id=$1 AND id=$2
		RETURNING id,organization_id,provider,name,status,auth_type,config,
			health_status,last_health_checked_at,consecutive_failures,
			rate_limit_remaining,rate_limit_reset_at,created_by_user_id,updated_by_user_id,
			created_at,updated_at
	`, connection.OrganizationID, connection.ID, connection.Name, connection.Status, string(raw),
		encryptedCredentials, connection.UpdatedByUserID, connection.UpdatedAt).Scan(
		&item.ID, &item.OrganizationID, &item.Provider, &item.Name, &item.Status, &item.AuthType, &configRaw,
		&item.HealthStatus, &item.LastHealthCheckedAt, &item.ConsecutiveFailures,
		&item.RateLimitRemaining, &item.RateLimitResetAt, &item.CreatedByUserID, &item.UpdatedByUserID,
		&item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.IntegrationConnection{}, ErrIntegrationConnectionNotFound
	}
	if err != nil {
		return model.IntegrationConnection{}, err
	}
	if err := json.Unmarshal(configRaw, &item.Config); err != nil {
		return model.IntegrationConnection{}, err
	}
	return item, nil
}

func (r *PostgresIntegrationRepository) GetIntegrationConnection(organizationID, connectionID int64) (model.IntegrationConnection, error) {
	var item model.IntegrationConnection
	var raw []byte
	err := r.db.QueryRow(`SELECT id,organization_id,provider,name,status,auth_type,config,
		health_status,last_health_checked_at,consecutive_failures,rate_limit_remaining,rate_limit_reset_at,
		created_by_user_id,updated_by_user_id,created_at,updated_at
		FROM integration_connections WHERE organization_id=$1 AND id=$2`, organizationID, connectionID).Scan(
		&item.ID, &item.OrganizationID, &item.Provider, &item.Name, &item.Status, &item.AuthType, &raw,
		&item.HealthStatus, &item.LastHealthCheckedAt, &item.ConsecutiveFailures, &item.RateLimitRemaining,
		&item.RateLimitResetAt, &item.CreatedByUserID, &item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.IntegrationConnection{}, ErrIntegrationConnectionNotFound
	}
	if err != nil {
		return model.IntegrationConnection{}, err
	}
	if err := json.Unmarshal(raw, &item.Config); err != nil {
		return model.IntegrationConnection{}, err
	}
	return item, nil
}

func (r *PostgresIntegrationRepository) GetIntegrationConnectionSecret(connectionID int64) (model.IntegrationConnectionSecret, error) {
	var item model.IntegrationConnectionSecret
	err := r.db.QueryRow(`SELECT id,organization_id,encrypted_credentials FROM integration_connections WHERE id=$1`, connectionID).
		Scan(&item.ConnectionID, &item.OrganizationID, &item.EncryptedCredentials)
	if errors.Is(err, sql.ErrNoRows) {
		return model.IntegrationConnectionSecret{}, ErrIntegrationConnectionNotFound
	}
	return item, err
}

func (r *PostgresIntegrationRepository) ListIntegrationConnections(organizationID int64) ([]model.IntegrationConnection, error) {
	rows, err := r.db.Query(`SELECT id,organization_id,provider,name,status,auth_type,config,
		health_status,last_health_checked_at,consecutive_failures,rate_limit_remaining,rate_limit_reset_at,
		created_by_user_id,updated_by_user_id,created_at,updated_at
		FROM integration_connections WHERE organization_id=$1 ORDER BY id`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.IntegrationConnection, 0)
	for rows.Next() {
		var item model.IntegrationConnection
		var raw []byte
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.Provider, &item.Name, &item.Status, &item.AuthType, &raw,
			&item.HealthStatus, &item.LastHealthCheckedAt, &item.ConsecutiveFailures, &item.RateLimitRemaining,
			&item.RateLimitResetAt, &item.CreatedByUserID, &item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Config); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresIntegrationRepository) UpdateIntegrationHealth(connectionID int64, health string, failures int, checkedAt time.Time, remaining *int64, reset *time.Time) error {
	res, err := r.db.Exec(`UPDATE integration_connections SET health_status=$2,consecutive_failures=$3,
		last_health_checked_at=$4,rate_limit_remaining=$5,rate_limit_reset_at=$6,updated_at=$4 WHERE id=$1`,
		connectionID, health, failures, checkedAt, remaining, reset)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrIntegrationConnectionNotFound
	}
	return nil
}

func (r *PostgresIntegrationRepository) CreateIntegrationDelivery(d model.IntegrationDelivery) (model.IntegrationDelivery, error) {
	raw, err := json.Marshal(d.Payload)
	if err != nil {
		return model.IntegrationDelivery{}, err
	}
	var item model.IntegrationDelivery
	var payload []byte
	err = r.db.QueryRow(`INSERT INTO integration_deliveries (
		organization_id,connection_id,event_key,event_type,payload,status,attempts,max_attempts,
		available_at,created_by_user_id,created_at,updated_at
	) VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11,$12)
	RETURNING id,organization_id,connection_id,event_key,event_type,payload,status,attempts,max_attempts,
		available_at,locked_at,locked_by,last_error,last_http_status,delivered_at,dead_lettered_at,
		created_by_user_id,created_at,updated_at`,
		d.OrganizationID, d.ConnectionID, d.EventKey, d.EventType, string(raw), d.Status, d.Attempts, d.MaxAttempts,
		d.AvailableAt, d.CreatedByUserID, d.CreatedAt, d.UpdatedAt).Scan(
		&item.ID, &item.OrganizationID, &item.ConnectionID, &item.EventKey, &item.EventType, &payload, &item.Status,
		&item.Attempts, &item.MaxAttempts, &item.AvailableAt, &item.LockedAt, &item.LockedBy, &item.LastError,
		&item.LastHTTPStatus, &item.DeliveredAt, &item.DeadLetteredAt, &item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return model.IntegrationDelivery{}, err
	}
	if err := json.Unmarshal(payload, &item.Payload); err != nil {
		return model.IntegrationDelivery{}, err
	}
	return item, nil
}

func (r *PostgresIntegrationRepository) ListIntegrationDeliveries(orgID int64, limit int) ([]model.IntegrationDelivery, error) {
	rows, err := r.db.Query(`SELECT id,organization_id,connection_id,event_key,event_type,payload,status,attempts,max_attempts,
		available_at,locked_at,locked_by,last_error,last_http_status,delivered_at,dead_lettered_at,created_by_user_id,created_at,updated_at
		FROM integration_deliveries WHERE organization_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIntegrationDeliveries(rows)
}
func (r *PostgresIntegrationRepository) GetIntegrationDelivery(orgID, id int64) (model.IntegrationDelivery, error) {
	rows, err := r.db.Query(`SELECT id,organization_id,connection_id,event_key,event_type,payload,status,attempts,max_attempts,
		available_at,locked_at,locked_by,last_error,last_http_status,delivered_at,dead_lettered_at,created_by_user_id,created_at,updated_at
		FROM integration_deliveries WHERE organization_id=$1 AND id=$2`, orgID, id)
	if err != nil {
		return model.IntegrationDelivery{}, err
	}
	defer rows.Close()
	items, err := scanIntegrationDeliveries(rows)
	if err != nil {
		return model.IntegrationDelivery{}, err
	}
	if len(items) == 0 {
		return model.IntegrationDelivery{}, ErrIntegrationDeliveryNotFound
	}
	return items[0], nil
}
func scanIntegrationDeliveries(rows *sql.Rows) ([]model.IntegrationDelivery, error) {
	items := make([]model.IntegrationDelivery, 0)
	for rows.Next() {
		var i model.IntegrationDelivery
		var raw []byte
		if err := rows.Scan(&i.ID, &i.OrganizationID, &i.ConnectionID, &i.EventKey, &i.EventType, &raw, &i.Status, &i.Attempts, &i.MaxAttempts,
			&i.AvailableAt, &i.LockedAt, &i.LockedBy, &i.LastError, &i.LastHTTPStatus, &i.DeliveredAt, &i.DeadLetteredAt,
			&i.CreatedByUserID, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &i.Payload); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

func (r *PostgresIntegrationRepository) ClaimIntegrationDeliveries(workerID string, limit int, now time.Time) ([]model.IntegrationDelivery, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id,organization_id,connection_id,event_key,event_type,payload,status,attempts,max_attempts,
		available_at,locked_at,locked_by,last_error,last_http_status,delivered_at,dead_lettered_at,created_by_user_id,created_at,updated_at
		FROM integration_deliveries WHERE status IN ('pending','retry') AND dead_lettered_at IS NULL
		AND available_at <= $1 AND locked_at IS NULL ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	items, err := scanIntegrationDeliveries(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, i := range items {
		if _, err := tx.Exec(`UPDATE integration_deliveries SET locked_at=$2,locked_by=$3 WHERE id=$1`, i.ID, now, workerID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for idx := range items {
		t := now
		items[idx].LockedAt = &t
		items[idx].LockedBy = workerID
	}
	return items, nil
}

func (r *PostgresIntegrationRepository) MarkIntegrationDelivered(id int64, httpStatus int, now time.Time) error {
	res, err := r.db.Exec(`UPDATE integration_deliveries SET status='delivered',last_http_status=$2,delivered_at=$3,
		locked_at=NULL,locked_by='',last_error='',updated_at=$3 WHERE id=$1`, id, httpStatus, now)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrIntegrationDeliveryNotFound
	}
	return nil
}
func (r *PostgresIntegrationRepository) MarkIntegrationFailed(id int64, httpStatus int, failure string, now time.Time) (bool, error) {
	var dead bool
	err := r.db.QueryRow(`UPDATE integration_deliveries SET attempts=attempts+1,last_http_status=$2,last_error=LEFT($3,2000),
		locked_at=NULL,locked_by='',updated_at=$4,
		status=CASE WHEN attempts+1>=max_attempts THEN 'dead_letter' ELSE 'retry' END,
		dead_lettered_at=CASE WHEN attempts+1>=max_attempts THEN $4 ELSE NULL END,
		available_at=$4 + (LEAST(POWER(2,attempts+1),300)::text || ' seconds')::interval
		WHERE id=$1 RETURNING dead_lettered_at IS NOT NULL`, id, httpStatus, failure, now).Scan(&dead)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrIntegrationDeliveryNotFound
	}
	return dead, err
}
func (r *PostgresIntegrationRepository) ReplayIntegrationDelivery(orgID, id int64, now time.Time) error {
	res, err := r.db.Exec(`UPDATE integration_deliveries SET status='pending',attempts=0,available_at=$3,locked_at=NULL,locked_by='',
		last_error='',last_http_status=0,delivered_at=NULL,dead_lettered_at=NULL,updated_at=$3 WHERE organization_id=$1 AND id=$2`, orgID, id, now)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrIntegrationDeliveryNotFound
	}
	return nil
}
func (r *PostgresIntegrationRepository) ReleaseIntegrationLocks(before time.Time) (int64, error) {
	res, err := r.db.Exec(`UPDATE integration_deliveries SET locked_at=NULL,locked_by='' WHERE status IN ('pending','retry') AND locked_at<$1`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *PostgresIntegrationRepository) RecordInboundIntegrationEvent(e model.IntegrationInboundEvent) (model.IntegrationInboundEvent, error) {
	raw, err := json.Marshal(e.Payload)
	if err != nil {
		return model.IntegrationInboundEvent{}, err
	}
	var item model.IntegrationInboundEvent
	var payload []byte
	err = r.db.QueryRow(`INSERT INTO integration_inbound_events (organization_id,connection_id,provider_event_id,event_type,payload,status,received_at)
		VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7) RETURNING id,organization_id,connection_id,provider_event_id,event_type,payload,status,received_at`,
		e.OrganizationID, e.ConnectionID, e.ProviderEventID, e.EventType, string(raw), e.Status, e.ReceivedAt).Scan(
		&item.ID, &item.OrganizationID, &item.ConnectionID, &item.ProviderEventID, &item.EventType, &payload, &item.Status, &item.ReceivedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return model.IntegrationInboundEvent{}, ErrIntegrationInboundDuplicate
		}
		return model.IntegrationInboundEvent{}, err
	}
	if err := json.Unmarshal(payload, &item.Payload); err != nil {
		return model.IntegrationInboundEvent{}, err
	}
	return item, nil
}
func (r *PostgresIntegrationRepository) ListInboundIntegrationEvents(orgID int64, limit int) ([]model.IntegrationInboundEvent, error) {
	rows, err := r.db.Query(`SELECT id,organization_id,connection_id,provider_event_id,event_type,payload,status,received_at
		FROM integration_inbound_events WHERE organization_id=$1 ORDER BY received_at DESC,id DESC LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.IntegrationInboundEvent, 0)
	for rows.Next() {
		var i model.IntegrationInboundEvent
		var raw []byte
		if err := rows.Scan(&i.ID, &i.OrganizationID, &i.ConnectionID, &i.ProviderEventID, &i.EventType, &raw, &i.Status, &i.ReceivedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &i.Payload); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}
