package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"go-simple-task-api/internal/model"
)

type PostgresNotificationRepository struct {
	db *sql.DB
}

func NewPostgresNotificationRepository(db *sql.DB) *PostgresNotificationRepository {
	return &PostgresNotificationRepository{db: db}
}

func (r *PostgresNotificationRepository) GetPreference(userID int64) (model.NotificationPreference, error) {
	now := time.Now().UTC()
	if _, err := r.db.Exec(`
		INSERT INTO notification_preferences (user_id, created_at, updated_at)
		VALUES ($1,$2,$2)
		ON CONFLICT (user_id) DO NOTHING
	`, userID, now); err != nil {
		return model.NotificationPreference{}, err
	}
	return scanNotificationPreference(r.db.QueryRow(`
		SELECT user_id, locale, timezone, quiet_hours_enabled, quiet_start, quiet_end,
			digest_frequency, digest_hour, channels, events, created_at, updated_at
		FROM notification_preferences
		WHERE user_id=$1
	`, userID))
}

func (r *PostgresNotificationRepository) UpsertPreference(item model.NotificationPreference) (model.NotificationPreference, error) {
	channels, err := json.Marshal(item.Channels)
	if err != nil {
		return model.NotificationPreference{}, err
	}
	events, err := json.Marshal(item.Events)
	if err != nil {
		return model.NotificationPreference{}, err
	}
	return scanNotificationPreference(r.db.QueryRow(`
		INSERT INTO notification_preferences (
			user_id, locale, timezone, quiet_hours_enabled, quiet_start, quiet_end,
			digest_frequency, digest_hour, channels, events, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11,$12)
		ON CONFLICT (user_id) DO UPDATE SET
			locale=EXCLUDED.locale,
			timezone=EXCLUDED.timezone,
			quiet_hours_enabled=EXCLUDED.quiet_hours_enabled,
			quiet_start=EXCLUDED.quiet_start,
			quiet_end=EXCLUDED.quiet_end,
			digest_frequency=EXCLUDED.digest_frequency,
			digest_hour=EXCLUDED.digest_hour,
			channels=EXCLUDED.channels,
			events=EXCLUDED.events,
			updated_at=EXCLUDED.updated_at
		RETURNING user_id, locale, timezone, quiet_hours_enabled, quiet_start, quiet_end,
			digest_frequency, digest_hour, channels, events, created_at, updated_at
	`, item.UserID, item.Locale, item.Timezone, item.QuietHoursEnabled, item.QuietStart, item.QuietEnd,
		item.DigestFrequency, item.DigestHour, string(channels), string(events), item.CreatedAt, item.UpdatedAt))
}

func (r *PostgresNotificationRepository) ListDigestPreferences() ([]model.NotificationPreference, error) {
	rows, err := r.db.Query(`
		SELECT user_id, locale, timezone, quiet_hours_enabled, quiet_start, quiet_end,
			digest_frequency, digest_hour, channels, events, created_at, updated_at
		FROM notification_preferences
		WHERE digest_frequency <> 'off'
		ORDER BY user_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.NotificationPreference, 0)
	for rows.Next() {
		item, err := scanNotificationPreference(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type notificationPreferenceScanner interface {
	Scan(...any) error
}

func scanNotificationPreference(scanner notificationPreferenceScanner) (model.NotificationPreference, error) {
	var item model.NotificationPreference
	var channelsRaw, eventsRaw []byte
	err := scanner.Scan(&item.UserID, &item.Locale, &item.Timezone, &item.QuietHoursEnabled,
		&item.QuietStart, &item.QuietEnd, &item.DigestFrequency, &item.DigestHour,
		&channelsRaw, &eventsRaw, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return model.NotificationPreference{}, err
	}
	item.Channels = map[string]bool{}
	item.Events = map[string]bool{}
	if err := json.Unmarshal(channelsRaw, &item.Channels); err != nil {
		return model.NotificationPreference{}, err
	}
	if err := json.Unmarshal(eventsRaw, &item.Events); err != nil {
		return model.NotificationPreference{}, err
	}
	return item, nil
}

func (r *PostgresNotificationRepository) CreateEndpoint(item model.NotificationEndpoint) (model.NotificationEndpoint, error) {
	err := r.db.QueryRow(`
		INSERT INTO notification_endpoints (
			user_id, channel, address, secret, active, created_at, updated_at, deleted_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id,user_id,channel,address,secret,active,created_at,updated_at,deleted_at
	`, item.UserID, item.Channel, item.Address, item.Secret, item.Active, item.CreatedAt, item.UpdatedAt, item.DeletedAt).
		Scan(&item.ID, &item.UserID, &item.Channel, &item.Address, &item.Secret, &item.Active,
			&item.CreatedAt, &item.UpdatedAt, &item.DeletedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.NotificationEndpoint{}, ErrTaskRelationExists
		}
	}
	return item, err
}

func (r *PostgresNotificationRepository) ListEndpoints(userID int64) ([]model.NotificationEndpoint, error) {
	rows, err := r.db.Query(`
		SELECT id,user_id,channel,address,'' AS secret,active,created_at,updated_at,deleted_at
		FROM notification_endpoints
		WHERE user_id=$1 AND deleted_at IS NULL
		ORDER BY id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.NotificationEndpoint, 0)
	for rows.Next() {
		var item model.NotificationEndpoint
		if err := rows.Scan(&item.ID, &item.UserID, &item.Channel, &item.Address, &item.Secret,
			&item.Active, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresNotificationRepository) GetEndpoint(userID, endpointID int64) (model.NotificationEndpoint, error) {
	var item model.NotificationEndpoint
	err := r.db.QueryRow(`
		SELECT id,user_id,channel,address,secret,active,created_at,updated_at,deleted_at
		FROM notification_endpoints
		WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL
	`, userID, endpointID).Scan(&item.ID, &item.UserID, &item.Channel, &item.Address, &item.Secret,
		&item.Active, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.NotificationEndpoint{}, ErrNotificationEndpointNotFound
	}
	return item, err
}

func (r *PostgresNotificationRepository) DeleteEndpoint(userID, endpointID int64, at time.Time) error {
	result, err := r.db.Exec(`
		UPDATE notification_endpoints
		SET active=FALSE, deleted_at=$3, updated_at=$3
		WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL
	`, userID, endpointID, at)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotificationEndpointNotFound
	}
	return nil
}

func (r *PostgresNotificationRepository) CreateTemplate(item model.NotificationTemplate) (model.NotificationTemplate, error) {
	err := r.db.QueryRow(`
		INSERT INTO notification_templates (
			organization_id,template_key,locale,channel,version,status,subject,body,
			created_by_user_id,published_by_user_id,published_at,created_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id,organization_id,template_key,locale,channel,version,status,subject,body,
			created_by_user_id,published_by_user_id,published_at,created_at,updated_at
	`, item.OrganizationID, item.Key, item.Locale, item.Channel, item.Version, item.Status,
		item.Subject, item.Body, item.CreatedByUserID, item.PublishedByID, item.PublishedAt,
		item.CreatedAt, item.UpdatedAt).
		Scan(&item.ID, &item.OrganizationID, &item.Key, &item.Locale, &item.Channel, &item.Version,
			&item.Status, &item.Subject, &item.Body, &item.CreatedByUserID, &item.PublishedByID,
			&item.PublishedAt, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PostgresNotificationRepository) ListTemplates(organizationID int64) ([]model.NotificationTemplate, error) {
	rows, err := r.db.Query(`
		SELECT id,organization_id,template_key,locale,channel,version,status,subject,body,
			created_by_user_id,published_by_user_id,published_at,created_at,updated_at
		FROM notification_templates
		WHERE organization_id=$1
		ORDER BY template_key,locale,channel,version
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.NotificationTemplate, 0)
	for rows.Next() {
		item, err := scanNotificationTemplate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type notificationTemplateScanner interface {
	Scan(...any) error
}

func scanNotificationTemplate(scanner notificationTemplateScanner) (model.NotificationTemplate, error) {
	var item model.NotificationTemplate
	err := scanner.Scan(&item.ID, &item.OrganizationID, &item.Key, &item.Locale, &item.Channel,
		&item.Version, &item.Status, &item.Subject, &item.Body, &item.CreatedByUserID,
		&item.PublishedByID, &item.PublishedAt, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PostgresNotificationRepository) GetTemplate(organizationID *int64, templateID int64) (model.NotificationTemplate, error) {
	var row *sql.Row
	if organizationID == nil {
		row = r.db.QueryRow(`
			SELECT id,organization_id,template_key,locale,channel,version,status,subject,body,
				created_by_user_id,published_by_user_id,published_at,created_at,updated_at
			FROM notification_templates WHERE id=$1 AND organization_id IS NULL
		`, templateID)
	} else {
		row = r.db.QueryRow(`
			SELECT id,organization_id,template_key,locale,channel,version,status,subject,body,
				created_by_user_id,published_by_user_id,published_at,created_at,updated_at
			FROM notification_templates WHERE id=$1 AND organization_id=$2
		`, templateID, *organizationID)
	}
	item, err := scanNotificationTemplate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.NotificationTemplate{}, ErrNotificationTemplateNotFound
	}
	return item, err
}

func (r *PostgresNotificationRepository) UpdateTemplate(item model.NotificationTemplate) (model.NotificationTemplate, error) {
	var row *sql.Row
	if item.OrganizationID == nil {
		row = r.db.QueryRow(`
			UPDATE notification_templates
			SET status=$2,subject=$3,body=$4,published_by_user_id=$5,published_at=$6,updated_at=$7
			WHERE id=$1 AND organization_id IS NULL
			RETURNING id,organization_id,template_key,locale,channel,version,status,subject,body,
				created_by_user_id,published_by_user_id,published_at,created_at,updated_at
		`, item.ID, item.Status, item.Subject, item.Body, item.PublishedByID, item.PublishedAt, item.UpdatedAt)
	} else {
		row = r.db.QueryRow(`
			UPDATE notification_templates
			SET status=$3,subject=$4,body=$5,published_by_user_id=$6,published_at=$7,updated_at=$8
			WHERE id=$1 AND organization_id=$2
			RETURNING id,organization_id,template_key,locale,channel,version,status,subject,body,
				created_by_user_id,published_by_user_id,published_at,created_at,updated_at
		`, item.ID, *item.OrganizationID, item.Status, item.Subject, item.Body, item.PublishedByID,
			item.PublishedAt, item.UpdatedAt)
	}
	updated, err := scanNotificationTemplate(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.NotificationTemplate{}, ErrNotificationTemplateNotFound
	}
	return updated, err
}

func (r *PostgresNotificationRepository) NextTemplateVersion(organizationID *int64, key, locale, channel string) (int, error) {
	var next int
	var err error
	if organizationID == nil {
		err = r.db.QueryRow(`
			SELECT COALESCE(MAX(version),0)+1
			FROM notification_templates
			WHERE organization_id IS NULL AND template_key=$1 AND locale=$2 AND channel=$3
		`, key, locale, channel).Scan(&next)
	} else {
		err = r.db.QueryRow(`
			SELECT COALESCE(MAX(version),0)+1
			FROM notification_templates
			WHERE organization_id=$1 AND template_key=$2 AND locale=$3 AND channel=$4
		`, *organizationID, key, locale, channel).Scan(&next)
	}
	return next, err
}

func (r *PostgresNotificationRepository) GetPublishedTemplate(organizationID *int64, key, locale, channel string) (model.NotificationTemplate, error) {
	if organizationID != nil {
		item, err := scanNotificationTemplate(r.db.QueryRow(`
			SELECT id,organization_id,template_key,locale,channel,version,status,subject,body,
				created_by_user_id,published_by_user_id,published_at,created_at,updated_at
			FROM notification_templates
			WHERE organization_id=$1 AND template_key=$2 AND locale=$3 AND channel=$4 AND status='published'
			ORDER BY version DESC LIMIT 1
		`, *organizationID, key, locale, channel))
		if err == nil {
			return item, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return model.NotificationTemplate{}, err
		}
	}
	item, err := scanNotificationTemplate(r.db.QueryRow(`
		SELECT id,organization_id,template_key,locale,channel,version,status,subject,body,
			created_by_user_id,published_by_user_id,published_at,created_at,updated_at
		FROM notification_templates
		WHERE organization_id IS NULL AND template_key=$1 AND locale=$2 AND channel=$3 AND status='published'
		ORDER BY version DESC LIMIT 1
	`, key, locale, channel))
	if errors.Is(err, sql.ErrNoRows) {
		return model.NotificationTemplate{}, ErrNotificationTemplateNotFound
	}
	return item, err
}

func (r *PostgresNotificationRepository) CreateNotification(item model.Notification) (model.Notification, bool, error) {
	data, err := json.Marshal(item.Data)
	if err != nil {
		return model.Notification{}, false, err
	}
	var raw []byte
	err = r.db.QueryRow(`
		INSERT INTO notifications (
			organization_id,workspace_id,user_id,event_type,template_key,title,body,data,
			dedupe_key,read_at,created_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12)
		ON CONFLICT (user_id,dedupe_key) DO NOTHING
		RETURNING id,organization_id,workspace_id,user_id,event_type,template_key,title,body,
			data,dedupe_key,read_at,created_at,updated_at
	`, item.OrganizationID, item.WorkspaceID, item.UserID, item.EventType, item.TemplateKey,
		item.Title, item.Body, string(data), item.DedupeKey, item.ReadAt, item.CreatedAt, item.UpdatedAt).
		Scan(&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.UserID, &item.EventType,
			&item.TemplateKey, &item.Title, &item.Body, &raw, &item.DedupeKey, &item.ReadAt,
			&item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		existing, getErr := r.getNotificationByDedupe(item.UserID, item.DedupeKey)
		return existing, false, getErr
	}
	if err != nil {
		return model.Notification{}, false, err
	}
	item.Data = map[string]any{}
	if err := json.Unmarshal(raw, &item.Data); err != nil {
		return model.Notification{}, false, err
	}
	return item, true, nil
}

func (r *PostgresNotificationRepository) getNotificationByDedupe(userID int64, dedupeKey string) (model.Notification, error) {
	item, err := scanNotification(r.db.QueryRow(`
		SELECT id,organization_id,workspace_id,user_id,event_type,template_key,title,body,data,
			dedupe_key,read_at,created_at,updated_at
		FROM notifications WHERE user_id=$1 AND dedupe_key=$2
	`, userID, dedupeKey))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Notification{}, ErrNotificationNotFound
	}
	return item, err
}

type notificationScanner interface {
	Scan(...any) error
}

func scanNotification(scanner notificationScanner) (model.Notification, error) {
	var item model.Notification
	var raw []byte
	err := scanner.Scan(&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.UserID,
		&item.EventType, &item.TemplateKey, &item.Title, &item.Body, &raw, &item.DedupeKey,
		&item.ReadAt, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return model.Notification{}, err
	}
	item.Data = map[string]any{}
	if err := json.Unmarshal(raw, &item.Data); err != nil {
		return model.Notification{}, err
	}
	return item, nil
}

func (r *PostgresNotificationRepository) GetNotification(userID, notificationID int64) (model.Notification, error) {
	item, err := scanNotification(r.db.QueryRow(`
		SELECT id,organization_id,workspace_id,user_id,event_type,template_key,title,body,data,
			dedupe_key,read_at,created_at,updated_at
		FROM notifications WHERE user_id=$1 AND id=$2
	`, userID, notificationID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Notification{}, ErrNotificationNotFound
	}
	return item, err
}

func (r *PostgresNotificationRepository) ListNotifications(userID int64, unreadOnly bool, limit int) ([]model.Notification, int64, error) {
	var unread int64
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id=$1 AND read_at IS NULL`, userID).Scan(&unread); err != nil {
		return nil, 0, err
	}
	query := `
		SELECT id,organization_id,workspace_id,user_id,event_type,template_key,title,body,data,
			dedupe_key,read_at,created_at,updated_at
		FROM notifications
		WHERE user_id=$1
	`
	if unreadOnly {
		query += " AND read_at IS NULL"
	}
	query += " ORDER BY created_at DESC,id DESC LIMIT $2"
	rows, err := r.db.Query(query, userID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]model.Notification, 0)
	for rows.Next() {
		item, err := scanNotification(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, unread, rows.Err()
}

func (r *PostgresNotificationRepository) MarkNotificationRead(userID, notificationID int64, at time.Time) (model.Notification, error) {
	item, err := scanNotification(r.db.QueryRow(`
		UPDATE notifications
		SET read_at=COALESCE(read_at,$3), updated_at=$3
		WHERE user_id=$1 AND id=$2
		RETURNING id,organization_id,workspace_id,user_id,event_type,template_key,title,body,data,
			dedupe_key,read_at,created_at,updated_at
	`, userID, notificationID, at))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Notification{}, ErrNotificationNotFound
	}
	return item, err
}

func (r *PostgresNotificationRepository) MarkAllNotificationsRead(userID int64, at time.Time) (int64, error) {
	result, err := r.db.Exec(`
		UPDATE notifications SET read_at=$2,updated_at=$2
		WHERE user_id=$1 AND read_at IS NULL
	`, userID, at)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *PostgresNotificationRepository) UnreadCountSince(userID int64, since time.Time) (int64, error) {
	var count int64
	err := r.db.QueryRow(`
		SELECT COUNT(*)
		FROM notifications
		WHERE user_id=$1 AND read_at IS NULL AND created_at >= $2 AND event_type <> $3
	`, userID, since, model.NotificationEventDigestSummary).Scan(&count)
	return count, err
}

func (r *PostgresNotificationRepository) CreateDelivery(item model.NotificationDelivery) (model.NotificationDelivery, error) {
	err := r.db.QueryRow(`
		INSERT INTO notification_deliveries (
			notification_id,user_id,channel,destination,endpoint_id,status,attempts,max_attempts,
			available_at,last_error,sent_at,created_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (notification_id,channel,destination) DO UPDATE
		SET updated_at=notification_deliveries.updated_at
		RETURNING id,notification_id,user_id,channel,destination,endpoint_id,status,attempts,
			max_attempts,available_at,last_error,sent_at,created_at,updated_at
	`, item.NotificationID, item.UserID, item.Channel, item.Destination, item.EndpointID,
		item.Status, item.Attempts, item.MaxAttempts, item.AvailableAt, item.LastError,
		item.SentAt, item.CreatedAt, item.UpdatedAt).
		Scan(&item.ID, &item.NotificationID, &item.UserID, &item.Channel, &item.Destination,
			&item.EndpointID, &item.Status, &item.Attempts, &item.MaxAttempts, &item.AvailableAt,
			&item.LastError, &item.SentAt, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PostgresNotificationRepository) ClaimDeliveries(workerID string, limit int, now time.Time) ([]model.NotificationDeliveryDetail, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`
		SELECT d.id,d.notification_id,d.user_id,d.channel,d.destination,d.endpoint_id,d.status,
			d.attempts,d.max_attempts,d.available_at,d.last_error,d.sent_at,d.created_at,d.updated_at,
			n.id,n.organization_id,n.workspace_id,n.user_id,n.event_type,n.template_key,n.title,n.body,
			n.data,n.dedupe_key,n.read_at,n.created_at,n.updated_at
		FROM notification_deliveries d
		JOIN notifications n ON n.id=d.notification_id
		WHERE d.status IN ('pending','retry')
		  AND d.available_at <= $1
		  AND d.locked_at IS NULL
		ORDER BY d.available_at,d.id
		FOR UPDATE OF d SKIP LOCKED
		LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, err
	}
	items := make([]model.NotificationDeliveryDetail, 0)
	ids := make([]int64, 0)
	for rows.Next() {
		var detail model.NotificationDeliveryDetail
		var dataRaw []byte
		if err := rows.Scan(
			&detail.Delivery.ID, &detail.Delivery.NotificationID, &detail.Delivery.UserID,
			&detail.Delivery.Channel, &detail.Delivery.Destination, &detail.Delivery.EndpointID,
			&detail.Delivery.Status, &detail.Delivery.Attempts, &detail.Delivery.MaxAttempts,
			&detail.Delivery.AvailableAt, &detail.Delivery.LastError, &detail.Delivery.SentAt,
			&detail.Delivery.CreatedAt, &detail.Delivery.UpdatedAt,
			&detail.Notification.ID, &detail.Notification.OrganizationID, &detail.Notification.WorkspaceID,
			&detail.Notification.UserID, &detail.Notification.EventType, &detail.Notification.TemplateKey,
			&detail.Notification.Title, &detail.Notification.Body, &dataRaw,
			&detail.Notification.DedupeKey, &detail.Notification.ReadAt,
			&detail.Notification.CreatedAt, &detail.Notification.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		detail.Notification.Data = map[string]any{}
		if err := json.Unmarshal(dataRaw, &detail.Notification.Data); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, detail)
		ids = append(ids, detail.Delivery.ID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err := tx.Exec(`
			UPDATE notification_deliveries SET locked_at=$2,locked_by=$3,updated_at=$2
			WHERE id=$1
		`, id, now, workerID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *PostgresNotificationRepository) MarkDeliverySent(deliveryID int64, at time.Time) (model.NotificationDelivery, error) {
	var item model.NotificationDelivery
	err := r.db.QueryRow(`
		UPDATE notification_deliveries
		SET status='sent',attempts=attempts+1,last_error='',sent_at=$2,locked_at=NULL,locked_by=NULL,updated_at=$2
		WHERE id=$1
		RETURNING id,notification_id,user_id,channel,destination,endpoint_id,status,attempts,max_attempts,
			available_at,last_error,sent_at,created_at,updated_at
	`, deliveryID, at).Scan(&item.ID, &item.NotificationID, &item.UserID, &item.Channel,
		&item.Destination, &item.EndpointID, &item.Status, &item.Attempts, &item.MaxAttempts,
		&item.AvailableAt, &item.LastError, &item.SentAt, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.NotificationDelivery{}, ErrNotificationDeliveryNotFound
	}
	return item, err
}

func (r *PostgresNotificationRepository) MarkDeliveryFailed(deliveryID int64, failure string, at time.Time) (model.NotificationDelivery, error) {
	var item model.NotificationDelivery
	err := r.db.QueryRow(`
		UPDATE notification_deliveries
		SET attempts=attempts+1,
			last_error=LEFT($2,2000),
			status=CASE WHEN attempts+1 >= max_attempts THEN 'dead_letter' ELSE 'retry' END,
			available_at=CASE
				WHEN attempts+1 >= max_attempts THEN available_at
				ELSE $3 + (LEAST(POWER(2,attempts+1),300)::text || ' seconds')::interval
			END,
			locked_at=NULL,locked_by=NULL,updated_at=$3
		WHERE id=$1
		RETURNING id,notification_id,user_id,channel,destination,endpoint_id,status,attempts,max_attempts,
			available_at,last_error,sent_at,created_at,updated_at
	`, deliveryID, failure, at).Scan(&item.ID, &item.NotificationID, &item.UserID,
		&item.Channel, &item.Destination, &item.EndpointID, &item.Status, &item.Attempts,
		&item.MaxAttempts, &item.AvailableAt, &item.LastError, &item.SentAt,
		&item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.NotificationDelivery{}, ErrNotificationDeliveryNotFound
	}
	return item, err
}

func (r *PostgresNotificationRepository) ListDeliveries(userID int64, limit int) ([]model.NotificationDelivery, error) {
	rows, err := r.db.Query(`
		SELECT id,notification_id,user_id,channel,destination,endpoint_id,status,attempts,max_attempts,
			available_at,last_error,sent_at,created_at,updated_at
		FROM notification_deliveries WHERE user_id=$1 ORDER BY id DESC LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.NotificationDelivery, 0)
	for rows.Next() {
		var item model.NotificationDelivery
		if err := rows.Scan(&item.ID, &item.NotificationID, &item.UserID, &item.Channel,
			&item.Destination, &item.EndpointID, &item.Status, &item.Attempts, &item.MaxAttempts,
			&item.AvailableAt, &item.LastError, &item.SentAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresNotificationRepository) RetryDelivery(userID, deliveryID int64, at time.Time) (model.NotificationDelivery, error) {
	var item model.NotificationDelivery
	err := r.db.QueryRow(`
		UPDATE notification_deliveries
		SET status='pending',attempts=0,last_error='',available_at=$3,sent_at=NULL,
			locked_at=NULL,locked_by=NULL,updated_at=$3
		WHERE user_id=$1 AND id=$2 AND status='dead_letter'
		RETURNING id,notification_id,user_id,channel,destination,endpoint_id,status,attempts,max_attempts,
			available_at,last_error,sent_at,created_at,updated_at
	`, userID, deliveryID, at).Scan(&item.ID, &item.NotificationID, &item.UserID,
		&item.Channel, &item.Destination, &item.EndpointID, &item.Status, &item.Attempts,
		&item.MaxAttempts, &item.AvailableAt, &item.LastError, &item.SentAt,
		&item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.NotificationDelivery{}, ErrNotificationDeliveryNotFound
	}
	return item, err
}

func (r *PostgresNotificationRepository) ReleaseStaleDeliveryLocks(before time.Time) (int64, error) {
	result, err := r.db.Exec(`
		UPDATE notification_deliveries
		SET locked_at=NULL,locked_by=NULL
		WHERE status IN ('pending','retry') AND locked_at < $1
	`, before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *PostgresNotificationRepository) ListReminderCandidates(now, horizon time.Time, limit int) ([]model.NotificationReminderCandidate, error) {
	_ = now
	rows, err := r.db.Query(`
		WITH recipients AS (
			SELECT t.workspace_id,t.id AS task_id,t.title,t.due_at,a.user_id
			FROM tasks t
			JOIN task_assignees a ON a.task_id=t.id
			WHERE t.workspace_id IS NOT NULL
			  AND t.deleted_at IS NULL AND t.archived_at IS NULL
			  AND t.status NOT IN ('DONE','ARCHIVED')
			  AND t.due_at IS NOT NULL AND t.due_at <= $1
			UNION
			SELECT t.workspace_id,t.id AS task_id,t.title,t.due_at,COALESCE(t.created_by_user_id,t.user_id)
			FROM tasks t
			WHERE t.workspace_id IS NOT NULL
			  AND t.deleted_at IS NULL AND t.archived_at IS NULL
			  AND t.status NOT IN ('DONE','ARCHIVED')
			  AND t.due_at IS NOT NULL AND t.due_at <= $1
		)
		SELECT workspace_id,task_id,user_id,title,due_at
		FROM recipients
		WHERE user_id IS NOT NULL
		ORDER BY due_at,task_id,user_id
		LIMIT $2
	`, horizon, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.NotificationReminderCandidate, 0)
	for rows.Next() {
		var item model.NotificationReminderCandidate
		if err := rows.Scan(&item.WorkspaceID, &item.TaskID, &item.UserID, &item.TaskTitle, &item.DueAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresNotificationRepository) ListApprovalCandidates(limit int) ([]model.NotificationApprovalCandidate, error) {
	rows, err := r.db.Query(`
		SELECT a.organization_id,e.workflow_id,e.id,a.id,m.user_id,w.name,a.node_id
		FROM workflow_approvals a
		JOIN workflow_executions e ON e.id=a.execution_id
		JOIN workflow_definitions w ON w.id=e.workflow_id
		JOIN organization_members m ON m.organization_id=a.organization_id
		WHERE a.status='pending'
		  AND m.role IN ('owner','admin','delegated_admin')
		ORDER BY a.requested_at,a.id,m.user_id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.NotificationApprovalCandidate, 0)
	for rows.Next() {
		var item model.NotificationApprovalCandidate
		if err := rows.Scan(&item.OrganizationID, &item.WorkflowID, &item.ExecutionID,
			&item.ApprovalID, &item.UserID, &item.WorkflowName, &item.NodeID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresNotificationRepository) WorkspaceOrganization(workspaceID int64) (*int64, error) {
	var organizationID int64
	err := r.db.QueryRow(`
		SELECT organization_id FROM organization_workspaces WHERE workspace_id=$1
	`, workspaceID).Scan(&organizationID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &organizationID, nil
}
