package service

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestGovernanceLifecycleAndLegalHoldDeletionGuard(t *testing.T) {
	users := repository.NewInMemoryUserRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	governance := repository.NewInMemoryGovernanceRepository()

	now := time.Now()
	owner, err := users.Create(model.User{Name: "Owner", Email: "owner-governance@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	member, err := users.Create(model.User{Name: "Member", Email: "member-governance@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaces.Create(owner.ID, "Governed Workspace", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.AddMember(workspace.ID, member.ID, model.WorkspaceRoleMember, now); err != nil {
		t.Fatal(err)
	}

	svc := NewGovernanceService(governance, workspaces)
	policy, err := svc.UpdatePolicy(owner.ID, workspace.ID, model.UpdateGovernancePolicyRequest{
		DefaultClassification: model.DataClassificationConfidential,
		AuditRetentionDays: 730, OperationalRetentionDays: 365, PrivacyRequestSLAHours: 72,
		RequireDPA: true, RestrictCrossRegionTransfer: true, AllowedDataRegions: []string{"id", "sg"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if policy.PrivacyRequestSLAHours != 72 || policy.DefaultClassification != model.DataClassificationConfidential {
		t.Fatalf("unexpected policy: %+v", policy)
	}

	inventory, err := svc.UpsertDataInventory(owner.ID, workspace.ID, model.UpsertDataInventoryRequest{
		ResourceType: "users", FieldName: "email", Classification: model.DataClassificationRestricted,
		ContainsPersonalData: true, DataRegion: "id", RetentionDays: 365,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !inventory.ContainsPersonalData {
		t.Fatalf("unexpected inventory: %+v", inventory)
	}

	hold, err := svc.CreateLegalHold(owner.ID, workspace.ID, model.CreateLegalHoldRequest{
		Name: "Investigation", Reason: "Preserve records", ResourceType: "workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err := svc.HasActiveLegalHold(workspace.ID)
	if err != nil || !active {
		t.Fatalf("active hold = %v err=%v", active, err)
	}

	workspaceSvc := NewWorkspaceService(workspaces, users)
	workspaceSvc.SetDeletionGuard(svc)
	if err := workspaceSvc.Delete(owner.ID, workspace.ID); !errors.Is(err, ErrWorkspaceLegalHold) {
		t.Fatalf("Delete() error = %v, want ErrWorkspaceLegalHold", err)
	}

	request, err := svc.CreatePrivacyRequest(owner.ID, workspace.ID, model.CreatePrivacyRequestRequest{
		SubjectUserID: member.ID, Type: model.PrivacyRequestExport,
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.DueAt.Sub(request.RequestedAt) != 72*time.Hour {
		t.Fatalf("privacy request SLA = %s", request.DueAt.Sub(request.RequestedAt))
	}
	if _, err := svc.CompletePrivacyRequest(owner.ID, workspace.ID, request.ID, model.CompletePrivacyRequestRequest{
		Status: model.PrivacyStatusCompleted,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CreateComplianceEvidence(owner.ID, workspace.ID, model.CreateComplianceEvidenceRequest{
		Framework: "SOC2", Control: "CC6.1", EvidenceType: "configuration",
		Description: "MFA and access policy evidence", Metadata: map[string]any{"automated": true},
	}); err != nil {
		t.Fatal(err)
	}

	if err := svc.ReleaseLegalHold(owner.ID, workspace.ID, hold.ID); err != nil {
		t.Fatal(err)
	}
	active, err = svc.HasActiveLegalHold(workspace.ID)
	if err != nil || active {
		t.Fatalf("active hold after release = %v err=%v", active, err)
	}
	if err := workspaceSvc.Delete(owner.ID, workspace.ID); err != nil {
		t.Fatalf("Delete() after hold release error = %v", err)
	}
}
