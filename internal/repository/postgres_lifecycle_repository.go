package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresLifecycleRepository struct {
	db *sql.DB
}

func NewPostgresLifecycleRepository(db *sql.DB) *PostgresLifecycleRepository {
	return &PostgresLifecycleRepository{db: db}
}

func (r *PostgresLifecycleRepository) ListConfiguredWorkspaceIDs() ([]int64, error) {
	rows, err := r.db.Query(`SELECT workspace_id FROM workspace_governance_policies ORDER BY workspace_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *PostgresLifecycleRepository) CreateLifecycleRun(run model.LifecycleRun) (model.LifecycleRun, error) {
	var item model.LifecycleRun
	err := r.db.QueryRow(`
		INSERT INTO governance_lifecycle_runs (
			workspace_id, status, archived_count, purged_count, skipped_reason,
			triggered_by_user_id, started_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, workspace_id, status, archived_count, purged_count, skipped_reason,
			triggered_by_user_id, started_at, finished_at
	`, run.WorkspaceID, run.Status, run.ArchivedCount, run.PurgedCount, run.SkippedReason,
		run.TriggeredByUserID, run.StartedAt).
		Scan(&item.ID, &item.WorkspaceID, &item.Status, &item.ArchivedCount, &item.PurgedCount,
			&item.SkippedReason, &item.TriggeredByUserID, &item.StartedAt, &item.FinishedAt)
	return item, err
}

func (r *PostgresLifecycleRepository) FinishLifecycleRun(runID int64, status string, archived, purged int, skippedReason string, finishedAt time.Time) (model.LifecycleRun, error) {
	var item model.LifecycleRun
	err := r.db.QueryRow(`
		UPDATE governance_lifecycle_runs
		SET status = $2, archived_count = $3, purged_count = $4, skipped_reason = $5, finished_at = $6
		WHERE id = $1
		RETURNING id, workspace_id, status, archived_count, purged_count, skipped_reason,
			triggered_by_user_id, started_at, finished_at
	`, runID, status, archived, purged, skippedReason, finishedAt).
		Scan(&item.ID, &item.WorkspaceID, &item.Status, &item.ArchivedCount, &item.PurgedCount,
			&item.SkippedReason, &item.TriggeredByUserID, &item.StartedAt, &item.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.LifecycleRun{}, ErrLifecycleRunNotFound
	}
	return item, err
}

func (r *PostgresLifecycleRepository) ListLifecycleRuns(workspaceID int64, limit int) ([]model.LifecycleRun, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, status, archived_count, purged_count, skipped_reason,
			triggered_by_user_id, started_at, finished_at
		FROM governance_lifecycle_runs
		WHERE workspace_id = $1
		ORDER BY started_at DESC, id DESC
		LIMIT $2
	`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LifecycleRun, 0)
	for rows.Next() {
		var item model.LifecycleRun
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Status, &item.ArchivedCount, &item.PurgedCount,
			&item.SkippedReason, &item.TriggeredByUserID, &item.StartedAt, &item.FinishedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresLifecycleRepository) ArchiveTask(task model.Task, archivedAt time.Time) error {
	raw, err := json.Marshal(task)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO archived_tasks (workspace_id, task_id, snapshot, archived_at)
		VALUES ($1,$2,$3::jsonb,$4)
		ON CONFLICT (workspace_id, task_id) DO NOTHING
	`, task.WorkspaceID, task.ID, string(raw), archivedAt)
	return err
}

func (r *PostgresLifecycleRepository) PurgeArchivedTasks(workspaceID int64, cutoff time.Time) (int, error) {
	result, err := r.db.Exec(`
		DELETE FROM archived_tasks
		WHERE workspace_id = $1 AND archived_at < $2
	`, workspaceID, cutoff)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	return int(count), err
}

func (r *PostgresLifecycleRepository) SavePrivacyExport(pkg model.PrivacyExportPackage) (model.PrivacyExportPackage, error) {
	raw, err := json.Marshal(pkg.Payload)
	if err != nil {
		return model.PrivacyExportPackage{}, err
	}
	var item model.PrivacyExportPackage
	var payload []byte
	err = r.db.QueryRow(`
		INSERT INTO privacy_export_packages (
			workspace_id, privacy_request_id, subject_user_id, checksum_sha256,
			payload, created_by_user_id, created_at
		) VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7)
		RETURNING id, workspace_id, privacy_request_id, subject_user_id, checksum_sha256,
			payload, created_by_user_id, created_at
	`, pkg.WorkspaceID, pkg.PrivacyRequestID, pkg.SubjectUserID, pkg.ChecksumSHA256,
		string(raw), pkg.CreatedByUserID, pkg.CreatedAt).
		Scan(&item.ID, &item.WorkspaceID, &item.PrivacyRequestID, &item.SubjectUserID,
			&item.ChecksumSHA256, &payload, &item.CreatedByUserID, &item.CreatedAt)
	if err != nil {
		return model.PrivacyExportPackage{}, err
	}
	if err := json.Unmarshal(payload, &item.Payload); err != nil {
		return model.PrivacyExportPackage{}, err
	}
	return item, nil
}

func (r *PostgresLifecycleRepository) AddConsent(record model.ConsentRecord) (model.ConsentRecord, error) {
	var item model.ConsentRecord
	err := r.db.QueryRow(`
		INSERT INTO consent_ledger (
			workspace_id, subject_user_id, purpose, status, policy_version,
			source, recorded_by_user_id, recorded_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, workspace_id, subject_user_id, purpose, status, policy_version,
			source, recorded_by_user_id, recorded_at
	`, record.WorkspaceID, record.SubjectUserID, record.Purpose, record.Status,
		record.PolicyVersion, record.Source, record.RecordedByUserID, record.RecordedAt).
		Scan(&item.ID, &item.WorkspaceID, &item.SubjectUserID, &item.Purpose, &item.Status,
			&item.PolicyVersion, &item.Source, &item.RecordedByUserID, &item.RecordedAt)
	return item, err
}

func (r *PostgresLifecycleRepository) ListConsents(workspaceID int64) ([]model.ConsentRecord, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, subject_user_id, purpose, status, policy_version,
			source, recorded_by_user_id, recorded_at
		FROM consent_ledger
		WHERE workspace_id = $1
		ORDER BY recorded_at DESC, id DESC
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.ConsentRecord, 0)
	for rows.Next() {
		var item model.ConsentRecord
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.SubjectUserID, &item.Purpose, &item.Status,
			&item.PolicyVersion, &item.Source, &item.RecordedByUserID, &item.RecordedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
