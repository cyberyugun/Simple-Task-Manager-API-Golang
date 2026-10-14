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

	AttachmentPreviewStatusNone       = "none"
	AttachmentPreviewStatusPending    = "pending"
	AttachmentPreviewStatusProcessing = "processing"
	AttachmentPreviewStatusReady      = "ready"
	AttachmentPreviewStatusFailed     = "failed"

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
	Encryption           string     `json:"encryption,omitempty"`
	EncryptionKeyID      string     `json:"encryption_key_id,omitempty"`
	Version              int        `json:"version"`
	VersionGroupID       string     `json:"version_group_id"`
	PreviousAttachmentID *int64     `json:"previous_attachment_id,omitempty"`
	LegalHold            bool       `json:"legal_hold"`
	RetainUntil          *time.Time `json:"retain_until,omitempty"`
	QuarantineReason     string     `json:"quarantine_reason,omitempty"`
	ReleasedByUserID     *int64     `json:"released_by_user_id,omitempty"`
	ReleasedAt           *time.Time `json:"released_at,omitempty"`
	ReleaseNote          string     `json:"release_note,omitempty"`
	PreviewStatus        string     `json:"preview_status"`
	PreviewObjectKey     string     `json:"preview_object_key,omitempty"`
	PreviewContentType   string     `json:"preview_content_type,omitempty"`
	PreviewWidth         int        `json:"preview_width,omitempty"`
	PreviewHeight        int        `json:"preview_height,omitempty"`
	PreviewEngine        string     `json:"preview_engine,omitempty"`
	PreviewMessage       string     `json:"preview_message,omitempty"`
	PreviewCreatedAt     *time.Time `json:"preview_created_at,omitempty"`
	UploadedAt           *time.Time `json:"uploaded_at,omitempty"`
	ScannedAt            *time.Time `json:"scanned_at,omitempty"`
	DeletedAt            *time.Time `json:"deleted_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
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


type AttachmentVersionUploadRequest struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
}

type AttachmentQuarantineRequest struct {
	Reason string `json:"reason"`
}

type AttachmentReleaseRequest struct {
	Note string `json:"note"`
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


type AttachmentPreviewResult struct {
	ObjectKey   string `json:"object_key"`
	ContentType string `json:"content_type"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Engine      string `json:"engine"`
	Message     string `json:"message,omitempty"`
}

type AttachmentPreviewDownload struct {
	Attachment Attachment `json:"attachment"`
	PreviewURL string     `json:"preview_url"`
	ExpiresAt  time.Time  `json:"expires_at"`
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
