package service

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestWorkspaceServiceRBACAndAudit(t *testing.T) {
	users := repository.NewInMemoryUserRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	now := time.Now()

	owner, _ := users.Create(model.User{Name: "Owner", Email: "owner@example.com", PasswordHash: "x", CreatedAt: now, UpdatedAt: now})
	admin, _ := users.Create(model.User{Name: "Admin", Email: "admin@example.com", PasswordHash: "x", CreatedAt: now, UpdatedAt: now})
	member, _ := users.Create(model.User{Name: "Member", Email: "member@example.com", PasswordHash: "x", CreatedAt: now, UpdatedAt: now})
	svc := NewWorkspaceService(workspaces, users)

	workspace, err := svc.Create(owner.ID, model.CreateWorkspaceRequest{Name: "Team"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AddMember(owner.ID, workspace.ID, model.AddWorkspaceMemberRequest{Email: admin.Email, Role: model.WorkspaceRoleAdmin}); err != nil {
		t.Fatalf("owner add admin: %v", err)
	}
	if _, err := svc.AddMember(admin.ID, workspace.ID, model.AddWorkspaceMemberRequest{Email: member.Email, Role: model.WorkspaceRoleMember}); err != nil {
		t.Fatalf("admin add member: %v", err)
	}

	if _, err := svc.UpdateMemberRole(admin.ID, workspace.ID, owner.ID, model.UpdateWorkspaceMemberRequest{Role: model.WorkspaceRoleMember}); !errors.Is(err, ErrOwnerMembership) {
		t.Fatalf("admin owner mutation error = %v, want ErrOwnerMembership", err)
	}
	if _, err := svc.UpdateMemberRole(member.ID, workspace.ID, admin.ID, model.UpdateWorkspaceMemberRequest{Role: model.WorkspaceRoleMember}); !errors.Is(err, ErrWorkspaceForbidden) {
		t.Fatalf("member role mutation error = %v, want ErrWorkspaceForbidden", err)
	}

	audit, err := svc.Audit(owner.ID, workspace.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(audit) < 3 {
		t.Fatalf("audit events = %d, want at least 3", len(audit))
	}
}

func TestWorkspaceServicePreventsCrossTenantAccess(t *testing.T) {
	users := repository.NewInMemoryUserRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	now := time.Now()
	owner, _ := users.Create(model.User{Name: "Owner", Email: "owner@example.com", PasswordHash: "x", CreatedAt: now, UpdatedAt: now})
	other, _ := users.Create(model.User{Name: "Other", Email: "other@example.com", PasswordHash: "x", CreatedAt: now, UpdatedAt: now})
	svc := NewWorkspaceService(workspaces, users)

	workspace, err := svc.Create(owner.ID, model.CreateWorkspaceRequest{Name: "Private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Access(other.ID, workspace.ID); !errors.Is(err, repository.ErrWorkspaceNotFound) {
		t.Fatalf("cross-tenant access error = %v, want ErrWorkspaceNotFound", err)
	}
}
