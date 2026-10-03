package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrWorkflowForbidden         = errors.New("workflow action is forbidden")
	ErrInvalidWorkflow           = errors.New("invalid workflow definition")
	ErrWorkflowVersionImmutable  = errors.New("published workflow version is immutable")
	ErrWorkflowNotPublished      = errors.New("workflow version is not published")
	ErrWorkflowNotActive         = errors.New("workflow has no active published version")
	ErrInvalidWorkflowDecision   = errors.New("invalid workflow approval decision")
	ErrInvalidWorkflowExecution  = errors.New("invalid workflow execution state")
	ErrWorkflowLimit             = errors.New("workflow limit reached")
	ErrWorkflowRecursion         = errors.New("workflow subworkflow recursion detected")
)

type WorkflowService struct {
	repo      repository.WorkflowRepository
	orgs      repository.OrganizationRepository
	executors *WorkflowExecutorRegistry
}

func NewWorkflowService(repo repository.WorkflowRepository, orgs repository.OrganizationRepository, executors *WorkflowExecutorRegistry) *WorkflowService {
	return &WorkflowService{repo: repo, orgs: orgs, executors: executors}
}

func (s *WorkflowService) NodeSchemas(actorUserID, organizationID int64) ([]model.WorkflowNodeSchema, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return workflowNodeSchemas(s.executors), nil
}

func (s *WorkflowService) Workflows(actorUserID, organizationID int64) ([]model.WorkflowDefinition, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListWorkflows(organizationID)
}

func (s *WorkflowService) GetWorkflow(actorUserID, organizationID, workflowID int64) (model.WorkflowDefinition, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return model.WorkflowDefinition{}, err
	}
	return s.repo.GetWorkflow(organizationID, workflowID)
}

func (s *WorkflowService) CreateWorkflow(actorUserID, organizationID int64, req model.CreateWorkflowRequest) (model.WorkflowDefinition, model.WorkflowVersion, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return model.WorkflowDefinition{}, model.WorkflowVersion{}, err
	}
	items, err := s.repo.ListWorkflows(organizationID)
	if err != nil {
		return model.WorkflowDefinition{}, model.WorkflowVersion{}, err
	}
	if len(items) >= 100 {
		return model.WorkflowDefinition{}, model.WorkflowVersion{}, ErrWorkflowLimit
	}
	name := strings.TrimSpace(req.Name)
	description := strings.TrimSpace(req.Description)
	if name == "" || len(name) > 200 || len(description) > 8000 {
		return model.WorkflowDefinition{}, model.WorkflowVersion{}, ErrInvalidWorkflow
	}
	if err := validateWorkflowGraph(req.Graph, s.executors); err != nil {
		return model.WorkflowDefinition{}, model.WorkflowVersion{}, err
	}
	now := time.Now().UTC()
	workflow, err := s.repo.CreateWorkflow(model.WorkflowDefinition{
		OrganizationID:  organizationID,
		Name:            name,
		Description:     description,
		CreatedByUserID: actorUserID,
		UpdatedByUserID: actorUserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return model.WorkflowDefinition{}, model.WorkflowVersion{}, err
	}
	version, err := s.repo.CreateWorkflowVersion(model.WorkflowVersion{
		OrganizationID:  organizationID,
		WorkflowID:      workflow.ID,
		Version:         1,
		Status:          model.WorkflowVersionDraft,
		Graph:           req.Graph,
		CreatedByUserID: actorUserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return model.WorkflowDefinition{}, model.WorkflowVersion{}, err
	}
	s.workflowAudit(organizationID, &actorUserID, "workflow.created", "workflow", fmt.Sprint(workflow.ID), map[string]any{"version_id": version.ID})
	return workflow, version, nil
}

func (s *WorkflowService) Versions(actorUserID, organizationID, workflowID int64) ([]model.WorkflowVersion, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetWorkflow(organizationID, workflowID); err != nil {
		return nil, err
	}
	return s.repo.ListWorkflowVersions(organizationID, workflowID)
}

func (s *WorkflowService) CreateDraft(actorUserID, organizationID, workflowID int64, req model.CreateWorkflowDraftRequest) (model.WorkflowVersion, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return model.WorkflowVersion{}, err
	}
	if _, err := s.repo.GetWorkflow(organizationID, workflowID); err != nil {
		return model.WorkflowVersion{}, err
	}
	graph := model.WorkflowGraph{Nodes: []model.WorkflowNode{}, Edges: []model.WorkflowEdge{}}
	if req.FromVersionID != nil {
		source, err := s.repo.GetWorkflowVersion(organizationID, *req.FromVersionID)
		if err != nil {
			return model.WorkflowVersion{}, err
		}
		if source.WorkflowID != workflowID {
			return model.WorkflowVersion{}, repository.ErrWorkflowVersionNotFound
		}
		graph = source.Graph
	}
	next, err := s.repo.NextWorkflowVersionNumber(workflowID)
	if err != nil {
		return model.WorkflowVersion{}, err
	}
	now := time.Now().UTC()
	version, err := s.repo.CreateWorkflowVersion(model.WorkflowVersion{
		OrganizationID: organizationID, WorkflowID: workflowID, Version: next,
		Status: model.WorkflowVersionDraft, Graph: graph, CreatedByUserID: actorUserID,
		CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		s.workflowAudit(organizationID, &actorUserID, "workflow.version.draft_created", "workflow_version", fmt.Sprint(version.ID), map[string]any{"workflow_id": workflowID, "version": next})
	}
	return version, err
}

func (s *WorkflowService) UpdateDraft(actorUserID, organizationID, workflowID, versionID int64, req model.UpdateWorkflowDraftRequest) (model.WorkflowVersion, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return model.WorkflowVersion{}, err
	}
	item, err := s.repo.GetWorkflowVersion(organizationID, versionID)
	if err != nil {
		return model.WorkflowVersion{}, err
	}
	if item.WorkflowID != workflowID {
		return model.WorkflowVersion{}, repository.ErrWorkflowVersionNotFound
	}
	if item.Status != model.WorkflowVersionDraft {
		return model.WorkflowVersion{}, ErrWorkflowVersionImmutable
	}
	if err := validateWorkflowGraph(req.Graph, s.executors); err != nil {
		return model.WorkflowVersion{}, err
	}
	item.Graph = req.Graph
	item.Checksum = ""
	item.UpdatedAt = time.Now().UTC()
	updated, err := s.repo.UpdateWorkflowVersion(item)
	if err == nil {
		s.workflowAudit(organizationID, &actorUserID, "workflow.version.updated", "workflow_version", fmt.Sprint(versionID), map[string]any{"workflow_id": workflowID})
	}
	return updated, err
}

func (s *WorkflowService) Publish(actorUserID, organizationID, workflowID, versionID int64) (model.WorkflowVersion, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return model.WorkflowVersion{}, err
	}
	item, err := s.repo.GetWorkflowVersion(organizationID, versionID)
	if err != nil {
		return model.WorkflowVersion{}, err
	}
	if item.WorkflowID != workflowID {
		return model.WorkflowVersion{}, repository.ErrWorkflowVersionNotFound
	}
	if item.Status != model.WorkflowVersionDraft {
		return model.WorkflowVersion{}, ErrWorkflowVersionImmutable
	}
	if err := validateWorkflowGraph(item.Graph, s.executors); err != nil {
		return model.WorkflowVersion{}, err
	}
	checksum, err := workflowGraphChecksum(item.Graph)
	if err != nil {
		return model.WorkflowVersion{}, err
	}
	now := time.Now().UTC()
	item.Status = model.WorkflowVersionPublished
	item.Checksum = checksum
	item.PublishedAt = &now
	item.PublishedByID = &actorUserID
	item.UpdatedAt = now
	updated, err := s.repo.UpdateWorkflowVersion(item)
	if err == nil {
		s.workflowAudit(organizationID, &actorUserID, "workflow.version.published", "workflow_version", fmt.Sprint(versionID), map[string]any{"workflow_id": workflowID, "checksum": checksum})
	}
	return updated, err
}

func (s *WorkflowService) Activate(actorUserID, organizationID, workflowID, versionID int64) (model.WorkflowDefinition, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return model.WorkflowDefinition{}, err
	}
	workflow, err := s.repo.GetWorkflow(organizationID, workflowID)
	if err != nil {
		return model.WorkflowDefinition{}, err
	}
	version, err := s.repo.GetWorkflowVersion(organizationID, versionID)
	if err != nil {
		return model.WorkflowDefinition{}, err
	}
	if version.WorkflowID != workflowID {
		return model.WorkflowDefinition{}, repository.ErrWorkflowVersionNotFound
	}
	if version.Status != model.WorkflowVersionPublished {
		return model.WorkflowDefinition{}, ErrWorkflowNotPublished
	}
	workflow.ActiveVersionID = &versionID
	workflow.UpdatedByUserID = actorUserID
	workflow.UpdatedAt = time.Now().UTC()
	updated, err := s.repo.UpdateWorkflow(workflow)
	if err == nil {
		s.workflowAudit(organizationID, &actorUserID, "workflow.activated", "workflow", fmt.Sprint(workflowID), map[string]any{"version_id": versionID, "version": version.Version})
	}
	return updated, err
}

func (s *WorkflowService) Executions(actorUserID, organizationID int64) ([]model.WorkflowExecution, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListWorkflowExecutions(organizationID, 200)
}

func (s *WorkflowService) ExecutionDetail(actorUserID, organizationID, executionID int64) (model.WorkflowExecutionDetail, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	return s.executionDetail(organizationID, executionID)
}

func (s *WorkflowService) executionDetail(organizationID, executionID int64) (model.WorkflowExecutionDetail, error) {
	execution, err := s.repo.GetWorkflowExecution(organizationID, executionID)
	if err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	nodes, err := s.repo.ListWorkflowNodeExecutions(executionID)
	if err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	approvals, err := s.repo.ListWorkflowApprovals(executionID)
	if err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	checkpoints, err := s.repo.ListWorkflowCheckpoints(executionID)
	if err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	return model.WorkflowExecutionDetail{Execution: execution, Nodes: nodes, Approvals: approvals, Checkpoints: checkpoints}, nil
}

func (s *WorkflowService) Start(actorUserID, organizationID, workflowID int64, req model.StartWorkflowExecutionRequest) (model.WorkflowExecutionDetail, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	execution, err := s.startWorkflow(actorUserID, organizationID, workflowID, req, map[int64]bool{})
	if err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	return s.executionDetail(organizationID, execution.ID)
}

func (s *WorkflowService) Trigger(actorUserID, organizationID int64, req model.TriggerWorkflowRequest) ([]model.WorkflowExecution, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	triggerType := normalizeWorkflowTrigger(req.TriggerType)
	if !validWorkflowTrigger(triggerType) || triggerType == model.WorkflowTriggerManual {
		return nil, ErrInvalidWorkflow
	}
	workflows, err := s.repo.ListWorkflows(organizationID)
	if err != nil {
		return nil, err
	}
	created := make([]model.WorkflowExecution, 0)
	for _, workflow := range workflows {
		if workflow.ActiveVersionID == nil {
			continue
		}
		version, err := s.repo.GetWorkflowVersion(organizationID, *workflow.ActiveVersionID)
		if err != nil {
			return nil, err
		}
		if !workflowTriggerMatches(version.Graph, triggerType, req.TriggerKey) {
			continue
		}
		execution, err := s.startWorkflow(actorUserID, organizationID, workflow.ID, model.StartWorkflowExecutionRequest{
			TriggerType: triggerType, TriggerKey: req.TriggerKey, TriggerPayload: req.Payload,
			Variables: map[string]any{}, DryRun: req.DryRun,
		}, map[int64]bool{})
		if err != nil {
			return nil, err
		}
		created = append(created, execution)
	}
	return created, nil
}

func (s *WorkflowService) Cancel(actorUserID, organizationID, executionID int64) (model.WorkflowExecution, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return model.WorkflowExecution{}, err
	}
	execution, err := s.repo.GetWorkflowExecution(organizationID, executionID)
	if err != nil {
		return model.WorkflowExecution{}, err
	}
	if workflowExecutionTerminal(execution.Status) {
		return model.WorkflowExecution{}, ErrInvalidWorkflowExecution
	}
	now := time.Now().UTC()
	execution.Status = model.WorkflowExecutionCancelled
	execution.NextNodeIDs = []string{}
	execution.ResumeAt = nil
	execution.CompletedAt = &now
	execution.UpdatedAt = now
	updated, err := s.repo.UpdateWorkflowExecution(execution)
	if err == nil {
		_ = s.checkpoint(updated, "cancelled")
		s.workflowAudit(organizationID, &actorUserID, "workflow.execution.cancelled", "workflow_execution", fmt.Sprint(executionID), nil)
	}
	return updated, err
}

func (s *WorkflowService) DecideApproval(actorUserID, organizationID, approvalID int64, req model.DecideWorkflowApprovalRequest) (model.WorkflowExecutionDetail, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	approval, err := s.repo.GetWorkflowApproval(organizationID, approvalID)
	if err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	if approval.Status != model.WorkflowApprovalPending {
		return model.WorkflowExecutionDetail{}, ErrInvalidWorkflowDecision
	}
	execution, err := s.repo.GetWorkflowExecution(organizationID, approval.ExecutionID)
	if err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	if execution.Status != model.WorkflowExecutionWaitingApproval {
		return model.WorkflowExecutionDetail{}, ErrInvalidWorkflowDecision
	}
	now := time.Now().UTC()
	approval.DecidedAt = &now
	approval.DecidedByUserID = &actorUserID
	approval.Comment = strings.TrimSpace(req.Comment)
	if req.Approve {
		approval.Status = model.WorkflowApprovalApproved
	} else {
		approval.Status = model.WorkflowApprovalRejected
	}
	approval.UpdatedAt = now
	if _, err := s.repo.UpdateWorkflowApproval(approval); err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	if !req.Approve {
		execution.ErrorMessage = "workflow approval rejected at node " + approval.NodeID
		execution, err = s.failWorkflowExecution(execution, actorUserID, approval.NodeID)
		if err != nil {
			return model.WorkflowExecutionDetail{}, err
		}
		s.workflowAudit(organizationID, &actorUserID, "workflow.approval.rejected", "workflow_approval", fmt.Sprint(approvalID), map[string]any{"execution_id": execution.ID, "node_id": approval.NodeID})
		return s.executionDetail(organizationID, execution.ID)
	}
	execution.Status = model.WorkflowExecutionRunning
	execution.ResumeAt = nil
	execution.UpdatedAt = now
	execution.NextNodeIDs = []string{approval.NodeID}
	execution, err = s.repo.UpdateWorkflowExecution(execution)
	if err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	execution, err = s.advanceWorkflow(execution, actorUserID, map[int64]bool{execution.WorkflowID: true})
	if err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	s.workflowAudit(organizationID, &actorUserID, "workflow.approval.approved", "workflow_approval", fmt.Sprint(approvalID), map[string]any{"execution_id": execution.ID, "node_id": approval.NodeID})
	return s.executionDetail(organizationID, execution.ID)
}

func (s *WorkflowService) Retry(actorUserID, organizationID, executionID int64, req model.RetryWorkflowExecutionRequest) (model.WorkflowExecutionDetail, error) {
	if _, err := s.requireWorkflowAdmin(actorUserID, organizationID); err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	execution, err := s.repo.GetWorkflowExecution(organizationID, executionID)
	if err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	if execution.Status != model.WorkflowExecutionFailed && execution.Status != model.WorkflowExecutionCompensated {
		return model.WorkflowExecutionDetail{}, ErrInvalidWorkflowExecution
	}
	nodeID := strings.TrimSpace(req.NodeID)
	if nodeID == "" {
		runs, err := s.repo.ListWorkflowNodeExecutions(executionID)
		if err != nil {
			return model.WorkflowExecutionDetail{}, err
		}
		for i := len(runs) - 1; i >= 0; i-- {
			if runs[i].Status == model.WorkflowNodeExecutionFailed {
				nodeID = runs[i].NodeID
				break
			}
		}
	}
	if nodeID == "" || workflowNodeByID(execution.WorkflowSnapshot, nodeID) == nil {
		return model.WorkflowExecutionDetail{}, ErrInvalidWorkflowExecution
	}
	execution.Status = model.WorkflowExecutionRunning
	execution.ErrorMessage = ""
	execution.CompletedAt = nil
	execution.ResumeAt = nil
	execution.NextNodeIDs = []string{nodeID}
	execution.UpdatedAt = time.Now().UTC()
	execution, err = s.repo.UpdateWorkflowExecution(execution)
	if err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	execution, err = s.advanceWorkflow(execution, actorUserID, map[int64]bool{execution.WorkflowID: true})
	if err != nil {
		return model.WorkflowExecutionDetail{}, err
	}
	s.workflowAudit(organizationID, &actorUserID, "workflow.execution.manual_retry", "workflow_execution", fmt.Sprint(executionID), map[string]any{"node_id": nodeID})
	return s.executionDetail(organizationID, execution.ID)
}

func (s *WorkflowService) ProcessDueSystem(limit int) ([]model.WorkflowExecution, error) {
	if limit <= 0 {
		limit = 50
	}
	items, err := s.repo.ListDueWorkflowExecutions(time.Now().UTC(), limit)
	if err != nil {
		return nil, err
	}
	processed := make([]model.WorkflowExecution, 0, len(items))
	for _, execution := range items {
		workflow, err := s.repo.GetWorkflow(execution.OrganizationID, execution.WorkflowID)
		if err != nil {
			return nil, err
		}
		execution.Status = model.WorkflowExecutionRunning
		execution.ResumeAt = nil
		execution.UpdatedAt = time.Now().UTC()
		execution, err = s.repo.UpdateWorkflowExecution(execution)
		if err != nil {
			return nil, err
		}
		execution, err = s.advanceWorkflow(execution, workflow.CreatedByUserID, map[int64]bool{execution.WorkflowID: true})
		if err != nil {
			return nil, err
		}
		processed = append(processed, execution)
	}
	return processed, nil
}

func (s *WorkflowService) requireWorkflowAdmin(userID, organizationID int64) (model.OrganizationMember, error) {
	member, err := s.orgs.GetMember(organizationID, userID)
	if err != nil {
		return model.OrganizationMember{}, ErrWorkflowForbidden
	}
	switch member.Role {
	case model.OrganizationRoleOwner, model.OrganizationRoleAdmin, model.OrganizationRoleDelegatedAdmin:
		return member, nil
	default:
		return model.OrganizationMember{}, ErrWorkflowForbidden
	}
}

func (s *WorkflowService) workflowAudit(organizationID int64, actorUserID *int64, action, resourceType, resourceID string, metadata map[string]any) {
	_ = s.orgs.RecordAudit(model.OrganizationAuditEvent{
		OrganizationID: organizationID, ActorUserID: actorUserID, Action: action,
		ResourceType: resourceType, ResourceID: resourceID, Metadata: metadata,
		CreatedAt: time.Now().UTC(),
	})
}

func workflowGraphChecksum(graph model.WorkflowGraph) (string, error) {
	raw, err := json.Marshal(graph)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
