package repository

import (
	"database/sql"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresAttachmentRepository struct{ db *sql.DB }

func NewPostgresAttachmentRepository(db *sql.DB) *PostgresAttachmentRepository {
	return &PostgresAttachmentRepository{db: db}
}

const attachmentColumns = `id,workspace_id,task_id,comment_id,uploaded_by_user_id,provider,bucket,object_key,file_name,content_type,size_bytes,sha256,status,scan_engine,scan_message,encryption,encryption_key_id,version,legal_hold,retain_until,uploaded_at,scanned_at,deleted_at,created_at,updated_at`

type attachmentScanner interface{ Scan(...any) error }

func scanAttachment(s attachmentScanner) (model.Attachment, error) {
	var a model.Attachment
	err := s.Scan(
		&a.ID, &a.WorkspaceID, &a.TaskID, &a.CommentID, &a.UploadedByUserID, &a.Provider, &a.Bucket,
		&a.ObjectKey, &a.FileName, &a.ContentType, &a.SizeBytes, &a.SHA256, &a.Status, &a.ScanEngine,
		&a.ScanMessage, &a.Encryption, &a.EncryptionKeyID, &a.Version, &a.LegalHold, &a.RetainUntil,
		&a.UploadedAt, &a.ScannedAt, &a.DeletedAt, &a.CreatedAt, &a.UpdatedAt,
	)
	return a, err
}

func (r *PostgresAttachmentRepository) CreateAttachment(a model.Attachment) (model.Attachment, error) {
	return scanAttachment(r.db.QueryRow(`INSERT INTO attachments
		(workspace_id,task_id,comment_id,uploaded_by_user_id,provider,bucket,object_key,file_name,content_type,size_bytes,sha256,status,scan_engine,scan_message,encryption,encryption_key_id,version,legal_hold,retain_until,uploaded_at,scanned_at,deleted_at,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
		RETURNING `+attachmentColumns,
		a.WorkspaceID, a.TaskID, a.CommentID, a.UploadedByUserID, a.Provider, a.Bucket, a.ObjectKey, a.FileName, a.ContentType, a.SizeBytes, a.SHA256, a.Status, a.ScanEngine, a.ScanMessage, a.Encryption, a.EncryptionKeyID, a.Version, a.LegalHold, a.RetainUntil, a.UploadedAt, a.ScannedAt, a.DeletedAt, a.CreatedAt, a.UpdatedAt))
}

func (r *PostgresAttachmentRepository) UpdateAttachment(a model.Attachment) (model.Attachment, error) {
	out, err := scanAttachment(r.db.QueryRow(`UPDATE attachments SET
		task_id=$3,comment_id=$4,provider=$5,bucket=$6,object_key=$7,file_name=$8,content_type=$9,size_bytes=$10,sha256=$11,status=$12,scan_engine=$13,scan_message=$14,encryption=$15,encryption_key_id=$16,version=$17,legal_hold=$18,retain_until=$19,uploaded_at=$20,scanned_at=$21,deleted_at=$22,updated_at=$23
		WHERE workspace_id=$1 AND id=$2
		RETURNING `+attachmentColumns,
		a.WorkspaceID, a.ID, a.TaskID, a.CommentID, a.Provider, a.Bucket, a.ObjectKey, a.FileName, a.ContentType, a.SizeBytes, a.SHA256, a.Status, a.ScanEngine, a.ScanMessage, a.Encryption, a.EncryptionKeyID, a.Version, a.LegalHold, a.RetainUntil, a.UploadedAt, a.ScannedAt, a.DeletedAt, a.UpdatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Attachment{}, ErrAttachmentNotFound
	}
	return out, err
}

func (r *PostgresAttachmentRepository) GetAttachment(workspaceID, attachmentID int64) (model.Attachment, error) {
	out, err := scanAttachment(r.db.QueryRow(`SELECT `+attachmentColumns+` FROM attachments WHERE workspace_id=$1 AND id=$2`, workspaceID, attachmentID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Attachment{}, ErrAttachmentNotFound
	}
	return out, err
}

func (r *PostgresAttachmentRepository) ListAttachments(workspaceID int64, taskID, commentID *int64) ([]model.Attachment, error) {
	rows, err := r.db.Query(`SELECT `+attachmentColumns+` FROM attachments
		WHERE workspace_id=$1 AND deleted_at IS NULL
		  AND ($2::bigint IS NULL OR task_id=$2)
		  AND ($3::bigint IS NULL OR comment_id=$3)
		ORDER BY id`, workspaceID, taskID, commentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Attachment{}
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *PostgresAttachmentRepository) ListPendingScans(limit int) ([]model.Attachment, error) {
	rows, err := r.db.Query(`SELECT `+attachmentColumns+` FROM attachments
		WHERE status IN ('uploaded','scanning') ORDER BY id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Attachment{}
	for rows.Next() {
		a, e := scanAttachment(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *PostgresAttachmentRepository) ListRetentionCandidates(now time.Time, limit int) ([]model.Attachment, error) {
	rows, err := r.db.Query(`SELECT `+attachmentColumns+` FROM attachments
		WHERE deleted_at IS NULL AND legal_hold=FALSE AND retain_until IS NOT NULL AND retain_until <= $1
		ORDER BY retain_until,id LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Attachment{}
	for rows.Next() {
		a, e := scanAttachment(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *PostgresAttachmentRepository) FindCleanByHash(workspaceID int64, sha256 string, sizeBytes int64) (model.Attachment, error) {
	out, err := scanAttachment(r.db.QueryRow(`SELECT `+attachmentColumns+` FROM attachments
		WHERE workspace_id=$1 AND sha256=$2 AND size_bytes=$3 AND status='clean' AND deleted_at IS NULL
		ORDER BY id LIMIT 1`, workspaceID, sha256, sizeBytes))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Attachment{}, ErrAttachmentNotFound
	}
	return out, err
}

func (r *PostgresAttachmentRepository) UsageBytes(workspaceID int64) (int64, error) {
	var total int64
	err := r.db.QueryRow(`SELECT COALESCE(SUM(size_bytes),0) FROM attachments WHERE workspace_id=$1 AND deleted_at IS NULL`, workspaceID).Scan(&total)
	return total, err
}
