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

func TestIntegrationPostgresEnterpriseDataPlatform(t *testing.T) {
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
	orgs := repository.NewPostgresOrganizationRepository(db)
	governance := repository.NewPostgresGovernanceRepository(db)
	analytics := repository.NewPostgresSearchAnalyticsRepository(db)
	dataRepo := repository.NewPostgresDataPlatformRepository(db)
	dataPlatform := service.NewDataPlatformService(dataRepo, orgs, analytics, governance)

	now := time.Now().UTC().Truncate(time.Microsecond)
	owner, err := users.Create(model.User{
		Name: "Data Platform Owner", Email: "phase44-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaces.Create(owner.ID, "Phase 44 Analytics Workspace", now)
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Phase 44 Organization", Status: model.OrganizationStatusActive, OwnerUserID: owner.ID,
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
		AllowedDataRegions: []string{"ap-southeast"}, UpdatedByUserID: owner.ID, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	task, err := tasks.Create(model.Task{
		WorkspaceID: workspace.ID, UserID: owner.ID, Title: "Warehouse analytics task",
		Status: model.TaskStatusInProgress, Priority: model.TaskPriorityHigh,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := dataPlatform.CreateConnection(owner.ID, org.ID, model.CreateDataPlatformConnectionRequest{
		Name: "Primary BigQuery", Provider: model.DataWarehouseBigQuery,
		Target:              "analytics.task_analytics",
		BIContracts:         []string{model.BIContractPowerBI, model.BIContractTableau},
		Config:              map[string]string{"project_id": "phase44-project", "dataset": "analytics"},
		SecretRef:           "secret://phase44/bigquery",
		Masking:             model.DataMaskingPolicy{Mode: model.MaskingHash, Fields: []string{"title"}},
		FreshnessSLOMinutes: 60, MaxMonthlyCostUSD: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := dataPlatform.RunExport(owner.ID, org.ID, connection.ID, model.RunDataExportRequest{Mode: model.DataExportModeIncremental})
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != model.DataExportSucceeded || first.Rows != 1 || first.PayloadHash == "" {
		t.Fatalf("first export = %+v", first)
	}
	checkpoint, err := dataPlatform.Checkpoint(owner.ID, org.ID, connection.ID)
	if err != nil || checkpoint.LastJobID != first.ID {
		t.Fatalf("checkpoint=%+v err=%v", checkpoint, err)
	}

	task.Title = "Warehouse analytics task updated"
	task.UpdatedAt = now.Add(time.Minute)
	if _, err := tasks.Update(task); err != nil {
		t.Fatal(err)
	}
	second, err := dataPlatform.RunExport(owner.ID, org.ID, connection.ID, model.RunDataExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Rows != 1 || second.CheckpointAfter == second.CheckpointBefore {
		t.Fatalf("second export = %+v", second)
	}
	lineage, err := dataPlatform.Lineage(owner.ID, org.ID)
	if err != nil || len(lineage) != 2 || lineage[0].TargetDataset != connection.Target {
		t.Fatalf("lineage=%+v err=%v", lineage, err)
	}
	dashboard, err := dataPlatform.Dashboard(owner.ID, org.ID)
	if err != nil || dashboard.SucceededExports != 2 || dashboard.RowsExported != 2 || dashboard.FreshnessBreaches != 0 {
		t.Fatalf("dashboard=%+v err=%v", dashboard, err)
	}
}
