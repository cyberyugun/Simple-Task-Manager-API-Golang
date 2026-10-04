package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrInvalidAttachment          = errors.New("invalid attachment")
	ErrAttachmentQuotaExceeded    = errors.New("attachment quota exceeded")
	ErrAttachmentNotReady         = errors.New("attachment is not ready")
	ErrAttachmentBlocked          = errors.New("attachment is blocked")
	ErrAttachmentGovernanceDenied = errors.New("attachment governance action is forbidden")
)

type AttachmentConfig struct {
	Provider             string
	Bucket               string
	BaseURL              string
	SigningSecret        string
	MaxFileBytes         int64
	WorkspaceQuotaBytes  int64
	PresignTTL           time.Duration
	Deduplicate          bool
	Encryption           string
	EncryptionKeyID      string
	AllowInsecure        bool
	ScannerURL           string
	ScannerBearerToken   string
	ScannerSigningSecret string
	ScannerTimeout       time.Duration
	ScannerRequired      bool
}

type ObjectStore interface {
	PresignUpload(objectKey, contentType string, sizeBytes int64, expiresAt time.Time) (string, map[string]string, error)
	PresignDownload(objectKey string, expiresAt time.Time) (string, error)
	Delete(context.Context, string) error
}

type AttachmentScanner interface {
	Scan(context.Context, model.Attachment) (model.AttachmentScanResult, error)
}

type SignedObjectStore struct {
	baseURL       *url.URL
	provider      string
	bucket        string
	signingSecret []byte
}

func NewSignedObjectStore(cfg AttachmentConfig) (*SignedObjectStore, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = "https://storage.invalid"
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(cfg.AllowInsecure && u.Scheme == "http")) {
		return nil, errors.New("invalid attachment storage base url")
	}
	secret := strings.TrimSpace(cfg.SigningSecret)
	if len(secret) < 16 {
		return nil, errors.New("attachment signing secret must be at least 16 characters")
	}
	return &SignedObjectStore{
		baseURL: u, provider: cfg.Provider, bucket: cfg.Bucket, signingSecret: []byte(secret),
	}, nil
}

func (s *SignedObjectStore) signedURL(operation, objectKey string, expiresAt time.Time) string {
	u := *s.baseURL
	u.Path = path.Join(u.Path, s.bucket, objectKey)
	q := u.Query()
	q.Set("op", operation)
	q.Set("expires", fmt.Sprint(expiresAt.Unix()))
	mac := hmac.New(sha256.New, s.signingSecret)
	_, _ = mac.Write([]byte(operation + "\n" + objectKey + "\n" + fmt.Sprint(expiresAt.Unix())))
	q.Set("signature", hex.EncodeToString(mac.Sum(nil)))
	if s.provider != "" {
		q.Set("provider", s.provider)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *SignedObjectStore) PresignUpload(objectKey, contentType string, sizeBytes int64, expiresAt time.Time) (string, map[string]string, error) {
	return s.signedURL("upload", objectKey, expiresAt), map[string]string{
		"Content-Type":     contentType,
		"X-Content-Length": fmt.Sprint(sizeBytes),
	}, nil
}

func (s *SignedObjectStore) PresignDownload(objectKey string, expiresAt time.Time) (string, error) {
	return s.signedURL("download", objectKey, expiresAt), nil
}

func (s *SignedObjectStore) Delete(context.Context, string) error { return nil }

type NoopAttachmentScanner struct{}

func (NoopAttachmentScanner) Scan(_ context.Context, attachment model.Attachment) (model.AttachmentScanResult, error) {
	return model.AttachmentScanResult{Clean: true, Engine: "noop", Message: "scanner hook completed"}, nil
}

type AttachmentService struct {
	repo       repository.AttachmentRepository
	tasks      repository.TaskRepository
	collab     repository.TaskCollaborationRepository
	workspaces repository.WorkspaceRepository
	store      ObjectStore
	scanner    AttachmentScanner
	cfg        AttachmentConfig
}

func NewAttachmentService(
	repo repository.AttachmentRepository,
	tasks repository.TaskRepository,
	collab repository.TaskCollaborationRepository,
	workspaces repository.WorkspaceRepository,
	store ObjectStore,
	scanner AttachmentScanner,
	cfg AttachmentConfig,
) *AttachmentService {
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = 25 << 20
	}
	if cfg.WorkspaceQuotaBytes <= 0 {
		cfg.WorkspaceQuotaBytes = 1 << 30
	}
	if cfg.PresignTTL <= 0 {
		cfg.PresignTTL = 15 * time.Minute
	}
	if cfg.Provider == "" {
		cfg.Provider = model.AttachmentProviderDevelopment
	}
	if scanner == nil {
		scanner = NoopAttachmentScanner{}
	}
	return &AttachmentService{repo: repo, tasks: tasks, collab: collab, workspaces: workspaces, store: store, scanner: scanner, cfg: cfg}
}

func (s *AttachmentService) CreateUpload(actorUserID int64, access model.WorkspaceAccess, req model.AttachmentUploadRequest) (model.AttachmentUploadSession, error) {
	fileName := strings.TrimSpace(req.FileName)
	contentType := strings.ToLower(strings.TrimSpace(req.ContentType))
	hash := strings.ToLower(strings.TrimSpace(req.SHA256))
	if req.TaskID == nil || *req.TaskID <= 0 || fileName == "" || len(fileName) > 255 ||
		req.SizeBytes <= 0 || req.SizeBytes > s.cfg.MaxFileBytes || !validSHA256(hash) || !allowedAttachmentMIME(contentType) {
		return model.AttachmentUploadSession{}, ErrInvalidAttachment
	}
	if _, err := s.tasks.FindByID(access.ID, *req.TaskID); err != nil {
		return model.AttachmentUploadSession{}, err
	}
	if req.CommentID != nil {
		if *req.CommentID <= 0 {
			return model.AttachmentUploadSession{}, ErrInvalidAttachment
		}
		comments, err := s.collab.ListComments(access.ID, *req.TaskID)
		if err != nil {
			return model.AttachmentUploadSession{}, err
		}
		found := false
		for _, comment := range comments {
			if comment.ID == *req.CommentID && comment.DeletedAt == nil {
				found = true
				break
			}
		}
		if !found {
			return model.AttachmentUploadSession{}, repository.ErrTaskCommentNotFound
		}
	}
	used, err := s.repo.UsageBytes(access.ID)
	if err != nil {
		return model.AttachmentUploadSession{}, err
	}
	if used+req.SizeBytes > s.cfg.WorkspaceQuotaBytes {
		return model.AttachmentUploadSession{}, ErrAttachmentQuotaExceeded
	}
	now := time.Now().UTC()
	if s.cfg.Deduplicate {
		if existing, err := s.repo.FindCleanByHash(access.ID, hash, req.SizeBytes); err == nil {
			item := existing
			item.ID = 0
			item.TaskID = req.TaskID
			item.CommentID = req.CommentID
			item.UploadedByUserID = actorUserID
			item.FileName = fileName
			item.Version++
			item.CreatedAt = now
			item.UpdatedAt = now
			item.UploadedAt = &now
			item.ScannedAt = &now
			item, err = s.repo.CreateAttachment(item)
			if err != nil {
				return model.AttachmentUploadSession{}, err
			}
			s.audit(actorUserID, access.ID, "attachment.deduplicated", item.ID, map[string]any{"source_attachment_id": existing.ID})
			return model.AttachmentUploadSession{Attachment: item, Method: "DEDUPLICATED", ExpiresAt: now}, nil
		}
	}
	objectKey := fmt.Sprintf("workspaces/%d/%s/%d-%s", access.ID, hash[:12], now.UnixNano(), sanitizeAttachmentName(fileName))
	item, err := s.repo.CreateAttachment(model.Attachment{
		WorkspaceID: access.ID, TaskID: req.TaskID, CommentID: req.CommentID, UploadedByUserID: actorUserID,
		Provider: s.cfg.Provider, Bucket: s.cfg.Bucket, ObjectKey: objectKey, FileName: fileName,
		ContentType: contentType, SizeBytes: req.SizeBytes, SHA256: hash, Status: model.AttachmentStatusPending,
		Encryption: s.cfg.Encryption, EncryptionKeyID: s.cfg.EncryptionKeyID, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return model.AttachmentUploadSession{}, err
	}
	expires := now.Add(s.cfg.PresignTTL)
	uploadURL, headers, err := s.store.PresignUpload(objectKey, contentType, req.SizeBytes, expires)
	if err != nil {
		return model.AttachmentUploadSession{}, err
	}
	s.audit(actorUserID, access.ID, "attachment.upload_requested", item.ID, map[string]any{"size_bytes": req.SizeBytes, "content_type": contentType})
	return model.AttachmentUploadSession{Attachment: item, UploadURL: uploadURL, Method: "PUT", Headers: headers, ExpiresAt: expires}, nil
}

func (s *AttachmentService) CompleteUpload(actorUserID int64, access model.WorkspaceAccess, attachmentID int64, req model.CompleteAttachmentUploadRequest) (model.Attachment, error) {
	item, err := s.repo.GetAttachment(access.ID, attachmentID)
	if err != nil {
		return model.Attachment{}, err
	}
	if item.UploadedByUserID != actorUserID && access.Role == model.WorkspaceRoleMember {
		return model.Attachment{}, ErrAttachmentGovernanceDenied
	}
	if item.Status != model.AttachmentStatusPending || req.SizeBytes != item.SizeBytes || strings.ToLower(strings.TrimSpace(req.SHA256)) != item.SHA256 {
		return model.Attachment{}, ErrInvalidAttachment
	}
	now := time.Now().UTC()
	item.Status = model.AttachmentStatusUploaded
	item.UploadedAt = &now
	item.UpdatedAt = now
	updated, err := s.repo.UpdateAttachment(item)
	if err == nil {
		s.audit(actorUserID, access.ID, "attachment.upload_completed", item.ID, nil)
	}
	return updated, err
}

func (s *AttachmentService) List(access model.WorkspaceAccess, taskID, commentID *int64) ([]model.Attachment, error) {
	return s.repo.ListAttachments(access.ID, taskID, commentID)
}

func (s *AttachmentService) Download(actorUserID int64, access model.WorkspaceAccess, attachmentID int64) (model.AttachmentDownload, error) {
	item, err := s.repo.GetAttachment(access.ID, attachmentID)
	if err != nil {
		return model.AttachmentDownload{}, err
	}
	if item.Status != model.AttachmentStatusClean {
		if item.Status == model.AttachmentStatusInfected || item.Status == model.AttachmentStatusQuarantine {
			return model.AttachmentDownload{}, ErrAttachmentBlocked
		}
		return model.AttachmentDownload{}, ErrAttachmentNotReady
	}
	expires := time.Now().UTC().Add(s.cfg.PresignTTL)
	u, err := s.store.PresignDownload(item.ObjectKey, expires)
	if err != nil {
		return model.AttachmentDownload{}, err
	}
	s.audit(actorUserID, access.ID, "attachment.download_requested", item.ID, nil)
	return model.AttachmentDownload{Attachment: item, DownloadURL: u, ExpiresAt: expires}, nil
}

func (s *AttachmentService) Delete(actorUserID int64, access model.WorkspaceAccess, attachmentID int64) (model.Attachment, error) {
	item, err := s.repo.GetAttachment(access.ID, attachmentID)
	if err != nil {
		return model.Attachment{}, err
	}
	if item.LegalHold || (item.RetainUntil != nil && item.RetainUntil.After(time.Now().UTC())) {
		return model.Attachment{}, ErrAttachmentGovernanceDenied
	}
	if item.UploadedByUserID != actorUserID && access.Role == model.WorkspaceRoleMember {
		return model.Attachment{}, ErrAttachmentGovernanceDenied
	}
	if err := s.store.Delete(context.Background(), item.ObjectKey); err != nil {
		return model.Attachment{}, err
	}
	now := time.Now().UTC()
	item.Status = model.AttachmentStatusDeleted
	item.DeletedAt = &now
	item.UpdatedAt = now
	updated, err := s.repo.UpdateAttachment(item)
	if err == nil {
		s.audit(actorUserID, access.ID, "attachment.deleted", item.ID, nil)
	}
	return updated, err
}

func (s *AttachmentService) Governance(actorUserID int64, access model.WorkspaceAccess, attachmentID int64, req model.UpdateAttachmentGovernanceRequest) (model.Attachment, error) {
	if access.Role != model.WorkspaceRoleOwner && access.Role != model.WorkspaceRoleAdmin {
		return model.Attachment{}, ErrAttachmentGovernanceDenied
	}
	item, err := s.repo.GetAttachment(access.ID, attachmentID)
	if err != nil {
		return model.Attachment{}, err
	}
	if req.LegalHold != nil {
		item.LegalHold = *req.LegalHold
	}
	item.RetainUntil = req.RetainUntil
	item.UpdatedAt = time.Now().UTC()
	updated, err := s.repo.UpdateAttachment(item)
	if err == nil {
		s.audit(actorUserID, access.ID, "attachment.governance_updated", item.ID, map[string]any{"legal_hold": item.LegalHold, "retain_until": item.RetainUntil})
	}
	return updated, err
}

func (s *AttachmentService) Usage(access model.WorkspaceAccess) (model.AttachmentUsage, error) {
	used, err := s.repo.UsageBytes(access.ID)
	return model.AttachmentUsage{WorkspaceID: access.ID, UsedBytes: used, QuotaBytes: s.cfg.WorkspaceQuotaBytes}, err
}

func (s *AttachmentService) ProcessPendingScans(limit int) ([]model.Attachment, error) {
	if limit <= 0 {
		limit = 50
	}
	items, err := s.repo.ListPendingScans(limit)
	if err != nil {
		return nil, err
	}
	out := make([]model.Attachment, 0, len(items))
	for _, item := range items {
		now := time.Now().UTC()
		item.Status = model.AttachmentStatusScanning
		item.UpdatedAt = now
		item, err = s.repo.UpdateAttachment(item)
		if err != nil {
			return nil, err
		}
		result, scanErr := s.scanner.Scan(context.Background(), item)
		now = time.Now().UTC()
		item.ScannedAt = &now
		item.ScanEngine = result.Engine
		item.ScanMessage = result.Message
		if scanErr != nil {
			item.Status = model.AttachmentStatusQuarantine
			item.ScanMessage = strings.TrimSpace(result.Message + " " + scanErr.Error())
		} else if result.Clean {
			item.Status = model.AttachmentStatusClean
		} else {
			item.Status = model.AttachmentStatusInfected
		}
		item.UpdatedAt = now
		item, err = s.repo.UpdateAttachment(item)
		if err != nil {
			return nil, err
		}
		s.audit(0, item.WorkspaceID, "attachment.scan_completed", item.ID, map[string]any{"status": item.Status, "engine": item.ScanEngine})
		out = append(out, item)
	}
	return out, nil
}

func (s *AttachmentService) ProcessRetention(limit int) ([]model.Attachment, error) {
	if limit <= 0 {
		limit = 50
	}
	items, err := s.repo.ListRetentionCandidates(time.Now().UTC(), limit)
	if err != nil {
		return nil, err
	}
	out := make([]model.Attachment, 0, len(items))
	for _, item := range items {
		if err := s.store.Delete(context.Background(), item.ObjectKey); err != nil {
			continue
		}
		now := time.Now().UTC()
		item.Status = model.AttachmentStatusDeleted
		item.DeletedAt = &now
		item.UpdatedAt = now
		updated, err := s.repo.UpdateAttachment(item)
		if err != nil {
			return nil, err
		}
		s.audit(0, item.WorkspaceID, "attachment.retention_deleted", item.ID, nil)
		out = append(out, updated)
	}
	return out, nil
}

func (s *AttachmentService) audit(actorUserID, workspaceID int64, action string, attachmentID int64, metadata map[string]any) {
	workspace := workspaceID
	var actor *int64
	if actorUserID > 0 {
		actor = &actorUserID
	}
	_ = s.workspaces.RecordAudit(model.AuditEvent{
		WorkspaceID: &workspace, ActorUserID: actor, Action: action, ResourceType: "attachment",
		ResourceID: fmt.Sprint(attachmentID), Metadata: metadata, CreatedAt: time.Now().UTC(),
	})
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func allowedAttachmentMIME(value string) bool {
	if value == "" || value == "application/x-msdownload" || value == "application/x-dosexec" ||
		value == "application/x-sh" || value == "application/x-executable" {
		return false
	}
	return strings.Contains(value, "/") && len(value) <= 120
}

func sanitizeAttachmentName(value string) string {
	value = path.Base(strings.ReplaceAll(value, "\\", "/"))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._")
	if out == "" {
		return "file"
	}
	return out
}
