package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

type externalTestAIProvider struct{}

func (externalTestAIProvider) Key() string         { return "external_test" }
func (externalTestAIProvider) DisplayName() string { return "External Test" }
func (externalTestAIProvider) External() bool       { return true }
func (externalTestAIProvider) SupportedClassifications() []string {
	return []string{
		model.DataClassificationPublic,
		model.DataClassificationInternal,
		model.DataClassificationConfidential,
		model.DataClassificationRestricted,
	}
}
func (externalTestAIProvider) EstimateCostCents(AIProviderRequest) int64 { return 1 }
func (externalTestAIProvider) Generate(_ context.Context, req AIProviderRequest) (AIProviderResponse, error) {
	return AIProviderResponse{
		Model: "external-test-v1",
		Result: map[string]any{"summary": req.Text},
		InputUnits: 1, OutputUnits: 1, ActualCostCents: 1,
	}, nil
}

func newAITestService(t *testing.T) (*AIAssistanceService, *repository.InMemoryAIAssistanceRepository, *repository.InMemoryTaskRepository, *repository.InMemoryOrganizationRepository, int64, int64) {
	t.Helper()
	orgs := repository.NewInMemoryOrganizationRepository()
	tasks := repository.NewInMemoryTaskRepository()
	aiRepo := repository.NewInMemoryAIAssistanceRepository()
	operations := repository.NewInMemoryOperationsRepository()
	now := time.Now().UTC()
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "AI Org", Status: model.OrganizationStatusActive, OwnerUserID: 7, MaxWorkspaces: 20, MaxMembers: 100,
		CreatedByUserID: 7, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orgs.UpsertMember(model.OrganizationMember{
		OrganizationID: org.ID, UserID: 7, Role: model.OrganizationRoleOwner, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	const workspaceID int64 = 41
	if _, err := orgs.AttachWorkspace(model.OrganizationWorkspace{
		OrganizationID: org.ID, WorkspaceID: workspaceID, AttachedByID: 7, AttachedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	registry := NewAIProviderRegistry()
	registry.Register(externalTestAIProvider{})
	svc := NewAIAssistanceService(aiRepo, orgs, operations, tasks, NewTaskService(tasks), registry)
	return svc, aiRepo, tasks, orgs, org.ID, workspaceID
}

func TestAIAssistanceRedactionApprovalSemanticSearchAndQuality(t *testing.T) {
	svc, _, tasks, _, orgID, workspaceID := newAITestService(t)
	policy, err := svc.UpdatePolicy(7, orgID, model.UpdateAIPolicyRequest{
		Enabled: true, Provider: model.AIProviderLocalRules, MonthlyBudgetCents: 100,
		RedactionEnabled: true, MaxInputChars: 12000,
		AllowedClassifications: []string{
			model.DataClassificationPublic,
			model.DataClassificationInternal,
			model.DataClassificationConfidential,
		},
		ExternalMaxClassification: model.DataClassificationInternal,
		RequireHumanApprovalForActions: true,
	})
	if err != nil || !policy.Enabled {
		t.Fatalf("policy=%+v err=%v", policy, err)
	}

	now := time.Now().UTC()
	task, err := tasks.Create(model.Task{
		WorkspaceID: workspaceID, UserID: 7, Title: "Fix login authentication issue",
		Description: "contact dev@example.com password=demo123 before release",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityMedium,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	run, err := svc.Assist(context.Background(), 7, orgID, model.AIAssistRequest{
		Feature: model.AIFeatureDescriptionImprovement, WorkspaceID: &workspaceID, TaskID: &task.ID,
		Classification: model.DataClassificationInternal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != model.AIRequestPendingApproval || !run.RequiresApproval || run.RedactionCount < 2 {
		t.Fatalf("unexpected AI request: %+v", run)
	}
	description := strings.ToLower(run.StructuredResult["description"].(string))
	if strings.Contains(description, "dev@example.com") || strings.Contains(description, "demo123") {
		t.Fatalf("redaction failed: %s", description)
	}
	if run.InputHash == "" || strings.Contains(run.InputHash, task.Description) {
		t.Fatalf("unexpected input hash: %s", run.InputHash)
	}

	approved, err := svc.Decide(7, orgID, run.ID, model.AIDecisionRequest{Decision: model.AIApprovalApprove, Comment: "reviewed"})
	if err != nil || approved.Status != model.AIRequestApproved {
		t.Fatalf("approved=%+v err=%v", approved, err)
	}
	updated, err := tasks.FindByID(workspaceID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(updated.Description, "dev@example.com") || updated.Description == task.Description {
		t.Fatalf("approved description was not applied safely: %q", updated.Description)
	}

	second, err := tasks.Create(model.Task{
		WorkspaceID: workspaceID, UserID: 7, Title: "Customer auth failure",
		Description: "Investigate customer login issue", Status: model.TaskStatusTodo,
		Priority: model.TaskPriorityHigh, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	hits, err := svc.SemanticSearch(7, orgID, model.AISemanticSearchRequest{
		WorkspaceID: workspaceID, Query: "client authentication bug", Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, hit := range hits {
		if hit.Task.ID == second.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected semantic-normalized task hit: %+v", hits)
	}

	testCase, err := svc.CreateEvaluationCase(7, orgID, model.CreateAIEvaluationCaseRequest{
		Name: "Summary baseline", Feature: model.AIFeatureTaskSummary,
		Input: "Ship the governed AI assistance implementation",
		ExpectedKeywords: []string{"governed", "assistance"},
	})
	if err != nil {
		t.Fatal(err)
	}
	eval, err := svc.RunEvaluation(context.Background(), 7, orgID, testCase.ID)
	if err != nil || !eval.Passed || eval.ScoreBasisPoints != 10000 {
		t.Fatalf("evaluation=%+v err=%v", eval, err)
	}
	quality, err := svc.Quality(7, orgID)
	if err != nil || quality.TotalRuns != 1 || quality.PassedRuns != 1 {
		t.Fatalf("quality=%+v err=%v", quality, err)
	}
}

func TestAIAssistanceBudgetAndClassificationRouting(t *testing.T) {
	svc, _, _, _, orgID, _ := newAITestService(t)
	if _, err := svc.UpdatePolicy(7, orgID, model.UpdateAIPolicyRequest{
		Enabled: true, Provider: model.AIProviderLocalRules, MonthlyBudgetCents: 1,
		RedactionEnabled: true, MaxInputChars: 12000,
		AllowedClassifications: []string{model.DataClassificationInternal},
		ExternalMaxClassification: model.DataClassificationInternal,
		RequireHumanApprovalForActions: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Assist(context.Background(), 7, orgID, model.AIAssistRequest{
		Feature: model.AIFeatureTaskSummary, Input: "first request", Classification: model.DataClassificationInternal,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Assist(context.Background(), 7, orgID, model.AIAssistRequest{
		Feature: model.AIFeatureTaskSummary, Input: "second request", Classification: model.DataClassificationInternal,
	}); !errors.Is(err, ErrAIBudgetExceeded) {
		t.Fatalf("budget error=%v", err)
	}

	if _, err := svc.UpdatePolicy(7, orgID, model.UpdateAIPolicyRequest{
		Enabled: true, Provider: "external_test", MonthlyBudgetCents: 100,
		RedactionEnabled: true, MaxInputChars: 12000,
		AllowedClassifications: []string{
			model.DataClassificationInternal,
			model.DataClassificationConfidential,
			model.DataClassificationRestricted,
		},
		ExternalMaxClassification: model.DataClassificationInternal,
		RequireHumanApprovalForActions: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Assist(context.Background(), 7, orgID, model.AIAssistRequest{
		Feature: model.AIFeatureTaskSummary, Input: "restricted payload",
		Classification: model.DataClassificationRestricted,
	}); !errors.Is(err, ErrAIClassificationBlocked) {
		t.Fatalf("classification routing error=%v", err)
	}
}

func TestAIAssistanceDestructiveActionRequiresExplicitApproval(t *testing.T) {
	svc, _, tasks, _, orgID, workspaceID := newAITestService(t)
	if _, err := svc.UpdatePolicy(7, orgID, model.UpdateAIPolicyRequest{
		Enabled: true, Provider: model.AIProviderLocalRules, MonthlyBudgetCents: 100,
		RedactionEnabled: true, MaxInputChars: 12000,
		AllowedClassifications: []string{model.DataClassificationInternal},
		ExternalMaxClassification: model.DataClassificationInternal,
		RequireHumanApprovalForActions: true,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task, err := tasks.Create(model.Task{
		WorkspaceID: workspaceID, UserID: 7, Title: "Old task", Description: "Archive after review",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityLow, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.Assist(context.Background(), 7, orgID, model.AIAssistRequest{
		Feature: model.AIFeatureTaskSummary, WorkspaceID: &workspaceID, TaskID: &task.ID,
		Classification: model.DataClassificationInternal,
		ProposedAction: map[string]any{"type": "task.archive", "task_id": task.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !run.DestructiveAction || run.Status != model.AIRequestPendingApproval {
		t.Fatalf("destructive request was not held: %+v", run)
	}
	before, _ := tasks.FindByID(workspaceID, task.ID)
	if before.ArchivedAt != nil {
		t.Fatal("AI action executed before approval")
	}
	if _, err := svc.Decide(7, orgID, run.ID, model.AIDecisionRequest{Decision: model.AIApprovalApprove}); err != nil {
		t.Fatal(err)
	}
	after, _ := tasks.FindByID(workspaceID, task.ID)
	if after.ArchivedAt == nil {
		t.Fatal("approved archive was not applied")
	}
}
