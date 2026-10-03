//go:build integration

package repository_test

import (
	"testing"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestIntegrationPostgresAutomationRepository(t *testing.T) {
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
	orgs := repository.NewPostgresOrganizationRepository(db)
	automation := repository.NewPostgresAutomationRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	owner, err := users.Create(model.User{
		Name: "Automation Owner", Email: "automation-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Automation Org", Status: model.OrganizationStatusActive, OwnerUserID: owner.ID,
		MaxMembers: 100, MaxWorkspaces: 20, CreatedByUserID: owner.ID,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	policy, err := automation.CreateAutomationPolicy(model.AutomationPolicy{
		OrganizationID: org.ID, Name: "Capacity policy", Enabled: true,
		TriggerType: model.AutomationTriggerBillingUsagePercent, TriggerKey: "api_operations",
		Comparator: model.AutomationComparatorGreaterOrEqual, Threshold: 80,
		ActionType: model.AutomationActionOpenIncident,
		ActionConfig: map[string]any{"severity": model.IncidentSeverity3, "title": "Capacity"},
		ApprovalMode: model.AutomationApprovalAutomatic, CooldownMinutes: 60,
		CreatedByUserID: owner.ID, UpdatedByUserID: owner.ID,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || policy.ID == 0 {
		t.Fatalf("policy=%+v err=%v", policy, err)
	}
	policies, err := automation.ListEnabledAutomationPolicies()
	if err != nil || len(policies) != 1 {
		t.Fatalf("policies=%+v err=%v", policies, err)
	}

	execution, err := automation.CreateAutomationExecution(model.AutomationExecution{
		OrganizationID: org.ID, PolicyID: policy.ID,
		DedupeKey: "integration-execution", Status: model.AutomationExecutionPendingApproval,
		TriggerSnapshot: map[string]any{"utilization_percent": 90},
		ActionResult: map[string]any{}, RequestedAt: now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || execution.ID == 0 {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	approvedAt := now.Add(time.Minute)
	execution.Status = model.AutomationExecutionSucceeded
	execution.ApprovedAt = &approvedAt
	execution.ApprovedByUserID = &owner.ID
	execution.ExecutedAt = &approvedAt
	execution.CompletedAt = &approvedAt
	execution.ActionResult = map[string]any{"incident_id": float64(123)}
	execution.UpdatedAt = approvedAt
	updated, err := automation.UpdateAutomationExecution(execution)
	if err != nil || updated.Status != model.AutomationExecutionSucceeded {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	latest, err := automation.LatestAutomationExecution(policy.ID)
	if err != nil || latest.ID != execution.ID {
		t.Fatalf("latest=%+v err=%v", latest, err)
	}
	items, err := automation.ListAutomationExecutions(org.ID, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}
