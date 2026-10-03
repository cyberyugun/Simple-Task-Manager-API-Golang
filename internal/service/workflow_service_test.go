package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

type workflowTestExecutor struct {
	name  string
	fail  bool
	count *int
}

func (e workflowTestExecutor) Name() string { return e.name }

func (e workflowTestExecutor) Execute(_ context.Context, ctx WorkflowActionContext) (map[string]any, error) {
	if e.count != nil && !ctx.DryRun {
		*e.count = *e.count + 1
	}
	if e.fail {
		return nil, errors.New("expected workflow test failure")
	}
	return map[string]any{"ok": true, "executor": e.name}, nil
}

func TestWorkflowPublishActivateApprovalAndExecution(t *testing.T) {
	orgs := repository.NewInMemoryOrganizationRepository()
	workflows := repository.NewInMemoryWorkflowRepository()
	registry := NewWorkflowExecutorRegistry(nil, nil)
	svc := NewWorkflowService(workflows, orgs, registry)
	now := time.Now().UTC()

	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Workflow Org", Status: model.OrganizationStatusActive, OwnerUserID: 42,
		MaxMembers: 100, MaxWorkspaces: 20, CreatedByUserID: 42,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	graph := model.WorkflowGraph{
		Nodes: []model.WorkflowNode{
			{ID: "trigger", Type: model.WorkflowNodeTrigger, Config: map[string]any{"source": "manual"}},
			{ID: "transform", Type: model.WorkflowNodeTransform, Config: map[string]any{"set": map[string]any{"approved_message": "ready"}}},
			{ID: "approval", Type: model.WorkflowNodeApproval},
			{ID: "action", Type: model.WorkflowNodeAction, Config: map[string]any{"executor": "workflow.noop", "params": map[string]any{"message": "$approved_message"}}},
			{ID: "end", Type: model.WorkflowNodeEnd},
		},
		Edges: []model.WorkflowEdge{
			{From: "trigger", To: "transform"},
			{From: "transform", To: "approval"},
			{From: "approval", To: "action"},
			{From: "action", To: "end"},
		},
	}

	workflow, version, err := svc.CreateWorkflow(42, org.ID, model.CreateWorkflowRequest{Name: "Approval flow", Graph: graph})
	if err != nil {
		t.Fatal(err)
	}
	if version.Status != model.WorkflowVersionDraft {
		t.Fatalf("version status=%s", version.Status)
	}
	version, err = svc.Publish(42, org.ID, workflow.ID, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if version.Checksum == "" {
		t.Fatal("published version should have deterministic checksum")
	}
	if _, err := svc.Activate(42, org.ID, workflow.ID, version.ID); err != nil {
		t.Fatal(err)
	}

	detail, err := svc.Start(42, org.ID, workflow.ID, model.StartWorkflowExecutionRequest{TriggerType: model.WorkflowTriggerManual})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Execution.Status != model.WorkflowExecutionWaitingApproval || len(detail.Approvals) != 1 {
		t.Fatalf("unexpected detail: %+v", detail)
	}

	detail, err = svc.DecideApproval(42, org.ID, detail.Approvals[0].ID, model.DecideWorkflowApprovalRequest{Approve: true, Comment: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Execution.Status != model.WorkflowExecutionSucceeded {
		t.Fatalf("execution status=%s", detail.Execution.Status)
	}
	if len(detail.Checkpoints) < 4 {
		t.Fatalf("expected durable checkpoints, got %d", len(detail.Checkpoints))
	}
}

func TestWorkflowRetryExhaustionRunsCompensation(t *testing.T) {
	orgs := repository.NewInMemoryOrganizationRepository()
	workflows := repository.NewInMemoryWorkflowRepository()
	registry := NewWorkflowExecutorRegistry(nil, nil)
	successCount := 0
	failCount := 0
	rollbackCount := 0
	registry.Register(workflowTestExecutor{name: "test.success", count: &successCount})
	registry.Register(workflowTestExecutor{name: "test.fail", fail: true, count: &failCount})
	registry.Register(workflowTestExecutor{name: "test.rollback", count: &rollbackCount})
	svc := NewWorkflowService(workflows, orgs, registry)
	now := time.Now().UTC()

	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Compensation Org", Status: model.OrganizationStatusActive, OwnerUserID: 7,
		MaxMembers: 100, MaxWorkspaces: 20, CreatedByUserID: 7,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	graph := model.WorkflowGraph{
		Nodes: []model.WorkflowNode{
			{ID: "trigger", Type: model.WorkflowNodeTrigger, Config: map[string]any{"source": "manual"}},
			{ID: "reserve", Type: model.WorkflowNodeAction, Config: map[string]any{"executor": "test.success"}, CompensationNodeID: "rollback"},
			{ID: "charge", Type: model.WorkflowNodeAction, Config: map[string]any{"executor": "test.fail"}, Retry: model.WorkflowRetry{MaxAttempts: 1}},
			{ID: "rollback", Type: model.WorkflowNodeAction, Config: map[string]any{"executor": "test.rollback"}},
			{ID: "end", Type: model.WorkflowNodeEnd},
		},
		Edges: []model.WorkflowEdge{
			{From: "trigger", To: "reserve"},
			{From: "reserve", To: "charge"},
			{From: "charge", To: "end"},
		},
	}

	workflow, version, err := svc.CreateWorkflow(7, org.ID, model.CreateWorkflowRequest{Name: "Compensating flow", Graph: graph})
	if err != nil {
		t.Fatal(err)
	}
	version, err = svc.Publish(7, org.ID, workflow.ID, version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Activate(7, org.ID, workflow.ID, version.ID); err != nil {
		t.Fatal(err)
	}
	detail, err := svc.Start(7, org.ID, workflow.ID, model.StartWorkflowExecutionRequest{TriggerType: model.WorkflowTriggerManual})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Execution.Status != model.WorkflowExecutionCompensated {
		t.Fatalf("execution status=%s error=%s", detail.Execution.Status, detail.Execution.ErrorMessage)
	}
	if successCount != 1 || failCount != 1 || rollbackCount != 1 {
		t.Fatalf("counts success=%d fail=%d rollback=%d", successCount, failCount, rollbackCount)
	}
}

func TestWorkflowGraphRejectsCyclesAndUnallowlistedExecutor(t *testing.T) {
	registry := NewWorkflowExecutorRegistry(nil, nil)
	cycle := model.WorkflowGraph{
		Nodes: []model.WorkflowNode{
			{ID: "trigger", Type: model.WorkflowNodeTrigger, Config: map[string]any{"source": "manual"}},
			{ID: "action", Type: model.WorkflowNodeAction, Config: map[string]any{"executor": "workflow.noop"}},
			{ID: "end", Type: model.WorkflowNodeEnd},
		},
		Edges: []model.WorkflowEdge{
			{From: "trigger", To: "action"},
			{From: "action", To: "trigger"},
			{From: "action", To: "end"},
		},
	}
	if !errors.Is(validateWorkflowGraph(cycle, registry), ErrInvalidWorkflow) {
		t.Fatal("cycle should be rejected")
	}

	unsafe := model.WorkflowGraph{
		Nodes: []model.WorkflowNode{
			{ID: "trigger", Type: model.WorkflowNodeTrigger, Config: map[string]any{"source": "manual"}},
			{ID: "action", Type: model.WorkflowNodeAction, Config: map[string]any{"executor": "shell.exec"}},
			{ID: "end", Type: model.WorkflowNodeEnd},
		},
		Edges: []model.WorkflowEdge{{From: "trigger", To: "action"}, {From: "action", To: "end"}},
	}
	if !errors.Is(validateWorkflowGraph(unsafe, registry), ErrWorkflowExecutorNotAllowed) {
		t.Fatal("arbitrary executor should be rejected")
	}
}
