//go:build integration

package repository_test

import (
	"os"
	"testing"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestIntegrationPostgresOrganizationAdministration(t *testing.T) {
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
	orgs := repository.NewPostgresOrganizationRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	owner, err := users.Create(model.User{
		Name: "Org Owner", Email: "org-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	member, err := users.Create(model.User{
		Name: "Org Member", Email: "org-member@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	workspace, err := workspaces.Create(owner.ID, "Org Workspace", now)
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Integration Org", Status: model.OrganizationStatusActive,
		OwnerUserID: owner.ID, MaxWorkspaces: 2, MaxMembers: 10,
		CreatedByUserID:  owner.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orgs.GetMember(org.ID, owner.ID); err != nil {
		t.Fatalf("owner membership: %v", err)
	}
	if _, err := orgs.UpsertMember(model.OrganizationMember{
		OrganizationID: org.ID, UserID: member.ID, Role: model.OrganizationRoleAdmin,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := orgs.AttachWorkspace(model.OrganizationWorkspace{
		OrganizationID: org.ID, WorkspaceID: workspace.ID, AttachedByID: owner.ID, AttachedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	invitation, err := orgs.CreateInvitation(model.OrganizationInvitation{
		OrganizationID: org.ID, Email: member.Email, Role: model.OrganizationRoleMember,
		TokenHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Status:    model.InvitationStatusPending, InvitedByUserID: owner.ID,
		ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	found, err := orgs.FindInvitationByHash(invitation.TokenHash)
	if err != nil || found.ID != invitation.ID {
		t.Fatalf("find invitation=%+v err=%v", found, err)
	}
	if _, err := orgs.AcceptInvitation(invitation.ID, member.ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	team, err := orgs.CreateTeam(model.OrganizationTeam{
		OrganizationID: org.ID, Name: "Platform", CreatedByID: owner.ID,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orgs.AddTeamMember(model.OrganizationTeamMember{TeamID: team.ID, UserID: member.ID, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	domain, err := orgs.CreateDomain(model.OrganizationDomain{
		OrganizationID: org.ID, Domain: "example.com",
		VerificationHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		CreatedByUserID: owner.ID, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if verified, err := orgs.VerifyDomain(org.ID, domain.ID, owner.ID, domain.VerificationHash, now.Add(time.Minute)); err != nil || verified.VerifiedAt == nil {
		t.Fatalf("verify domain=%+v err=%v", verified, err)
	}

	if err := orgs.RecordAudit(model.OrganizationAuditEvent{
		OrganizationID: org.ID, ActorUserID: &owner.ID, Action: "integration.test",
		ResourceType: "organization", ResourceID: "test", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	audit, err := orgs.ListAudit(org.ID, 10)
	if err != nil || len(audit) != 1 {
		t.Fatalf("audit=%+v err=%v", audit, err)
	}

	transferred, err := orgs.TransferOwnership(org.ID, owner.ID, member.ID, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if transferred.OwnerUserID != member.ID {
		t.Fatalf("owner=%d want=%d", transferred.OwnerUserID, member.ID)
	}
}

func getenvRequired(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Skip(name + " is not set")
	}
	return value
}
