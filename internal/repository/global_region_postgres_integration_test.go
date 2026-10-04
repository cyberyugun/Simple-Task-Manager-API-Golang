//go:build integration

package repository_test

import (
	"testing"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
)

func TestIntegrationPostgresGlobalRegionManagement(t *testing.T) {
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
	governance := repository.NewPostgresGovernanceRepository(db)
	regions := repository.NewPostgresGlobalRegionRepository(db)
	svc := service.NewGlobalRegionService(regions, orgs, governance)

	now := time.Now().UTC().Truncate(time.Microsecond)
	owner, err := users.Create(model.User{
		Name: "Region Owner", Email: "region-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaces.Create(owner.ID, "APAC Workspace", now)
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Global Organization", Status: model.OrganizationStatusActive, OwnerUserID: owner.ID,
		MaxWorkspaces: 20, MaxMembers: 100, CreatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orgs.AttachWorkspace(model.OrganizationWorkspace{
		OrganizationID: org.ID, WorkspaceID: workspace.ID, AttachedByID: owner.ID, AttachedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.UpsertPolicy(model.GovernancePolicy{
		WorkspaceID: workspace.ID, DefaultClassification: model.DataClassificationInternal,
		AuditRetentionDays: 365, OperationalRetentionDays: 365, PrivacyRequestSLAHours: 720,
		RestrictCrossRegionTransfer: true, AllowedDataRegions: []string{"ap-southeast", "ap-northeast"},
		UpdatedByUserID: owner.ID, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	policy, err := svc.UpdatePolicy(owner.ID, org.ID, model.UpdateOrganizationRegionPolicyRequest{
		HomeRegion: "ap-southeast", AllowedRegions: []string{"ap-southeast", "ap-northeast"},
		FailoverRegions: []string{"ap-northeast"}, DataResidencyEnforced: true,
		CrossRegionApprovalRequired: true, RPOSeconds: 300, RTOSeconds: 1800,
	})
	if err != nil {
		t.Fatal(err)
	}
	if policy.HomeRegion != "ap-southeast" {
		t.Fatalf("home region = %q", policy.HomeRegion)
	}

	placement, err := svc.UpsertPlacement(owner.ID, org.ID, model.UpsertRegionalPlacementRequest{
		ResourceType: "database", ResourceID: "primary", PrimaryRegion: "ap-southeast",
		ReplicaRegions: []string{"ap-northeast"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if placement.PrimaryRegion != "ap-southeast" || len(placement.ReplicaRegions) != 1 {
		t.Fatalf("unexpected placement: %+v", placement)
	}

	migration, err := svc.CreateMigration(owner.ID, org.ID, model.CreateRegionMigrationRequest{
		Scope: "organization", SourceRegion: "ap-southeast", TargetRegion: "ap-northeast",
		Reason: "planned residency migration",
	})
	if err != nil {
		t.Fatal(err)
	}
	if migration.Status != model.RegionMigrationPendingApproval {
		t.Fatalf("migration status = %q", migration.Status)
	}
	migration, err = svc.DecideMigration(owner.ID, org.ID, migration.ID, model.DecideRegionMigrationRequest{
		Decision: "approve", Comment: "approved in integration test",
	})
	if err != nil {
		t.Fatal(err)
	}
	migration, err = svc.CompleteMigration(owner.ID, org.ID, migration.ID, model.CompleteRegionMigrationRequest{
		Success: true, Checkpoint: map[string]any{"replication_lag_seconds": float64(0)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if migration.Status != model.RegionMigrationCompleted {
		t.Fatalf("completed migration status = %q", migration.Status)
	}
	route, err := svc.Route(owner.ID, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if route.PrimaryRegion != "ap-northeast" {
		t.Fatalf("route primary = %q", route.PrimaryRegion)
	}

	transfer, err := svc.CreateTransfer(owner.ID, org.ID, model.CreateCrossRegionTransferRequest{
		ResourceType: "task_export", ResourceID: "export-1", Classification: model.DataClassificationInternal,
		SourceRegion: "ap-northeast", TargetRegion: "ap-southeast", Reason: "approved data export",
	})
	if err != nil {
		t.Fatal(err)
	}
	if transfer.Status != model.RegionTransferPendingApproval {
		t.Fatalf("transfer status = %q", transfer.Status)
	}
	transfer, err = svc.DecideTransfer(owner.ID, org.ID, transfer.ID, model.DecideCrossRegionTransferRequest{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	transfer, err = svc.CompleteTransfer(owner.ID, org.ID, transfer.ID)
	if err != nil || transfer.Status != model.RegionTransferCompleted {
		t.Fatalf("transfer=%+v err=%v", transfer, err)
	}

	exercise, err := svc.CreateFailoverExercise(owner.ID, org.ID, model.CreateFailoverExerciseRequest{
		SourceRegion: "ap-northeast", TargetRegion: "ap-southeast", Notes: "quarterly game day",
	})
	if err != nil {
		t.Fatal(err)
	}
	exercise, err = svc.CompleteFailoverExercise(owner.ID, org.ID, exercise.ID, model.CompleteFailoverExerciseRequest{
		Success: true, AchievedRPOSeconds: 120, AchievedRTOSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !exercise.MeetsRPO || !exercise.MeetsRTO {
		t.Fatalf("failover targets not met: %+v", exercise)
	}

	report, err := svc.Report(owner.ID, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Compliant || len(report.Violations) != 0 {
		t.Fatalf("unexpected residency report: %+v", report)
	}
}
