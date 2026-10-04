package service

import (
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func newDataPlatformTestService(t *testing.T) (*DataPlatformService, *repository.InMemoryTaskRepository, int64, int64) {
	t.Helper()
	orgs := repository.NewInMemoryOrganizationRepository()
	tasks := repository.NewInMemoryTaskRepository()
	analytics := repository.NewInMemorySearchAnalyticsRepository(tasks)
	governance := repository.NewInMemoryGovernanceRepository()
	repo := repository.NewInMemoryDataPlatformRepository()
	now := time.Now().UTC()
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Data Platform Test", Status: model.OrganizationStatusActive, OwnerUserID: 1,
		MaxWorkspaces: 10, MaxMembers: 10, CreatedByUserID: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	const workspaceID int64 = 42
	if _, err := orgs.AttachWorkspace(model.OrganizationWorkspace{
		OrganizationID: org.ID, WorkspaceID: workspaceID, AttachedByID: 1, AttachedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.UpsertPolicy(model.GovernancePolicy{
		WorkspaceID: workspaceID, DefaultClassification: model.DataClassificationConfidential,
		AuditRetentionDays: 365, OperationalRetentionDays: 365, PrivacyRequestSLAHours: 720,
		RestrictCrossRegionTransfer: true, AllowedDataRegions: []string{"ap-southeast"},
		UpdatedByUserID: 1, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return NewDataPlatformService(repo, orgs, analytics, governance), tasks, org.ID, workspaceID
}

func testBigQueryConnection(t *testing.T, svc *DataPlatformService, orgID int64, masking model.DataMaskingPolicy) model.DataPlatformConnection {
	t.Helper()
	item, err := svc.CreateConnection(1, orgID, model.CreateDataPlatformConnectionRequest{
		Name: "Analytics Warehouse", Provider: model.DataWarehouseBigQuery,
		Target: "analytics.task_analytics", BIContracts: []string{model.BIContractPowerBI, model.BIContractLooker},
		Config:    map[string]string{"project_id": "test-project", "dataset": "analytics"},
		SecretRef: "secret://warehouse/bigquery", Masking: masking,
		FreshnessSLOMinutes: 60, MaxMonthlyCostUSD: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestDataPlatformIncrementalExportCheckpointAndLineage(t *testing.T) {
	svc, tasks, orgID, workspaceID := newDataPlatformTestService(t)
	connection := testBigQueryConnection(t, svc, orgID, model.DataMaskingPolicy{
		Mode: model.MaskingHash, Fields: []string{"title"},
	})
	now := time.Now().UTC().Add(-time.Hour)
	task, err := tasks.Create(model.Task{
		WorkspaceID: workspaceID, UserID: 1, Title: "Confidential launch",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityHigh,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := svc.RunExport(1, orgID, connection.ID, model.RunDataExportRequest{Mode: model.DataExportModeIncremental})
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != model.DataExportSucceeded || first.Rows != 1 || first.CheckpointAfter == "" || len(first.PayloadHash) != 64 {
		t.Fatalf("unexpected first export: %+v", first)
	}

	second, err := svc.RunExport(1, orgID, connection.ID, model.RunDataExportRequest{Mode: model.DataExportModeIncremental})
	if err != nil {
		t.Fatal(err)
	}
	if second.Rows != 0 || second.CheckpointBefore != second.CheckpointAfter {
		t.Fatalf("unexpected no-op incremental export: %+v", second)
	}

	task.Title = "Confidential launch updated"
	task.UpdatedAt = time.Now().UTC()
	if _, err := tasks.Update(task); err != nil {
		t.Fatal(err)
	}
	third, err := svc.RunExport(1, orgID, connection.ID, model.RunDataExportRequest{Mode: model.DataExportModeIncremental})
	if err != nil {
		t.Fatal(err)
	}
	if third.Rows != 1 || third.CheckpointAfter == third.CheckpointBefore {
		t.Fatalf("unexpected incremental update export: %+v", third)
	}

	lineage, err := svc.Lineage(1, orgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(lineage) != 3 || !containsString(lineage[0].GovernanceTags, "classification:confidential") ||
		!containsString(lineage[0].GovernanceTags, "governance:cross_region_restricted") {
		t.Fatalf("unexpected lineage: %+v", lineage)
	}
	dashboard, err := svc.Dashboard(1, orgID)
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.SucceededExports != 3 || dashboard.RowsExported != 2 || dashboard.ActiveConnections != 1 {
		t.Fatalf("unexpected dashboard: %+v", dashboard)
	}
}

func TestDataPlatformMaskingAndTenantKeys(t *testing.T) {
	row := model.AnalyticsTaskRecord{
		OrganizationID: 9, WorkspaceID: 11, TaskID: 13, Title: "Customer secret",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityMedium,
	}
	redacted := applyAnalyticsMasking(row, model.DataMaskingPolicy{Mode: model.MaskingRedact, Fields: []string{"title"}})
	if redacted.Title != "[REDACTED]" || redacted.OrganizationID != 9 || redacted.WorkspaceID != 11 {
		t.Fatalf("redacted row = %+v", redacted)
	}
	hashed := applyAnalyticsMasking(row, model.DataMaskingPolicy{Mode: model.MaskingHash, Fields: []string{"title"}})
	if !strings.HasPrefix(hashed.Title, "sha256:") || len(hashed.Title) != len("sha256:")+64 {
		t.Fatalf("hashed title = %q", hashed.Title)
	}
	if hashed.OrganizationID != row.OrganizationID || hashed.WorkspaceID != row.WorkspaceID {
		t.Fatalf("tenant keys changed: %+v", hashed)
	}
}

func TestDataPlatformSchemaEvolutionRejectsBreakingChanges(t *testing.T) {
	svc, _, orgID, _ := newDataPlatformTestService(t)
	connection := testBigQueryConnection(t, svc, orgID, model.DataMaskingPolicy{Mode: model.MaskingNone})

	schemas, err := svc.Schemas(1, orgID, connection.ID)
	if err != nil || len(schemas) != 1 {
		t.Fatalf("schemas=%+v err=%v", schemas, err)
	}
	nextFields := append(cloneDataPlatformFields(schemas[0].Fields), model.DatasetField{
		Name: "custom_dimension", Type: "string", Nullable: true, SourcePath: "custom.dimension",
	})
	next, err := svc.CreateSchema(1, orgID, connection.ID, model.CreateDatasetSchemaRequest{Fields: nextFields})
	if err != nil {
		t.Fatal(err)
	}
	if next.Version != 2 {
		t.Fatalf("schema version = %d, want 2", next.Version)
	}

	breaking := cloneDataPlatformFields(next.Fields)
	breaking = breaking[1:]
	if _, err := svc.CreateSchema(1, orgID, connection.ID, model.CreateDatasetSchemaRequest{Fields: breaking}); err != ErrIncompatibleDatasetSchema {
		t.Fatalf("breaking schema error = %v, want %v", err, ErrIncompatibleDatasetSchema)
	}
}

func TestWarehouseAdapterRecoveryDoesNotAdvanceCheckpoint(t *testing.T) {
	svc, tasks, orgID, workspaceID := newDataPlatformTestService(t)
	connection, err := svc.CreateConnection(1, orgID, model.CreateDataPlatformConnectionRequest{
		Name: "Failing Warehouse", Provider: model.DataWarehouseBigQuery,
		Target: "analytics.task_analytics", BIContracts: []string{model.BIContractTableau},
		Config:  map[string]string{"project_id": "test-project", "dataset": "analytics", "fail_delivery": "true"},
		Masking: model.DataMaskingPolicy{Mode: model.MaskingNone}, FreshnessSLOMinutes: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := tasks.Create(model.Task{
		WorkspaceID: workspaceID, UserID: 1, Title: "Retry me", Status: model.TaskStatusTodo,
		Priority: model.TaskPriorityMedium, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	job, err := svc.RunExport(1, orgID, connection.ID, model.RunDataExportRequest{})
	if err == nil || job.Status != model.DataExportFailed {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	if _, err := svc.Checkpoint(1, orgID, connection.ID); err != repository.ErrDataExportCheckpointNotFound {
		t.Fatalf("checkpoint error = %v", err)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
