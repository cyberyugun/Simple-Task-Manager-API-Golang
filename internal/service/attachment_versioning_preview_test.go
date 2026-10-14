package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

type attachmentPreviewerStub struct{}

func (attachmentPreviewerStub) Enabled() bool { return true }

func (attachmentPreviewerStub) Generate(_ context.Context, _ model.Attachment, objectKey string) (model.AttachmentPreviewResult, error) {
	return model.AttachmentPreviewResult{
		ObjectKey: objectKey, ContentType: "image/webp", Width: 640, Height: 480,
		Engine: "preview-test", Message: "rendered",
	}, nil
}

func TestAttachmentVersioningQuarantineReleaseAndPreview(t *testing.T) {
	tasks := repository.NewInMemoryTaskRepository()
	collab := repository.NewInMemoryTaskCollaborationRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	attachments := repository.NewInMemoryAttachmentRepository()
	access, err := workspaces.Create(31, "Files Hardening", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	task, err := tasks.Create(model.Task{
		WorkspaceID: access.ID, UserID: 31, Title: "Versioned attachment",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityMedium,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := AttachmentConfig{
		Provider: model.AttachmentProviderS3Compatible, Bucket: "test-bucket",
		BaseURL: "https://storage.example.test", SigningSecret: strings.Repeat("p", 32),
		MaxFileBytes: 4096, WorkspaceQuotaBytes: 16384, PresignTTL: time.Minute,
	}
	store, err := NewSignedObjectStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewAttachmentService(attachments, tasks, collab, workspaces, store, NoopAttachmentScanner{}, cfg)
	svc.SetPreviewer(attachmentPreviewerStub{})

	firstHash := strings.Repeat("a", 64)
	first, err := svc.CreateUpload(31, access, model.AttachmentUploadRequest{
		TaskID: &task.ID, FileName: "design.png", ContentType: "image/png", SizeBytes: 120, SHA256: firstHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Attachment.Version != 1 || first.Attachment.VersionGroupID == "" || first.Attachment.PreviousAttachmentID != nil {
		t.Fatalf("unexpected first version: %+v", first.Attachment)
	}
	if _, err := svc.CompleteUpload(31, access, first.Attachment.ID, model.CompleteAttachmentUploadRequest{SizeBytes: 120, SHA256: firstHash}); err != nil {
		t.Fatal(err)
	}
	scanned, err := svc.ProcessPendingScans(10)
	if err != nil || len(scanned) != 1 || scanned[0].Status != model.AttachmentStatusClean || scanned[0].PreviewStatus != model.AttachmentPreviewStatusPending {
		t.Fatalf("scan=%+v err=%v", scanned, err)
	}
	previews, err := svc.ProcessPendingPreviews(10)
	if err != nil || len(previews) != 1 || previews[0].PreviewStatus != model.AttachmentPreviewStatusReady {
		t.Fatalf("previews=%+v err=%v", previews, err)
	}
	preview, err := svc.Preview(31, access, first.Attachment.ID)
	if err != nil || !strings.Contains(preview.PreviewURL, "op=download") || preview.Attachment.PreviewContentType != "image/webp" {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}

	secondHash := strings.Repeat("b", 64)
	second, err := svc.CreateVersionUpload(31, access, first.Attachment.ID, model.AttachmentVersionUploadRequest{
		FileName: "design-v2.png", ContentType: "image/png", SizeBytes: 140, SHA256: secondHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Attachment.Version != 2 || second.Attachment.VersionGroupID != first.Attachment.VersionGroupID ||
		second.Attachment.PreviousAttachmentID == nil || *second.Attachment.PreviousAttachmentID != first.Attachment.ID {
		t.Fatalf("unexpected second version: %+v", second.Attachment)
	}
	versions, err := svc.Versions(access, second.Attachment.ID)
	if err != nil || len(versions) != 2 || versions[0].Version != 1 || versions[1].Version != 2 {
		t.Fatalf("versions=%+v err=%v", versions, err)
	}

	quarantined, err := svc.Quarantine(31, access, first.Attachment.ID, model.AttachmentQuarantineRequest{Reason: "manual security review"})
	if err != nil || quarantined.Status != model.AttachmentStatusQuarantine || quarantined.QuarantineReason == "" {
		t.Fatalf("quarantined=%+v err=%v", quarantined, err)
	}
	if _, err := svc.Download(31, access, first.Attachment.ID); err != ErrAttachmentBlocked {
		t.Fatalf("download while quarantined error=%v", err)
	}
	memberAccess := access
	memberAccess.Role = model.WorkspaceRoleMember
	if _, err := svc.ReleaseQuarantine(31, memberAccess, first.Attachment.ID, model.AttachmentReleaseRequest{Note: "reviewed"}); err != ErrAttachmentGovernanceDenied {
		t.Fatalf("member release error=%v", err)
	}
	released, err := svc.ReleaseQuarantine(31, access, first.Attachment.ID, model.AttachmentReleaseRequest{Note: "manual review passed"})
	if err != nil || released.Status != model.AttachmentStatusClean || released.ReleasedByUserID == nil || *released.ReleasedByUserID != 31 || released.ReleasedAt == nil {
		t.Fatalf("released=%+v err=%v", released, err)
	}
}
