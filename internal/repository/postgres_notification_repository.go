package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresNotificationRepository struct{ db *sql.DB }

func NewPostgresNotificationRepository(db *sql.DB) *PostgresNotificationRepository {
	return &PostgresNotificationRepository{db: db}
}

func (r *PostgresNotificationRepository) GetPreference(userID int64, organizationID *int64) (model.NotificationPreference, error) {
	var item model.NotificationPreference
	var muted []byte
	var org sql.NullInt64
	err := r.db.QueryRow(`
		SELECT user_id, organization_id, in_app_enabled, email_enabled, push_enabled,
		       webhook_enabled, digest, timezone, locale,
		       COALESCE(quiet_hours_start::text,''), COALESCE(quiet_hours_end::text,''),
		       muted_event_types, updated_at
		FROM notification_preferences
		WHERE user_id=$1 AND COALESCE(organization_id,0)=COALESCE($2,0)
	`, userID, organizationID).Scan(
		&item.UserID, &org, &item.InAppEnabled, &item.EmailEnabled, &item.PushEnabled,
		&item.WebhookEnabled, &item.Digest, &item.Timezone, &item.Locale,
		&item.QuietHoursStart, &item.QuietHoursEnd, &muted, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.NotificationPreference{}, ErrNotificationPreferenceNotFound
	}
	if err != nil {
		return model.NotificationPreference{}, err
	}
	if org.Valid {
		value := org.Int64
		item.OrganizationID = &value
	}
	_ = json.Unmarshal(muted, &item.MutedEventTypes)
	if item.MutedEventTypes == nil {
		item.MutedEventTypes = []string{}
	}
	return item, nil
}

func (r *PostgresNotificationRepository) UpsertPreference(item model.NotificationPreference) (model.NotificationPreference, error) {
	muted, err := json.Marshal(item.MutedEventTypes)
	if err != nil {
		return model.NotificationPreference{}, err
	}
	var org sql.NullInt64
	var mutedOut []byte
	err = r.db.QueryRow(`
		INSERT INTO notification_preferences (
			user_id, organization_id, in_app_enabled, email_enabled, push_enabled, webhook_enabled,
			digest, timezone, locale, quiet_hours_start, quiet_hours_end, muted_event_types, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,
			NULLIF($10,'')::time,NULLIF($11,'')::time,$12::jsonb,$13
		)
		ON CONFLICT (user_id, (COALESCE(organization_id,0)))
		DO UPDATE SET
			in_app_enabled=EXCLUDED.in_app_enabled,
			email_enabled=EXCLUDED.email_enabled,
			push_enabled=EXCLUDED.push_enabled,
			webhook_enabled=EXCLUDED.webhook_enabled,
			digest=EXCLUDED.digest,
			timezone=EXCLUDED.timezone,
			locale=EXCLUDED.locale,
			quiet_hours_start=EXCLUDED.quiet_hours_start,
			quiet_hours_end=EXCLUDED.quiet_hours_end,
			muted_event_types=EXCLUDED.muted_event_types,
			updated_at=EXCLUDED.updated_at
		RETURNING user_id, organization_id, in_app_enabled, email_enabled, push_enabled,
			webhook_enabled, digest, timezone, locale,
			COALESCE(quiet_hours_start::text,''), COALESCE(quiet_hours_end::text,''),
			muted_event_types, updated_at
	`, item.UserID, item.OrganizationID, item.InAppEnabled, item.EmailEnabled, item.PushEnabled,
		item.WebhookEnabled, item.Digest, item.Timezone, item.Locale,
		item.QuietHoursStart, item.QuietHoursEnd, string(muted), item.UpdatedAt,
	).Scan(
		&item.UserID, &org, &item.InAppEnabled, &item.EmailEnabled, &item.PushEnabled,
		&item.WebhookEnabled, &item.Digest, &item.Timezone, &item.Locale,
		&item.QuietHoursStart, &item.QuietHoursEnd, &mutedOut, &item.UpdatedAt,
	)
	if err != nil {
		return model.NotificationPreference{}, err
	}
	item.OrganizationID = nil
	if org.Valid {
		value := org.Int64
		item.OrganizationID = &value
	}
	item.MutedEventTypes = nil
	_ = json.Unmarshal(mutedOut, &item.MutedEventTypes)
	if item.MutedEventTypes == nil {
		item.MutedEventTypes = []string{}
	}
	return item, nil
}

func (r *PostgresNotificationRepository) CreateNotification(item model.Notification) (model.Notification, bool, error) {
	raw, err := json.Marshal(item.Data)
	if err != nil {
		return model.Notification{}, false, err
	}
	var org, workspace sql.NullInt64
	err = r.db.QueryRow(`
		INSERT INTO notifications (
			user_id, organization_id, workspace_id, event_type, title, body, data,
			dedup_key, template_key, template_version, read_at, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11,$12)
		ON CONFLICT (user_id, dedup_key) WHERE dedup_key <> '' DO NOTHING
		RETURNING id,user_id,organization_id,workspace_id,event_type,title,body,data,
			dedup_key,template_key,template_version,read_at,created_at
	`, item.UserID, item.OrganizationID, item.WorkspaceID, item.EventType, item.Title, item.Body,
		string(raw), item.DedupKey, item.TemplateKey, item.TemplateVersion, item.ReadAt, item.CreatedAt,
	).Scan(
		&item.ID, &item.UserID, &org, &workspace, &item.EventType, &item.Title, &item.Body, &raw,
		&item.DedupKey, &item.TemplateKey, &item.TemplateVersion, &item.ReadAt, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		if item.DedupKey == "" {
			return model.Notification{}, false, err
		}
		existing, getErr := r.notificationByDedup(item.UserID, item.DedupKey)
		return existing, false, getErr
	}
	if err != nil {
		return model.Notification{}, false, err
	}
	if org.Valid {
		value := org.Int64
		item.OrganizationID = &value
	}
	if workspace.Valid {
		value := workspace.Int64
		item.WorkspaceID = &value
	}
	item.Data = map[string]any{}
	_ = json.Unmarshal(raw, &item.Data)
	return item, true, nil
}

func (r *PostgresNotificationRepository) notificationByDedup(userID int64, dedupKey string) (model.Notification, error) {
	var item model.Notification
	var org, workspace sql.NullInt64
	var raw []byte
	err := r.db.QueryRow(`
		SELECT id,user_id,organization_id,workspace_id,event_type,title,body,data,
		       dedup_key,template_key,template_version,read_at,created_at
		FROM notifications WHERE user_id=$1 AND dedup_key=$2
	`, userID, dedupKey).Scan(
		&item.ID, &item.UserID, &org, &workspace, &item.EventType, &item.Title, &item.Body, &raw,
		&item.DedupKey, &item.TemplateKey, &item.TemplateVersion, &item.ReadAt, &item.CreatedAt,
	)
	if err != nil {
		return model.Notification{}, err
	}
	if org.Valid {
		value := org.Int64
		item.OrganizationID = &value
	}
	if workspace.Valid {
		value := workspace.Int64
		item.WorkspaceID = &value
	}
	item.Data = map[string]any{}
	_ = json.Unmarshal(raw, &item.Data)
	return item, nil
}

func (r *PostgresNotificationRepository) ListNotifications(userID int64, limit int) ([]model.Notification, error) {
	rows, err := r.db.Query(`
		SELECT id,user_id,organization_id,workspace_id,event_type,title,body,data,
		       dedup_key,template_key,template_version,read_at,created_at
		FROM notifications WHERE user_id=$1
		ORDER BY created_at DESC,id DESC LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Notification, 0)
	for rows.Next() {
		var item model.Notification
		var org, workspace sql.NullInt64
		var raw []byte
		if err := rows.Scan(
			&item.ID, &item.UserID, &org, &workspace, &item.EventType, &item.Title, &item.Body, &raw,
			&item.DedupKey, &item.TemplateKey, &item.TemplateVersion, &item.ReadAt, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if org.Valid {
			value := org.Int64
			item.OrganizationID = &value
		}
		if workspace.Valid {
			value := workspace.Int64
			item.WorkspaceID = &value
		}
		item.Data = map[string]any{}
		_ = json.Unmarshal(raw, &item.Data)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresNotificationRepository) MarkNotificationRead(userID, notificationID int64, at time.Time) (model.Notification, error) {
	var item model.Notification
	var org, workspace sql.NullInt64
	var raw []byte
	err := r.db.QueryRow(`
		UPDATE notifications SET read_at=COALESCE(read_at,$3)
		WHERE user_id=$1 AND id=$2
		RETURNING id,user_id,organization_id,workspace_id,event_type,title,body,data,
			dedup_key,template_key,template_version,read_at,created_at
	`, userID, notificationID, at).Scan(
		&item.ID, &item.UserID, &org, &workspace, &item.EventType, &item.Title, &item.Body, &raw,
		&item.DedupKey, &item.TemplateKey, &item.TemplateVersion, &item.ReadAt, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Notification{}, ErrNotificationNotFound
	}
	if err != nil {
		return model.Notification{}, err
	}
	if org.Valid {
		value := org.Int64
		item.OrganizationID = &value
	}
	if workspace.Valid {
		value := workspace.Int64
		item.WorkspaceID = &value
	}
	item.Data = map[string]any{}
	_ = json.Unmarshal(raw, &item.Data)
	return item, nil
}

func (r *PostgresNotificationRepository) CreateDelivery(item model.NotificationDelivery) (model.NotificationDelivery, error) {
	err := r.db.QueryRow(`
		INSERT INTO notification_deliveries (
			notification_id,user_id,channel,destination,status,attempt,max_attempts,scheduled_at,
			next_attempt_at,last_error,sent_at,created_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (notification_id,channel) DO NOTHING
		RETURNING id,notification_id,user_id,channel,destination,status,attempt,max_attempts,
			scheduled_at,next_attempt_at,last_error,sent_at,created_at,updated_at
	`, item.NotificationID, item.UserID, item.Channel, item.Destination, item.Status, item.Attempt,
		item.MaxAttempts, item.ScheduledAt, item.NextAttemptAt, item.LastError, item.SentAt,
		item.CreatedAt, item.UpdatedAt,
	).Scan(
		&item.ID, &item.NotificationID, &item.UserID, &item.Channel, &item.Destination,
		&item.Status, &item.Attempt, &item.MaxAttempts, &item.ScheduledAt, &item.NextAttemptAt,
		&item.LastError, &item.SentAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return item, nil
	}
	return item, err
}

func (r *PostgresNotificationRepository) ClaimReadyDeliveries(now time.Time, limit int) ([]model.NotificationDelivery, error) {
	rows, err := r.db.Query(`
		SELECT id,notification_id,user_id,channel,destination,status,attempt,max_attempts,
		       scheduled_at,next_attempt_at,last_error,sent_at,created_at,updated_at
		FROM notification_deliveries
		WHERE status IN ('pending','failed')
		  AND COALESCE(next_attempt_at,scheduled_at) <= $1
		ORDER BY COALESCE(next_attempt_at,scheduled_at),id
		LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.NotificationDelivery, 0)
	for rows.Next() {
		var item model.NotificationDelivery
		if err := rows.Scan(&item.ID, &item.NotificationID, &item.UserID, &item.Channel,
			&item.Destination, &item.Status, &item.Attempt, &item.MaxAttempts, &item.ScheduledAt,
			&item.NextAttemptAt, &item.LastError, &item.SentAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresNotificationRepository) UpdateDelivery(item model.NotificationDelivery) (model.NotificationDelivery, error) {
	err := r.db.QueryRow(`
		UPDATE notification_deliveries
		SET status=$2,attempt=$3,next_attempt_at=$4,last_error=$5,sent_at=$6,updated_at=$7
		WHERE id=$1
		RETURNING id,notification_id,user_id,channel,destination,status,attempt,max_attempts,
			scheduled_at,next_attempt_at,last_error,sent_at,created_at,updated_at
	`, item.ID, item.Status, item.Attempt, item.NextAttemptAt, item.LastError, item.SentAt, item.UpdatedAt,
	).Scan(
		&item.ID, &item.NotificationID, &item.UserID, &item.Channel, &item.Destination,
		&item.Status, &item.Attempt, &item.MaxAttempts, &item.ScheduledAt, &item.NextAttemptAt,
		&item.LastError, &item.SentAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.NotificationDelivery{}, ErrNotificationNotFound
	}
	return item, err
}

func (r *PostgresNotificationRepository) ActiveTemplate(key, channel, locale string) (model.NotificationTemplate, error) {
	var item model.NotificationTemplate
	err := r.db.QueryRow(`
		SELECT id,template_key,channel,locale,version,subject,body,active,created_at
		FROM notification_templates
		WHERE template_key=$1 AND channel=$2 AND locale=$3 AND active=TRUE
		ORDER BY version DESC LIMIT 1
	`, key, channel, locale).Scan(
		&item.ID, &item.Key, &item.Channel, &item.Locale, &item.Version, &item.Subject,
		&item.Body, &item.Active, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.NotificationTemplate{}, ErrNotificationTemplateNotFound
	}
	return item, err
}

func (r *PostgresNotificationRepository) IsSuppressed(organizationID int64, eventType, channel string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM notification_suppression_rules
			WHERE organization_id=$1 AND active=TRUE
			  AND (event_type='*' OR event_type=$2)
			  AND (channel='*' OR channel=$3)
		)
	`, organizationID, eventType, channel).Scan(&exists)
	return exists, err
}

func (r *PostgresNotificationRepository) ListReminderCandidates(now, dueBefore time.Time, limit int) ([]model.ReminderCandidate, error) {
	rows, err := r.db.Query(`
		WITH recipients AS (
			SELECT t.id task_id,t.workspace_id,t.title,t.due_at,t.created_by_user_id user_id
			FROM tasks t
			WHERE t.due_at IS NOT NULL AND t.deleted_at IS NULL AND t.archived_at IS NULL
			  AND t.status NOT IN ('DONE','ARCHIVED')
			UNION
			SELECT t.id,t.workspace_id,t.title,t.due_at,a.user_id
			FROM tasks t JOIN task_assignees a ON a.task_id=t.id
			WHERE t.due_at IS NOT NULL AND t.deleted_at IS NULL AND t.archived_at IS NULL
			  AND t.status NOT IN ('DONE','ARCHIVED')
			UNION
			SELECT t.id,t.workspace_id,t.title,t.due_at,w.user_id
			FROM tasks t JOIN task_watchers w ON w.task_id=t.id
			WHERE t.due_at IS NOT NULL AND t.deleted_at IS NULL AND t.archived_at IS NULL
			  AND t.status NOT IN ('DONE','ARCHIVED')
		)
		SELECT task_id,workspace_id,user_id,title,due_at,
		       CASE WHEN due_at < $1 THEN 'overdue' ELSE 'due_soon' END
		FROM recipients
		WHERE due_at <= $2
		ORDER BY due_at,task_id,user_id
		LIMIT $3
	`, now, dueBefore, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.ReminderCandidate, 0)
	for rows.Next() {
		var item model.ReminderCandidate
		if err := rows.Scan(&item.TaskID, &item.WorkspaceID, &item.UserID, &item.Title, &item.DueAt, &item.Kind); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresNotificationRepository) ListOrganizationAdmins(organizationID int64) ([]int64, error) {
	rows, err := r.db.Query(`
		SELECT user_id FROM organization_members
		WHERE organization_id=$1 AND role IN ('owner','admin','delegated_admin')
		ORDER BY user_id
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		items = append(items, id)
	}
	return items, rows.Err()
}

func (r *PostgresNotificationRepository) Stats(userID int64, organizationID *int64) (model.NotificationStats, error) {
	var stats model.NotificationStats
	stats.ByChannel = map[string]int64{}
	stats.ByStatus = map[string]int64{}
	if err := r.db.QueryRow(`
		SELECT COUNT(*),COUNT(*) FILTER (WHERE read_at IS NULL)
		FROM notifications
		WHERE user_id=$1 AND ($2::bigint IS NULL OR organization_id=$2)
	`, userID, organizationID).Scan(&stats.Total, &stats.Unread); err != nil {
		return model.NotificationStats{}, err
	}
	rows, err := r.db.Query(`
		SELECT d.channel,d.status,COUNT(*)
		FROM notification_deliveries d
		JOIN notifications n ON n.id=d.notification_id
		WHERE d.user_id=$1 AND ($2::bigint IS NULL OR n.organization_id=$2)
		GROUP BY d.channel,d.status
	`, userID, organizationID)
	if err != nil {
		return model.NotificationStats{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var channel, status string
		var count int64
		if err := rows.Scan(&channel, &status, &count); err != nil {
			return model.NotificationStats{}, err
		}
		stats.ByChannel[channel] += count
		stats.ByStatus[status] += count
		if status == model.NotificationDeliveryDeadLetter {
			stats.DeadLettered += count
		}
		if status == model.NotificationDeliverySuppressed {
			stats.Suppressed += count
		}
	}
	return stats, rows.Err()
}

func (r *PostgresNotificationRepository) RecordNotificationAudit(organizationID *int64, userID *int64, action string, notificationID, deliveryID *int64, metadata map[string]any, at time.Time) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO notification_audit_events (
			organization_id,user_id,action,notification_id,delivery_id,metadata,created_at
		) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7)
	`, organizationID, userID, action, notificationID, deliveryID, string(raw), at)
	return err
}
