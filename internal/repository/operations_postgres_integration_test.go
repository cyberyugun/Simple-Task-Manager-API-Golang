//go:build integration

package repository_test

import (
	"testing"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestIntegrationPostgresOperationsRepository(t *testing.T) {
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
	ops := repository.NewPostgresOperationsRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	owner, err := users.Create(model.User{
		Name: "Operations Owner", Email: "operations-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Operations Org", Status: model.OrganizationStatusActive, OwnerUserID: owner.ID,
		MaxMembers: 100, MaxWorkspaces: 20, CreatedByUserID: owner.ID,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	policy, err := ops.UpsertOperationsPolicy(model.OperationsPolicy{
		OrganizationID: org.ID, MonthlyBudgetCents: 10000, BudgetAlertThresholdPercent: 80,
		SLOTargetBasisPoints: 9990, IncidentEscalationMinutes: 30,
		SupportTier: model.OperationsSupportPriority, SupportContact: "ops@example.com",
		UpdatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || policy.SupportTier != model.OperationsSupportPriority {
		t.Fatalf("policy=%+v err=%v", policy, err)
	}

	cost, err := ops.CreateCostAllocation(model.CostAllocation{
		OrganizationID: org.ID, Category: "compute", Source: "cloud", AmountCents: 2500,
		Currency: "USD", PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(time.Hour),
		Metadata: map[string]any{"service": "api"}, CreatedByUserID: owner.ID, CreatedAt: now,
	})
	if err != nil || cost.ID == 0 {
		t.Fatalf("cost=%+v err=%v", cost, err)
	}
	costs, err := ops.ListCostAllocations(org.ID, now.Add(-24*time.Hour), now.Add(24*time.Hour))
	if err != nil || len(costs) != 1 {
		t.Fatalf("costs=%+v err=%v", costs, err)
	}

	alert, err := ops.UpsertOperationalAlert(model.OperationalAlert{
		OrganizationID: org.ID, Fingerprint: "budget:integration", Type: "budget_threshold",
		Metric: "spend_cents", Status: model.OperationalAlertOpen, Message: "threshold",
		CurrentValue: 9000, ThresholdValue: 8000, DetectedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	alert, err = ops.AcknowledgeOperationalAlert(org.ID, alert.ID, owner.ID, now.Add(time.Minute))
	if err != nil || alert.Status != model.OperationalAlertAcknowledged {
		t.Fatalf("alert=%+v err=%v", alert, err)
	}

	window, err := ops.CreateMaintenanceWindow(model.MaintenanceWindow{
		OrganizationID: org.ID, Title: "maintenance", Status: model.MaintenanceStatusScheduled,
		StartsAt: now.Add(time.Hour), EndsAt: now.Add(2*time.Hour),
		CreatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	window, err = ops.UpdateMaintenanceWindowStatus(org.ID, window.ID, model.MaintenanceStatusCompleted, now.Add(3*time.Hour))
	if err != nil || window.Status != model.MaintenanceStatusCompleted {
		t.Fatalf("window=%+v err=%v", window, err)
	}

	incident, err := ops.CreateOperationalIncident(model.OperationalIncident{
		OrganizationID: org.ID, Title: "outage", Severity: model.IncidentSeverity1,
		Status: model.IncidentStatusOpen, StartedAt: now.Add(-time.Hour),
		CreatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolvedAt := now.Add(time.Minute)
	incident, err = ops.UpdateOperationalIncident(
		org.ID, incident.ID, model.IncidentStatusResolved, "recovered", &resolvedAt, resolvedAt,
	)
	if err != nil || incident.ResolvedAt == nil {
		t.Fatalf("incident=%+v err=%v", incident, err)
	}
	incidents, err := ops.ListOperationalIncidents(org.ID, "", now.Add(-24*time.Hour), now.Add(24*time.Hour))
	if err != nil || len(incidents) != 1 {
		t.Fatalf("incidents=%+v err=%v", incidents, err)
	}
}
