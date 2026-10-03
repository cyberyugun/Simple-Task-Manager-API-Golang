package service

import (
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestOperationsControlPlaneLifecycle(t *testing.T) {
	orgs := repository.NewInMemoryOrganizationRepository()
	billing := repository.NewInMemoryBillingRepository()
	opsRepo := repository.NewInMemoryOperationsRepository()
	billingService := NewBillingService(billing, orgs)
	svc := NewOperationsService(opsRepo, orgs, billing, billingService)
	now := time.Now().UTC()

	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Ops Org", Status: model.OrganizationStatusActive, OwnerUserID: 10,
		MaxMembers: 1000, MaxWorkspaces: 100, CreatedByUserID: 10,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := billingService.ChangeSubscription(10, org.ID, model.ChangeBillingSubscriptionRequest{PlanCode: "pro"}); err != nil {
		t.Fatal(err)
	}

	policy, err := svc.UpdatePolicy(10, org.ID, model.UpdateOperationsPolicyRequest{
		MonthlyBudgetCents: 10000, BudgetAlertThresholdPercent: 80,
		SLOTargetBasisPoints: 9990, IncidentEscalationMinutes: 30,
		SupportTier: model.OperationsSupportPriority, SupportContact: "ops@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if policy.SupportTier != model.OperationsSupportPriority {
		t.Fatalf("policy=%+v", policy)
	}

	monthStart, monthEnd := operationsMonth(now)
	if _, err := svc.CreateCostAllocation(10, org.ID, model.CreateCostAllocationRequest{
		Category: "compute", Source: "cloud", AmountCents: 9000,
		PeriodStart: monthStart, PeriodEnd: monthEnd,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := billing.RecordBillingUsage(org.ID, "api_operations", monthStart, monthEnd, 900000, now); err != nil {
		t.Fatal(err)
	}

	maintenanceStart := now.Add(-2 * time.Hour)
	maintenanceEnd := now.Add(-90 * time.Minute)
	if _, err := svc.CreateMaintenance(10, org.ID, model.CreateMaintenanceWindowRequest{
		Title: "Database maintenance", StartsAt: maintenanceStart, EndsAt: maintenanceEnd,
	}); err != nil {
		t.Fatal(err)
	}
	incident, err := svc.CreateIncident(10, org.ID, model.CreateOperationalIncidentRequest{
		Title: "Database outage", Severity: model.IncidentSeverity1,
		Summary: "availability impact", StartedAt: now.Add(-2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	alerts, err := svc.Evaluate(10, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) == 0 {
		t.Fatal("expected operational alerts")
	}
	foundCapacity := false
	foundIncident := false
	for _, alert := range alerts {
		if alert.Type == "capacity_forecast" {
			foundCapacity = true
		}
		if alert.Type == "incident_escalation" {
			foundIncident = true
		}
	}
	if !foundCapacity || !foundIncident {
		t.Fatalf("alerts=%+v", alerts)
	}

	dashboard, err := svc.Dashboard(10, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Policy.SupportTier != model.OperationsSupportPriority ||
		len(dashboard.CapacityForecasts) != 1 || len(dashboard.OpenIncidents) != 1 {
		t.Fatalf("dashboard=%+v", dashboard)
	}
	if dashboard.SLO.DowntimeSeconds <= 0 {
		t.Fatalf("expected downtime after excluding maintenance: %+v", dashboard.SLO)
	}

	if _, err := svc.UpdateIncident(10, org.ID, incident.ID, model.UpdateOperationalIncidentRequest{
		Status: model.IncidentStatusResolved, Summary: "recovered",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AcknowledgeAlert(10, org.ID, alerts[0].ID); err != nil {
		t.Fatal(err)
	}
}

func TestSLOExcludesPlannedMaintenance(t *testing.T) {
	orgs := repository.NewInMemoryOrganizationRepository()
	billing := repository.NewInMemoryBillingRepository()
	opsRepo := repository.NewInMemoryOperationsRepository()
	billingService := NewBillingService(billing, orgs)
	svc := NewOperationsService(opsRepo, orgs, billing, billingService)
	now := time.Now().UTC()
	org, _ := orgs.CreateOrganization(model.Organization{
		Name: "SLO Org", Status: model.OrganizationStatusActive, OwnerUserID: 1,
		MaxMembers: 100, MaxWorkspaces: 20, CreatedByUserID: 1,
		CreatedAt: now, UpdatedAt: now,
	})
	policy, _ := svc.Policy(1, org.ID)

	start := now.Add(-2 * time.Hour)
	end := now.Add(-time.Hour)
	_, _ = svc.CreateMaintenance(1, org.ID, model.CreateMaintenanceWindowRequest{
		Title: "planned", StartsAt: start, EndsAt: end,
	})
	_, _ = svc.CreateIncident(1, org.ID, model.CreateOperationalIncidentRequest{
		Title: "planned impact", Severity: model.IncidentSeverity1, StartedAt: start,
	})
	incidents, _ := opsRepo.ListOperationalIncidents(org.ID, "", start.Add(-time.Minute), now)
	if len(incidents) != 1 {
		t.Fatalf("incidents=%+v", incidents)
	}
	_, _ = opsRepo.UpdateOperationalIncident(org.ID, incidents[0].ID, model.IncidentStatusResolved, "done", &end, now)

	periodStart, periodEnd := operationsMonth(now)
	slo, err := svc.sloStatus(org.ID, policy, periodStart, periodEnd, now)
	if err != nil {
		t.Fatal(err)
	}
	if slo.DowntimeSeconds != 0 {
		t.Fatalf("planned maintenance should exclude downtime: %+v", slo)
	}
}
