package repository

import (
	"errors"
	"sort"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var ErrAttachmentNotFound = errors.New("attachment not found")

type AttachmentRepository interface {
	CreateAttachment(model.Attachment) (model.Attachment, error)
	UpdateAttachment(model.Attachment) (model.Attachment, error)
	GetAttachment(workspaceID, attachmentID int64) (model.Attachment, error)
	ListAttachments(workspaceID int64, taskID, commentID *int64) ([]model.Attachment, error)
	ListAttachmentVersions(workspaceID int64, versionGroupID string) ([]model.Attachment, error)
	ListPendingScans(limit int) ([]model.Attachment, error)
	ListPendingPreviews(limit int) ([]model.Attachment, error)
	ListRetentionCandidates(now time.Time, limit int) ([]model.Attachment, error)
	FindCleanByHash(workspaceID int64, sha256 string, sizeBytes int64) (model.Attachment, error)
	CountObjectReferences(workspaceID int64, objectKey string, excludeAttachmentID int64) (int, error)
	UsageBytes(workspaceID int64) (int64, error)
}

type InMemoryAttachmentRepository struct {
	mu     sync.Mutex
	items  map[int64]model.Attachment
	nextID int64
}

func NewInMemoryAttachmentRepository() *InMemoryAttachmentRepository {
	return &InMemoryAttachmentRepository{items: map[int64]model.Attachment{}, nextID: 1}
}

func (r *InMemoryAttachmentRepository) CreateAttachment(item model.Attachment) (model.Attachment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextID
	r.nextID++
	if item.PreviewStatus == "" {
		item.PreviewStatus = model.AttachmentPreviewStatusNone
	}
	r.items[item.ID] = cloneAttachment(item)
	return cloneAttachment(item), nil
}

func (r *InMemoryAttachmentRepository) UpdateAttachment(item model.Attachment) (model.Attachment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.items[item.ID]
	if !ok || old.WorkspaceID != item.WorkspaceID {
		return model.Attachment{}, ErrAttachmentNotFound
	}
	item.CreatedAt = old.CreatedAt
	r.items[item.ID] = cloneAttachment(item)
	return cloneAttachment(item), nil
}

func (r *InMemoryAttachmentRepository) GetAttachment(workspaceID, attachmentID int64) (model.Attachment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[attachmentID]
	if !ok || item.WorkspaceID != workspaceID {
		return model.Attachment{}, ErrAttachmentNotFound
	}
	return cloneAttachment(item), nil
}

func (r *InMemoryAttachmentRepository) ListAttachments(workspaceID int64, taskID, commentID *int64) ([]model.Attachment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.Attachment, 0)
	for _, item := range r.items {
		if item.WorkspaceID != workspaceID || item.Status == model.AttachmentStatusDeleted {
			continue
		}
		if taskID != nil && (item.TaskID == nil || *item.TaskID != *taskID) {
			continue
		}
		if commentID != nil && (item.CommentID == nil || *item.CommentID != *commentID) {
			continue
		}
		out = append(out, cloneAttachment(item))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *InMemoryAttachmentRepository) ListAttachmentVersions(workspaceID int64, versionGroupID string) ([]model.Attachment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.Attachment, 0)
	for _, item := range r.items {
		if item.WorkspaceID != workspaceID || item.VersionGroupID != versionGroupID {
			continue
		}
		out = append(out, cloneAttachment(item))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Version == out[j].Version {
			return out[i].ID < out[j].ID
		}
		return out[i].Version < out[j].Version
	})
	return out, nil
}

func (r *InMemoryAttachmentRepository) ListPendingScans(limit int) ([]model.Attachment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.Attachment, 0)
	for _, item := range r.items {
		if item.Status == model.AttachmentStatusUploaded || item.Status == model.AttachmentStatusScanning {
			out = append(out, cloneAttachment(item))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *InMemoryAttachmentRepository) ListPendingPreviews(limit int) ([]model.Attachment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.Attachment, 0)
	for _, item := range r.items {
		if item.Status == model.AttachmentStatusClean &&
			(item.PreviewStatus == model.AttachmentPreviewStatusPending || item.PreviewStatus == model.AttachmentPreviewStatusProcessing) {
			out = append(out, cloneAttachment(item))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *InMemoryAttachmentRepository) ListRetentionCandidates(now time.Time, limit int) ([]model.Attachment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.Attachment, 0)
	for _, item := range r.items {
		if item.Status == model.AttachmentStatusDeleted || item.LegalHold || item.RetainUntil == nil || item.RetainUntil.After(now) {
			continue
		}
		out = append(out, cloneAttachment(item))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RetainUntil.Before(*out[j].RetainUntil) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *InMemoryAttachmentRepository) FindCleanByHash(workspaceID int64, sha256 string, sizeBytes int64) (model.Attachment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.items {
		if item.WorkspaceID == workspaceID && item.SHA256 == sha256 && item.SizeBytes == sizeBytes && item.Status == model.AttachmentStatusClean {
			return cloneAttachment(item), nil
		}
	}
	return model.Attachment{}, ErrAttachmentNotFound
}

func (r *InMemoryAttachmentRepository) CountObjectReferences(workspaceID int64, objectKey string, excludeAttachmentID int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, item := range r.items {
		if item.WorkspaceID != workspaceID || item.ID == excludeAttachmentID || item.Status == model.AttachmentStatusDeleted {
			continue
		}
		if item.ObjectKey == objectKey {
			count++
		}
	}
	return count, nil
}

func (r *InMemoryAttachmentRepository) UsageBytes(workspaceID int64) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var total int64
	for _, item := range r.items {
		if item.WorkspaceID == workspaceID && item.Status != model.AttachmentStatusDeleted {
			total += item.SizeBytes
		}
	}
	return total, nil
}

func cloneAttachment(item model.Attachment) model.Attachment {
	if item.TaskID != nil {
		v := *item.TaskID
		item.TaskID = &v
	}
	if item.CommentID != nil {
		v := *item.CommentID
		item.CommentID = &v
	}
	if item.PreviousAttachmentID != nil {
		v := *item.PreviousAttachmentID
		item.PreviousAttachmentID = &v
	}
	if item.ReleasedByUserID != nil {
		v := *item.ReleasedByUserID
		item.ReleasedByUserID = &v
	}
	if item.RetainUntil != nil {
		v := *item.RetainUntil
		item.RetainUntil = &v
	}
	if item.UploadedAt != nil {
		v := *item.UploadedAt
		item.UploadedAt = &v
	}
	if item.ScannedAt != nil {
		v := *item.ScannedAt
		item.ScannedAt = &v
	}
	if item.ReleasedAt != nil {
		v := *item.ReleasedAt
		item.ReleasedAt = &v
	}
	if item.PreviewCreatedAt != nil {
		v := *item.PreviewCreatedAt
		item.PreviewCreatedAt = &v
	}
	if item.DeletedAt != nil {
		v := *item.DeletedAt
		item.DeletedAt = &v
	}
	return item
}
