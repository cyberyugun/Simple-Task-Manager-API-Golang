//go:build integration

package repository_test

import (
	"strings"
	"testing"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestIntegrationPostgresAttachmentPlatform(t *testing.T) {
	databaseURL := getenvRequired(t, "TEST_DATABASE_URL")
	db, err := appdb.OpenPostgres(databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgres() error = %v", err)
	}
	if err := resetDatabase(db); err != nil {
		_ = db.Close()
		t.Fatalf("reset database: %v", err)
	}
	t.Cleanup(func() {
		if err := resetDatabase(db); err != nil {
			t.Errorf("cleanup database: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	applyMigrations(t, db)

	users := repository.NewPostgresUserRepository(db)
	workspaces := repository.NewPostgresWorkspaceRepository(db)
	tasks := repository.NewPostgresTaskRepository(db)
	collab := repository.NewPostgresTaskCollaborationRepository(db)
	attachments := repository.NewPostgresAttachmentRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	owner, err := users.Create(model.User{
		Name: "Attachment Owner", Email: "attachment-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	access, err := workspaces.Create(owner.ID, "Attachment Workspace", now)
	if err != nil {
		t.Fatal(err)
	}
	task, err := tasks.Create(model.Task{
		WorkspaceID: access.ID, UserID: owner.ID, Title: "Attachment Task",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityMedium,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	comment, err := collab.CreateComment(model.TaskComment{
		TaskID: task.ID, WorkspaceID: access.ID, UserID: owner.ID,
		Body: "attachment comment", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	hash := strings.Repeat("a", 64)
	item, err := attachments.CreateAttachment(model.Attachment{
		WorkspaceID: access.ID, TaskID: &task.ID, CommentID: &comment.ID, UploadedByUserID: owner.ID,
		Provider: model.AttachmentProviderS3Compatible, Bucket: "bucket", ObjectKey: "workspaces/test/report.pdf",
		FileName: "report.pdf", ContentType: "application/pdf", SizeBytes: 128, SHA256: hash,
		Status: model.AttachmentStatusUploaded, Encryption: "AES256", Version: 1,
		UploadedAt: &now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || item.ID == 0 {
		t.Fatalf("attachment=%+v err=%v", item, err)
	}

	pending, err := attachments.ListPendingScans(10)
	if err != nil || len(pending) != 1 || pending[0].ID != item.ID {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}

	scannedAt := now.Add(time.Second)
	item.Status = model.AttachmentStatusClean
	item.ScanEngine = "integration"
	item.ScanMessage = "clean"
	item.ScannedAt = &scannedAt
	item.UpdatedAt = scannedAt
	item, err = attachments.UpdateAttachment(item)
	if err != nil {
		t.Fatal(err)
	}

	found, err := attachments.FindCleanByHash(access.ID, hash, 128)
	if err != nil || found.ID != item.ID {
		t.Fatalf("found=%+v err=%v", found, err)
	}
	usage, err := attachments.UsageBytes(access.ID)
	if err != nil || usage != 128 {
		t.Fatalf("usage=%d err=%v", usage, err)
	}
	listed, err := attachments.ListAttachments(access.ID, &task.ID, &comment.ID)
	if err != nil || len(listed) != 1 || listed[0].Status != model.AttachmentStatusClean {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}

	retainUntil := now.Add(-time.Minute)
	item.RetainUntil = &retainUntil
	item.UpdatedAt = now.Add(2 * time.Second)
	if _, err := attachments.UpdateAttachment(item); err != nil {
		t.Fatal(err)
	}
	retention, err := attachments.ListRetentionCandidates(now, 10)
	if err != nil || len(retention) != 1 || retention[0].ID != item.ID {
		t.Fatalf("retention=%+v err=%v", retention, err)
	}

	item.LegalHold = true
	item.UpdatedAt = now.Add(3 * time.Second)
	if _, err := attachments.UpdateAttachment(item); err != nil {
		t.Fatal(err)
	}
	retention, err = attachments.ListRetentionCandidates(now, 10)
	if err != nil || len(retention) != 0 {
		t.Fatalf("legal hold retention=%+v err=%v", retention, err)
	}
}
