package service

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestLifecycleRetentionLegalHoldPrivacyAndConsent(t *testing.T) {
	users := repository.NewInMemoryUserRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	governance := repository.NewInMemoryGovernanceRepository()
	lifecycleRepo := repository.NewInMemoryLifecycleRepository()
	tasks := repository.NewInMemoryTaskRepository()

	now := time.Now()
	owner, err := users.Create(model.User{Name: "Owner", Email: "lifecycle-owner@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	member, err := users.Create(model.User{Name: "Member", Email: "lifecycle-member@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaces.Create(owner.ID, "Lifecycle Workspace", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.AddMember(workspace.ID, member.ID, model.WorkspaceRoleMember, now); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.UpsertPolicy(model.GovernancePolicy{
		WorkspaceID: workspace.ID, DefaultClassification: model.DataClassificationInternal,
		AuditRetentionDays: 365, OperationalRetentionDays: 1, PrivacyRequestSLAHours: 24,
		UpdatedByUserID: owner.ID, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	lifecycleRepo.ConfigureWorkspace(workspace.ID)

	oldTask, err := tasks.Create(model.Task{
		WorkspaceID: workspace.ID, UserID: member.ID, Title: "Old personal task",
		Description: "private details", CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-48 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.Create(model.Task{
		WorkspaceID: workspace.ID, UserID: member.ID, Title: "Fresh task",
		Description: "keep", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewLifecycleService(lifecycleRepo, governance, workspaces, tasks)
	run, err := svc.Run(owner.ID, workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != model.LifecycleRunCompleted || run.ArchivedCount != 1 {
		t.Fatalf("unexpected lifecycle run: %+v", run)
	}
	if _, err := tasks.FindByID(workspace.ID, oldTask.ID); !errors.Is(err, repository.ErrTaskNotFound) {
		t.Fatalf("old task still active, err=%v", err)
	}

	hold, err := governance.CreateLegalHold(model.LegalHold{
		WorkspaceID: workspace.ID, Name: "Case", Reason: "preserve", ResourceType: "workspace",
		CreatedByUserID: owner.ID, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	skipped, err := svc.Run(owner.ID, workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if skipped.Status != model.LifecycleRunSkipped {
		t.Fatalf("run with legal hold = %+v", skipped)
	}

	deleteRequest, err := governance.CreatePrivacyRequest(model.PrivacyRequest{
		WorkspaceID: workspace.ID, SubjectUserID: member.ID, Type: model.PrivacyRequestDelete,
		Status: model.PrivacyStatusPending, RequestedByUserID: owner.ID, RequestedAt: now, DueAt: now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ErasePrivacyRequest(owner.ID, workspace.ID, deleteRequest.ID); !errors.Is(err, ErrPrivacyLegalHold) {
		t.Fatalf("erase during legal hold err=%v", err)
	}
	if err := governance.ReleaseLegalHold(workspace.ID, hold.ID, owner.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	count, err := svc.ErasePrivacyRequest(owner.ID, workspace.ID, deleteRequest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("anonymized count=%d, want 1", count)
	}

	exportRequest, err := governance.CreatePrivacyRequest(model.PrivacyRequest{
		WorkspaceID: workspace.ID, SubjectUserID: member.ID, Type: model.PrivacyRequestExport,
		Status: model.PrivacyStatusPending, RequestedByUserID: owner.ID, RequestedAt: now, DueAt: now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := svc.ExportPrivacyRequest(owner.ID, workspace.ID, exportRequest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.ChecksumSHA256) != 64 || pkg.Payload == nil {
		t.Fatalf("unexpected export package: %+v", pkg)
	}

	consent, err := svc.AddConsent(owner.ID, workspace.ID, model.CreateConsentRequest{
		SubjectUserID: member.ID, Purpose: "analytics", Status: model.ConsentGranted,
		PolicyVersion: "v1", Source: "settings",
	})
	if err != nil {
		t.Fatal(err)
	}
	if consent.Status != model.ConsentGranted {
		t.Fatalf("unexpected consent: %+v", consent)
	}

	report, err := svc.Report(owner.ID, workspace.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.ConsentRecords != 1 || report.LatestLifecycleRun == nil {
		t.Fatalf("unexpected governance report: %+v", report)
	}
}
