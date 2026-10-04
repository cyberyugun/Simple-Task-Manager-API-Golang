package model

import "time"

const (
	AttachmentStatusPending    = "upload_pending"
	AttachmentStatusUploaded   = "uploaded"
	AttachmentStatusScanning   = "scanning"
	AttachmentStatusClean      = "clean"
	AttachmentStatusInfected   = "infected"
	AttachmentStatusQuarantine = "quarantined"
	AttachmentStatusDeleted    = "deleted"

	AttachmentProviderS3           = "s3"
	AttachmentProviderS3Compatible = "s3_compatible"
	AttachmentProviderAzureBlob    = "azure_blob"
	AttachmentProviderGCS          = "gcs"
	AttachmentProviderDevelopment  = "development"
)

type Attachment struct {
	ID               int64      `json:"id"`
	WorkspaceID      int64      `json:"workspace_id"`
	TaskID           *int64     `json:"task_id,omitempty"`
	CommentID        *int64     `json:"comment_id,omitempty"`
	UploadedByUserID int64      `json:"uploaded_by_user_id"`
	Provider         string     `json:"provider"`
	Bucket           string     `json:"bucket"`
	ObjectKey        string     `json:"object_key"`
	FileName         string     `json:"file_name"`
	ContentType      string     `json:"content_type"`
	SizeBytes        int64      `json:"size_bytes"`
	SHA256           string     `json:"sha256"`
	Status           string     `json:"status"`
	ScanEngine       string     `json:"scan_engine,omitempty"`
	ScanMessage      string     `json:"scan_message,omitempty"`
	Encryption       string     `json:"encryption,omitempty"`
	EncryptionKeyID  string     `json:"encryption_key_id,omitempty"`
	Version          int        `json:"version"`
	LegalHold        bool       `json:"legal_hold"`
	RetainUntil      *time.Time `json:"retain_until,omitempty"`
	UploadedAt       *time.Time `json:"uploaded_at,omitempty"`
	ScannedAt        *time.Time `json:"scanned_at,omitempty"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type AttachmentUploadRequest struct {
	TaskID      *int64 `json:"task_id,omitempty"`
	CommentID   *int64 `json:"comment_id,omitempty"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
}

type CompleteAttachmentUploadRequest struct {
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

type AttachmentUploadSession struct {
	Attachment Attachment        `json:"attachment"`
	UploadURL  string            `json:"upload_url"`
	Method     string            `json:"method"`
	Headers    map[string]string `json:"headers,omitempty"`
	ExpiresAt  time.Time         `json:"expires_at"`
}

type AttachmentDownload struct {
	Attachment  Attachment `json:"attachment"`
	DownloadURL string     `json:"download_url"`
	ExpiresAt   time.Time  `json:"expires_at"`
}

type AttachmentScanResult struct {
	Clean   bool   `json:"clean"`
	Engine  string `json:"engine"`
	Message string `json:"message,omitempty"`
}

type AttachmentUsage struct {
	WorkspaceID int64 `json:"workspace_id"`
	UsedBytes   int64 `json:"used_bytes"`
	QuotaBytes  int64 `json:"quota_bytes"`
}

type UpdateAttachmentGovernanceRequest struct {
	LegalHold   *bool      `json:"legal_hold,omitempty"`
	RetainUntil *time.Time `json:"retain_until,omitempty"`
}
