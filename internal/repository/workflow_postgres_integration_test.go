//go:build integration

package repository_test

import (
	"testing"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestIntegrationPostgresWorkflowRepository(t *testing.T) {
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
	workflows := repository.NewPostgresWorkflowRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	owner, err := users.Create(model.User{
		Name: "Workflow Owner", Email: "workflow-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Workflow Org", Status: model.OrganizationStatusActive, OwnerUserID: owner.ID,
		MaxMembers: 100, MaxWorkspaces: 20, CreatedByUserID: owner.ID,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	workflow, err := workflows.CreateWorkflow(model.WorkflowDefinition{
		OrganizationID: org.ID, Name: "Integration workflow", Description: "phase 34",
		CreatedByUserID: owner.ID, UpdatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || workflow.ID == 0 {
		t.Fatalf("workflow=%+v err=%v", workflow, err)
	}
	graph := model.WorkflowGraph{
		Nodes: []model.WorkflowNode{
			{ID: "trigger", Type: model.WorkflowNodeTrigger, Config: map[string]any{"source": model.WorkflowTriggerManual}},
			{ID: "action", Type: model.WorkflowNodeAction, Config: map[string]any{"executor": "workflow.noop"}},
			{ID: "end", Type: model.WorkflowNodeEnd},
		},
		Edges: []model.WorkflowEdge{{From: "trigger", To: "action"}, {From: "action", To: "end"}},
	}
	version, err := workflows.CreateWorkflowVersion(model.WorkflowVersion{
		OrganizationID: org.ID, WorkflowID: workflow.ID, Version: 1,
		Status: model.WorkflowVersionPublished, Graph: graph, Checksum: "checksum",
		CreatedByUserID: owner.ID, PublishedByID: &owner.ID, PublishedAt: &now,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || version.ID == 0 {
		t.Fatalf("version=%+v err=%v", version, err)
	}
	workflow.ActiveVersionID = &version.ID
	workflow.UpdatedAt = now.Add(time.Second)
	if _, err := workflows.UpdateWorkflow(workflow); err != nil {
		t.Fatal(err)
	}

	resumeAt := now.Add(time.Minute)
	execution, err := workflows.CreateWorkflowExecution(model.WorkflowExecution{
		OrganizationID: org.ID, WorkflowID: workflow.ID, WorkflowVersionID: version.ID,
		Status: model.WorkflowExecutionWaitingDelay, TriggerType: model.WorkflowTriggerManual,
		TriggerPayload: map[string]any{"source": "integration"}, Variables: map[string]any{"count": float64(1)},
		WorkflowSnapshot: graph, NextNodeIDs: []string{"action"}, DryRun: false,
		RequestedByUserID: &owner.ID, ResumeAt: &resumeAt, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || execution.ID == 0 {
		t.Fatalf("execution=%+v err=%v", execution, err)
	}
	due, err := workflows.ListDueWorkflowExecutions(resumeAt.Add(time.Second), 10)
	if err != nil || len(due) != 1 || due[0].ID != execution.ID {
		t.Fatalf("due=%+v err=%v", due, err)
	}

	nodeRun, err := workflows.CreateWorkflowNodeExecution(model.WorkflowNodeExecution{
		ExecutionID: execution.ID, NodeID: "action", NodeType: model.WorkflowNodeAction,
		Status: model.WorkflowNodeExecutionRetryWaiting, Attempt: 1,
		Input: map[string]any{"value": "in"}, Output: map[string]any{},
		RetryAt: &resumeAt, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || nodeRun.ID == 0 {
		t.Fatalf("nodeRun=%+v err=%v", nodeRun, err)
	}
	latest, err := workflows.LatestWorkflowNodeExecution(execution.ID, "action")
	if err != nil || latest.ID != nodeRun.ID {
		t.Fatalf("latest=%+v err=%v", latest, err)
	}

	approval, err := workflows.CreateWorkflowApproval(model.WorkflowApproval{
		OrganizationID: org.ID, ExecutionID: execution.ID, NodeID: "approval",
		Status: model.WorkflowApprovalPending, RequestedAt: now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || approval.ID == 0 {
		t.Fatalf("approval=%+v err=%v", approval, err)
	}
	approval.Status = model.WorkflowApprovalApproved
	approval.DecidedAt = &resumeAt
	approval.DecidedByUserID = &owner.ID
	approval.UpdatedAt = resumeAt
	if _, err := workflows.UpdateWorkflowApproval(approval); err != nil {
		t.Fatal(err)
	}

	checkpoint, err := workflows.CreateWorkflowCheckpoint(model.WorkflowCheckpoint{
		ExecutionID: execution.ID, Status: execution.Status, NodeID: "action",
		Variables: execution.Variables, NextNodeIDs: execution.NextNodeIDs, CreatedAt: now,
	})
	if err != nil || checkpoint.Sequence != 1 {
		t.Fatalf("checkpoint=%+v err=%v", checkpoint, err)
	}
	if _, err := workflows.CreateWorkflowCheckpoint(model.WorkflowCheckpoint{
		ExecutionID: execution.ID, Status: execution.Status, NodeID: "action",
		Variables: execution.Variables, NextNodeIDs: execution.NextNodeIDs, CreatedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	checkpoints, err := workflows.ListWorkflowCheckpoints(execution.ID)
	if err != nil || len(checkpoints) != 2 || checkpoints[1].Sequence != 2 {
		t.Fatalf("checkpoints=%+v err=%v", checkpoints, err)
	}

	execution.Status = model.WorkflowExecutionSucceeded
	execution.ResumeAt = nil
	execution.NextNodeIDs = []string{}
	completedAt := now.Add(2 * time.Minute)
	execution.CompletedAt = &completedAt
	execution.UpdatedAt = completedAt
	updated, err := workflows.UpdateWorkflowExecution(execution)
	if err != nil || updated.Status != model.WorkflowExecutionSucceeded {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	listed, err := workflows.ListWorkflowExecutions(org.ID, 10)
	if err != nil || len(listed) != 1 || listed[0].WorkflowSnapshot.Nodes[1].ID != "action" {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
}
