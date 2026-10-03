package service

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestOrganizationAdministrationLifecycle(t *testing.T) {
	users := repository.NewInMemoryUserRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	orgs := repository.NewInMemoryOrganizationRepository()
	now := time.Now()

	owner, _ := users.Create(model.User{Name: "Owner", Email: "owner@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	newOwner, _ := users.Create(model.User{Name: "New Owner", Email: "newowner@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	delegated, _ := users.Create(model.User{Name: "Delegated", Email: "delegated@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	invited, _ := users.Create(model.User{Name: "Invited", Email: "invited@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})
	extra, _ := users.Create(model.User{Name: "Extra", Email: "extra@example.com", PasswordHash: "hash", CreatedAt: now, UpdatedAt: now})

	svc := NewOrganizationService(orgs, users, workspaces)
	org, err := svc.Create(owner.ID, model.CreateOrganizationRequest{
		Name: "Acme", MaxWorkspaces: 1, MaxMembers: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if org.OwnerUserID != owner.ID || org.Status != model.OrganizationStatusActive {
		t.Fatalf("unexpected organization: %+v", org)
	}

	if _, err := svc.BulkAddMembers(owner.ID, org.ID, model.BulkOrganizationMemberRequest{
		UserIDs: []int64{newOwner.ID}, Role: model.OrganizationRoleMember,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BulkAddMembers(owner.ID, org.ID, model.BulkOrganizationMemberRequest{
		UserIDs: []int64{delegated.ID}, Role: model.OrganizationRoleDelegatedAdmin,
	}); err != nil {
		t.Fatal(err)
	}

	transferred, err := svc.TransferOwnership(owner.ID, org.ID, model.TransferOrganizationOwnershipRequest{UserID: newOwner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if transferred.OwnerUserID != newOwner.ID {
		t.Fatalf("owner=%d want=%d", transferred.OwnerUserID, newOwner.ID)
	}

	child, err := svc.Create(owner.ID, model.CreateOrganizationRequest{Name: "Acme APAC", ParentID: &org.ID})
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentID == nil || *child.ParentID != org.ID {
		t.Fatalf("unexpected child organization: %+v", child)
	}

	workspace, err := workspaces.Create(owner.ID, "Enterprise Workspace", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AttachWorkspace(owner.ID, org.ID, model.AttachOrganizationWorkspaceRequest{WorkspaceID: workspace.ID}); err != nil {
		t.Fatal(err)
	}
	secondWorkspace, _ := workspaces.Create(owner.ID, "Second Workspace", now)
	if _, err := svc.AttachWorkspace(owner.ID, org.ID, model.AttachOrganizationWorkspaceRequest{WorkspaceID: secondWorkspace.ID}); !errors.Is(err, ErrOrganizationWorkspaceQuota) {
		t.Fatalf("workspace quota err=%v", err)
	}

	invite, err := svc.CreateInvitation(delegated.ID, org.ID, model.CreateOrganizationInvitationRequest{
		Email: invited.Email, Role: model.OrganizationRoleMember, ExpiresInHours: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	if invite.Token == "" || invite.Invitation.TokenHash == "" {
		t.Fatalf("invitation secret missing: %+v", invite)
	}
	member, err := svc.AcceptInvitation(invited.ID, model.AcceptOrganizationInvitationRequest{Token: invite.Token})
	if err != nil {
		t.Fatal(err)
	}
	if member.Role != model.OrganizationRoleMember {
		t.Fatalf("unexpected accepted role: %+v", member)
	}
	if _, err := svc.BulkAddMembers(owner.ID, org.ID, model.BulkOrganizationMemberRequest{
		UserIDs: []int64{extra.ID}, Role: model.OrganizationRoleMember,
	}); !errors.Is(err, ErrOrganizationMemberQuota) {
		t.Fatalf("member quota err=%v", err)
	}

	team, err := svc.CreateTeam(delegated.ID, org.ID, model.CreateOrganizationTeamRequest{Name: "Platform"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddTeamMember(delegated.ID, org.ID, team.ID, model.AddOrganizationTeamMemberRequest{UserID: invited.ID}); err != nil {
		t.Fatal(err)
	}

	domainSecret, err := svc.CreateDomain(delegated.ID, org.ID, model.CreateOrganizationDomainRequest{Domain: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := svc.VerifyDomain(delegated.ID, org.ID, domainSecret.Domain.ID, model.VerifyOrganizationDomainRequest{Token: domainSecret.Token})
	if err != nil {
		t.Fatal(err)
	}
	if verified.VerifiedAt == nil {
		t.Fatal("domain was not verified")
	}

	if _, err := svc.UpdateStatus(newOwner.ID, org.ID, model.UpdateOrganizationStatusRequest{Status: model.OrganizationStatusSuspended}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateTeam(delegated.ID, org.ID, model.CreateOrganizationTeamRequest{Name: "Blocked"}); !errors.Is(err, ErrOrganizationInactive) {
		t.Fatalf("suspended organization mutation err=%v", err)
	}
	if _, err := svc.UpdateStatus(newOwner.ID, org.ID, model.UpdateOrganizationStatusRequest{Status: model.OrganizationStatusActive}); err != nil {
		t.Fatal(err)
	}

	dashboard, err := svc.Dashboard(owner.ID, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.MemberCount != 4 || dashboard.WorkspaceCount != 1 || dashboard.TeamCount != 1 || dashboard.VerifiedDomains != 1 {
		t.Fatalf("unexpected dashboard: %+v", dashboard)
	}
	directory, err := svc.Directory(invited.ID, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(directory) != 4 {
		t.Fatalf("directory len=%d want=4", len(directory))
	}
}
