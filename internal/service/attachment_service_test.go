package service

import (
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestAttachmentUploadScanDownloadAndGovernance(t *testing.T) {
	tasks := repository.NewInMemoryTaskRepository()
	collab := repository.NewInMemoryTaskCollaborationRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	attachments := repository.NewInMemoryAttachmentRepository()
	access, err := workspaces.Create(7, "Files", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	task, err := tasks.Create(model.Task{
		WorkspaceID: access.ID, UserID: 7, Title: "Attachment task", Description: "test",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityMedium,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := AttachmentConfig{
		Provider: model.AttachmentProviderS3Compatible,
		Bucket: "test-bucket",
		BaseURL: "https://storage.example.test",
		SigningSecret: "0123456789abcdef0123456789abcdef",
		MaxFileBytes: 1024,
		WorkspaceQuotaBytes: 4096,
		PresignTTL: time.Minute,
		Encryption: "AES256",
		Deduplicate: true,
	}
	store, err := NewSignedObjectStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewAttachmentService(attachments, tasks, collab, workspaces, store, NoopAttachmentScanner{}, cfg)
	hash := strings.Repeat("a", 64)
	session, err := svc.CreateUpload(7, access, model.AttachmentUploadRequest{
		TaskID: &task.ID, FileName: "../report.pdf", ContentType: "application/pdf", SizeBytes: 100, SHA256: hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.Method != "PUT" || session.Attachment.Status != model.AttachmentStatusPending || !strings.Contains(session.UploadURL, "signature=") {
		t.Fatalf("unexpected upload session: %+v", session)
	}
	completed, err := svc.CompleteUpload(7, access, session.Attachment.ID, model.CompleteAttachmentUploadRequest{SizeBytes: 100, SHA256: hash})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != model.AttachmentStatusUploaded {
		t.Fatalf("status=%s", completed.Status)
	}
	scanned, err := svc.ProcessPendingScans(10)
	if err != nil || len(scanned) != 1 || scanned[0].Status != model.AttachmentStatusClean {
		t.Fatalf("scanned=%+v err=%v", scanned, err)
	}
	download, err := svc.Download(7, access, session.Attachment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(download.DownloadURL, "op=download") {
		t.Fatalf("download url=%s", download.DownloadURL)
	}

	hold := true
	held, err := svc.Governance(7, access, session.Attachment.ID, model.UpdateAttachmentGovernanceRequest{LegalHold: &hold})
	if err != nil || !held.LegalHold {
		t.Fatalf("held=%+v err=%v", held, err)
	}
	if _, err := svc.Delete(7, access, session.Attachment.ID); err != ErrAttachmentGovernanceDenied {
		t.Fatalf("delete on legal hold error=%v", err)
	}
}

func TestAttachmentQuotaAndValidation(t *testing.T) {
	tasks := repository.NewInMemoryTaskRepository()
	collab := repository.NewInMemoryTaskCollaborationRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	attachments := repository.NewInMemoryAttachmentRepository()
	access, _ := workspaces.Create(9, "Quota", time.Now().UTC())
	task, _ := tasks.Create(model.Task{WorkspaceID: access.ID, UserID: 9, Title: "Task", Status: model.TaskStatusTodo, Priority: model.TaskPriorityMedium, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	cfg := AttachmentConfig{BaseURL: "https://storage.example.test", SigningSecret: "0123456789abcdef", MaxFileBytes: 100, WorkspaceQuotaBytes: 100}
	store, err := NewSignedObjectStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewAttachmentService(attachments, tasks, collab, workspaces, store, NoopAttachmentScanner{}, cfg)
	hash := strings.Repeat("b", 64)
	if _, err := svc.CreateUpload(9, access, model.AttachmentUploadRequest{TaskID: &task.ID, FileName: "x.exe", ContentType: "application/x-msdownload", SizeBytes: 20, SHA256: hash}); err != ErrInvalidAttachment {
		t.Fatalf("dangerous mime error=%v", err)
	}
	first, err := svc.CreateUpload(9, access, model.AttachmentUploadRequest{TaskID: &task.ID, FileName: "a.txt", ContentType: "text/plain", SizeBytes: 80, SHA256: hash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteUpload(9, access, first.Attachment.ID, model.CompleteAttachmentUploadRequest{SizeBytes: 80, SHA256: hash}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateUpload(9, access, model.AttachmentUploadRequest{TaskID: &task.ID, FileName: "b.txt", ContentType: "text/plain", SizeBytes: 30, SHA256: strings.Repeat("c", 64)}); err != ErrAttachmentQuotaExceeded {
		t.Fatalf("quota error=%v", err)
	}
}
