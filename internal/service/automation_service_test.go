package service

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestAutomationBillingTriggerAutomaticRunbook(t *testing.T) {
	orgs := repository.NewInMemoryOrganizationRepository()
	billing := repository.NewInMemoryBillingRepository()
	operations := repository.NewInMemoryOperationsRepository()
	automation := repository.NewInMemoryAutomationRepository()
	billingService := NewBillingService(billing, orgs)
	svc := NewAutomationService(automation, orgs, operations, billing, billingService)
	now := time.Now().UTC()

	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Automation Org", Status: model.OrganizationStatusActive, OwnerUserID: 42,
		MaxMembers: 100, MaxWorkspaces: 20, CreatedByUserID: 42,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	start, end := automationMonth(now)
	if _, err := billing.RecordBillingUsage(org.ID, "api_operations", start, end, 6000, now); err != nil {
		t.Fatal(err)
	}

	policy, err := svc.CreatePolicy(42, org.ID, model.CreateAutomationPolicyRequest{
		Name:        "Open incident at 50% API capacity",
		TriggerType: model.AutomationTriggerBillingUsagePercent,
		TriggerKey:  "api_operations",
		Comparator:  model.AutomationComparatorGreaterOrEqual,
		Threshold:   50,
		ActionType:  model.AutomationActionOpenIncident,
		ActionConfig: map[string]any{
			"severity": model.IncidentSeverity3,
			"title":    "API capacity automation",
		},
		ApprovalMode:    model.AutomationApprovalAutomatic,
		CooldownMinutes: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.Enabled {
		t.Fatal("new policy should be enabled by default")
	}

	executions, err := svc.Evaluate(42, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(executions) != 1 || executions[0].Status != model.AutomationExecutionSucceeded {
		t.Fatalf("executions=%+v", executions)
	}
	incidents, err := operations.ListOperationalIncidents(org.ID, "", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil || len(incidents) != 1 || incidents[0].Severity != model.IncidentSeverity3 {
		t.Fatalf("incidents=%+v err=%v", incidents, err)
	}

	again, err := svc.Evaluate(42, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("cooldown should suppress duplicate execution: %+v", again)
	}
}

func TestAutomationApprovalAndSafetyGuardrail(t *testing.T) {
	orgs := repository.NewInMemoryOrganizationRepository()
	billing := repository.NewInMemoryBillingRepository()
	operations := repository.NewInMemoryOperationsRepository()
	automation := repository.NewInMemoryAutomationRepository()
	billingService := NewBillingService(billing, orgs)
	svc := NewAutomationService(automation, orgs, operations, billing, billingService)
	now := time.Now().UTC()

	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Approval Org", Status: model.OrganizationStatusActive, OwnerUserID: 7,
		MaxMembers: 100, MaxWorkspaces: 20, CreatedByUserID: 7,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CreatePolicy(7, org.ID, model.CreateAutomationPolicyRequest{
		Name:            "Unsafe auto SEV1",
		TriggerType:     model.AutomationTriggerOperationsAlert,
		TriggerKey:      "slo_breach",
		Comparator:      model.AutomationComparatorGreaterOrEqual,
		Threshold:       1,
		ActionType:      model.AutomationActionOpenIncident,
		ActionConfig:    map[string]any{"severity": model.IncidentSeverity1},
		ApprovalMode:    model.AutomationApprovalAutomatic,
		CooldownMinutes: 30,
	})
	if !errors.Is(err, ErrInvalidAutomationPolicy) {
		t.Fatalf("expected safety rejection, got %v", err)
	}

	_, err = svc.CreatePolicy(7, org.ID, model.CreateAutomationPolicyRequest{
		Name:        "Approve SEV1 for SLO breach",
		TriggerType: model.AutomationTriggerOperationsAlert,
		TriggerKey:  "slo_breach",
		Comparator:  model.AutomationComparatorGreaterOrEqual,
		Threshold:   9000,
		ActionType:  model.AutomationActionOpenIncident,
		ActionConfig: map[string]any{
			"severity": model.IncidentSeverity1,
			"title":    "Approved SLO incident",
		},
		ApprovalMode:    model.AutomationApprovalRequired,
		CooldownMinutes: 30,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := operations.UpsertOperationalAlert(model.OperationalAlert{
		OrganizationID: org.ID,
		Fingerprint:    "slo:test",
		Type:           "slo_breach",
		Metric:         "availability_basis_points",
		Status:         model.OperationalAlertOpen,
		Message:        "SLO below target",
		CurrentValue:   9500,
		ThresholdValue: 9990,
		DetectedAt:     now,
		UpdatedAt:      now,
	}); err != nil {
		t.Fatal(err)
	}

	executions, err := svc.Evaluate(7, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(executions) != 1 || executions[0].Status != model.AutomationExecutionPendingApproval {
		t.Fatalf("executions=%+v", executions)
	}
	approved, err := svc.DecideExecution(7, org.ID, executions[0].ID, model.DecideAutomationExecutionRequest{Approve: true})
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != model.AutomationExecutionSucceeded {
		t.Fatalf("approved=%+v", approved)
	}
	incidents, _ := operations.ListOperationalIncidents(org.ID, "", now.Add(-time.Hour), time.Now().UTC().Add(time.Hour))
	if len(incidents) != 1 || incidents[0].Severity != model.IncidentSeverity1 {
		t.Fatalf("incidents=%+v", incidents)
	}
}
