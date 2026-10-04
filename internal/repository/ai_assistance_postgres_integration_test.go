//go:build integration

package repository_test

import (
	"context"
	"strings"
	"testing"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
)

func TestIntegrationPostgresAIAssistance(t *testing.T) {
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
	tasks := repository.NewPostgresTaskRepository(db)
	operations := repository.NewPostgresOperationsRepository(db)
	aiRepo := repository.NewPostgresAIAssistanceRepository(db)
	taskService := service.NewTaskService(tasks)
	ai := service.NewAIAssistanceService(aiRepo, orgs, operations, tasks, taskService, service.NewAIProviderRegistry())

	now := time.Now().UTC().Truncate(time.Microsecond)
	owner, err := users.Create(model.User{
		Name: "AI Owner", Email: "ai-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaces.Create(owner.ID, "AI Workspace", now)
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "AI Organization", Status: model.OrganizationStatusActive, OwnerUserID: owner.ID,
		MaxWorkspaces: 20, MaxMembers: 100, CreatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orgs.UpsertMember(model.OrganizationMember{
		OrganizationID: org.ID, UserID: owner.ID, Role: model.OrganizationRoleOwner,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := orgs.AttachWorkspace(model.OrganizationWorkspace{
		OrganizationID: org.ID, WorkspaceID: workspace.ID, AttachedByID: owner.ID, AttachedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ai.UpdatePolicy(owner.ID, org.ID, model.UpdateAIPolicyRequest{
		Enabled: true, Provider: model.AIProviderLocalRules, MonthlyBudgetCents: 100,
		RedactionEnabled: true, MaxInputChars: 12000,
		AllowedClassifications: []string{
			model.DataClassificationPublic,
			model.DataClassificationInternal,
			model.DataClassificationConfidential,
		},
		ExternalMaxClassification: model.DataClassificationInternal,
		RequireHumanApprovalForActions: true,
	}); err != nil {
		t.Fatal(err)
	}

	task, err := tasks.Create(model.Task{
		WorkspaceID: workspace.ID, UserID: owner.ID, Title: "Fix payment login",
		Description: "Reach finance@example.com and improve this customer issue",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityMedium,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	run, err := ai.Assist(context.Background(), owner.ID, org.ID, model.AIAssistRequest{
		Feature: model.AIFeatureDescriptionImprovement, WorkspaceID: &workspace.ID, TaskID: &task.ID,
		Classification: model.DataClassificationConfidential,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != model.AIRequestPendingApproval || run.RedactionCount == 0 || run.InputHash == "" {
		t.Fatalf("unexpected run: %+v", run)
	}
	if strings.Contains(strings.ToLower(run.StructuredResult["description"].(string)), "finance@example.com") {
		t.Fatal("PII leaked into provider result")
	}

	approved, err := ai.Decide(owner.ID, org.ID, run.ID, model.AIDecisionRequest{Decision: model.AIApprovalApprove})
	if err != nil || approved.Status != model.AIRequestApproved {
		t.Fatalf("approved=%+v err=%v", approved, err)
	}
	updated, err := tasks.FindByID(workspace.ID, task.ID)
	if err != nil || updated.Description == task.Description {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}

	usage, err := ai.Usage(owner.ID, org.ID, now)
	if err != nil || usage.RequestCount != 1 || usage.SpentCents < 1 || usage.BudgetCents != 100 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}

	testCase, err := ai.CreateEvaluationCase(owner.ID, org.ID, model.CreateAIEvaluationCaseRequest{
		Name: "Task summary quality", Feature: model.AIFeatureTaskSummary,
		Input: "Governed AI summary evaluation", ExpectedKeywords: []string{"governed", "summary"},
	})
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := ai.RunEvaluation(context.Background(), owner.ID, org.ID, testCase.ID)
	if err != nil || !evaluation.Passed {
		t.Fatalf("evaluation=%+v err=%v", evaluation, err)
	}
	quality, err := ai.Quality(owner.ID, org.ID)
	if err != nil || quality.TotalRuns != 1 || quality.PassedRuns != 1 {
		t.Fatalf("quality=%+v err=%v", quality, err)
	}

	requests, err := ai.Requests(owner.ID, org.ID)
	if err != nil || len(requests) != 1 || requests[0].ID != run.ID {
		t.Fatalf("requests=%+v err=%v", requests, err)
	}
}
