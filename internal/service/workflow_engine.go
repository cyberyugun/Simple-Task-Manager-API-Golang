package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func (s *WorkflowService) startWorkflow(actorUserID, organizationID, workflowID int64, req model.StartWorkflowExecutionRequest, stack map[int64]bool) (model.WorkflowExecution, error) {
	if stack[workflowID] {
		return model.WorkflowExecution{}, ErrWorkflowRecursion
	}
	nextStack := make(map[int64]bool, len(stack)+1)
	for id, value := range stack {
		nextStack[id] = value
	}
	nextStack[workflowID] = true

	workflow, err := s.repo.GetWorkflow(organizationID, workflowID)
	if err != nil {
		return model.WorkflowExecution{}, err
	}
	if workflow.ActiveVersionID == nil {
		return model.WorkflowExecution{}, ErrWorkflowNotActive
	}
	version, err := s.repo.GetWorkflowVersion(organizationID, *workflow.ActiveVersionID)
	if err != nil {
		return model.WorkflowExecution{}, err
	}
	if version.Status != model.WorkflowVersionPublished || version.WorkflowID != workflowID {
		return model.WorkflowExecution{}, ErrWorkflowNotActive
	}
	triggerType := normalizeWorkflowTrigger(req.TriggerType)
	if triggerType == "" {
		triggerType = model.WorkflowTriggerManual
	}
	if !validWorkflowTrigger(triggerType) || !workflowTriggerMatches(version.Graph, triggerType, req.TriggerKey) {
		return model.WorkflowExecution{}, ErrInvalidWorkflow
	}
	trigger := workflowTriggerNode(version.Graph)
	if trigger == nil {
		return model.WorkflowExecution{}, ErrInvalidWorkflow
	}
	variables := cloneWorkflowVariables(req.Variables)
	payload := cloneWorkflowVariables(req.TriggerPayload)
	variables["trigger"] = payload
	variables["trigger_type"] = triggerType
	variables["trigger_key"] = strings.TrimSpace(req.TriggerKey)

	now := time.Now().UTC()
	actor := actorUserID
	execution, err := s.repo.CreateWorkflowExecution(model.WorkflowExecution{
		OrganizationID: organizationID, WorkflowID: workflowID, WorkflowVersionID: version.ID,
		Status: model.WorkflowExecutionRunning, TriggerType: triggerType,
		TriggerKey: strings.TrimSpace(req.TriggerKey), TriggerPayload: payload,
		Variables: variables, WorkflowSnapshot: version.Graph, NextNodeIDs: []string{trigger.ID},
		DryRun: req.DryRun, RequestedByUserID: &actor, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return model.WorkflowExecution{}, err
	}
	_ = s.checkpoint(execution, "started")
	s.workflowAudit(organizationID, &actor, "workflow.execution.started", "workflow_execution", fmt.Sprint(execution.ID), map[string]any{
		"workflow_id": workflowID, "version_id": version.ID, "trigger_type": triggerType,
		"trigger_key": execution.TriggerKey, "dry_run": execution.DryRun,
	})
	return s.advanceWorkflow(execution, actorUserID, nextStack)
}

func (s *WorkflowService) advanceWorkflow(execution model.WorkflowExecution, actorUserID int64, stack map[int64]bool) (model.WorkflowExecution, error) {
	if workflowExecutionTerminal(execution.Status) {
		return execution, nil
	}
	execution.Status = model.WorkflowExecutionRunning
	execution.ResumeAt = nil
	execution.UpdatedAt = time.Now().UTC()
	queue := workflowDedupeNodeIDs(execution.NextNodeIDs)
	execution.NextNodeIDs = []string{}
	processed := make(map[string]bool)
	steps := 0
	joinDeferrals := 0

	for len(queue) > 0 {
		if steps >= 1000 {
			execution.ErrorMessage = "workflow execution exceeded step safety limit"
			return s.failWorkflowExecution(execution, actorUserID, "")
		}
		steps++
		nodeID := queue[0]
		queue = queue[1:]
		if processed[nodeID] {
			continue
		}
		node := workflowNodeByID(execution.WorkflowSnapshot, nodeID)
		if node == nil {
			execution.ErrorMessage = "workflow snapshot references missing node " + nodeID
			return s.failWorkflowExecution(execution, actorUserID, nodeID)
		}
		if node.Type == model.WorkflowNodeJoin {
			ready, err := s.workflowJoinReady(execution.ID, execution.WorkflowSnapshot, *node)
			if err != nil {
				return model.WorkflowExecution{}, err
			}
			if !ready {
				queue = append(queue, nodeID)
				joinDeferrals++
				if joinDeferrals > len(execution.WorkflowSnapshot.Nodes)*2 && len(queue) == 1 {
					execution.ErrorMessage = "workflow join cannot be satisfied"
					return s.failWorkflowExecution(execution, actorUserID, nodeID)
				}
				continue
			}
		}
		processed[nodeID] = true
		next, wait, failed, err := s.executeWorkflowNode(&execution, actorUserID, *node, stack)
		if err != nil {
			return model.WorkflowExecution{}, err
		}
		if failed {
			return s.failWorkflowExecution(execution, actorUserID, nodeID)
		}
		if wait {
			execution.NextNodeIDs = workflowDedupeNodeIDs(append([]string{nodeID}, queue...))
			execution.UpdatedAt = time.Now().UTC()
			updated, err := s.repo.UpdateWorkflowExecution(execution)
			if err != nil {
				return model.WorkflowExecution{}, err
			}
			_ = s.checkpoint(updated, nodeID)
			return updated, nil
		}
		queue = workflowDedupeNodeIDs(append(queue, next...))
		execution.NextNodeIDs = append([]string(nil), queue...)
		execution.UpdatedAt = time.Now().UTC()
		updated, err := s.repo.UpdateWorkflowExecution(execution)
		if err != nil {
			return model.WorkflowExecution{}, err
		}
		execution = updated
		_ = s.checkpoint(execution, nodeID)
	}

	now := time.Now().UTC()
	execution.Status = model.WorkflowExecutionSucceeded
	execution.NextNodeIDs = []string{}
	execution.ResumeAt = nil
	execution.CompletedAt = &now
	execution.UpdatedAt = now
	updated, err := s.repo.UpdateWorkflowExecution(execution)
	if err != nil {
		return model.WorkflowExecution{}, err
	}
	_ = s.checkpoint(updated, "completed")
	s.workflowAudit(updated.OrganizationID, optionalWorkflowActor(actorUserID), "workflow.execution.completed", "workflow_execution", fmt.Sprint(updated.ID), map[string]any{"status": updated.Status})
	return updated, nil
}

func (s *WorkflowService) executeWorkflowNode(execution *model.WorkflowExecution, actorUserID int64, node model.WorkflowNode, stack map[int64]bool) ([]string, bool, bool, error) {
	switch node.Type {
	case model.WorkflowNodeTrigger:
		output := map[string]any{"trigger_type": execution.TriggerType, "trigger_key": execution.TriggerKey, "payload": execution.TriggerPayload}
		if _, err := s.recordWorkflowNodeSuccess(execution.ID, node, 1, map[string]any{}, output); err != nil {
			return nil, false, false, err
		}
		workflowApplyOutputMapping(execution.Variables, node.OutputMapping, output)
		return workflowOutgoing(execution.WorkflowSnapshot, node.ID, ""), false, false, nil

	case model.WorkflowNodeCondition, model.WorkflowNodeBranch:
		input := workflowResolveMapping(node.InputMapping, execution.Variables)
		matched := workflowEvaluateCondition(node.Config, execution.Variables, input)
		output := map[string]any{"matched": matched}
		if _, err := s.recordWorkflowNodeSuccess(execution.ID, node, 1, input, output); err != nil {
			return nil, false, false, err
		}
		workflowApplyOutputMapping(execution.Variables, node.OutputMapping, output)
		when := "false"
		if matched {
			when = "true"
		}
		return workflowOutgoing(execution.WorkflowSnapshot, node.ID, when), false, false, nil

	case model.WorkflowNodeTransform:
		input := workflowResolveMapping(node.InputMapping, execution.Variables)
		output := make(map[string]any)
		if raw, ok := node.Config["set"].(map[string]any); ok {
			for key, value := range raw {
				resolved := workflowMaterialize(value, execution.Variables)
				execution.Variables[key] = resolved
				output[key] = resolved
			}
		}
		for key, value := range input {
			output[key] = value
		}
		if _, err := s.recordWorkflowNodeSuccess(execution.ID, node, 1, input, output); err != nil {
			return nil, false, false, err
		}
		workflowApplyOutputMapping(execution.Variables, node.OutputMapping, output)
		return workflowOutgoing(execution.WorkflowSnapshot, node.ID, ""), false, false, nil

	case model.WorkflowNodeApproval:
		if execution.DryRun {
			if _, err := s.recordWorkflowNodeSuccess(execution.ID, node, 1, map[string]any{}, map[string]any{"dry_run": true, "approved": true}); err != nil {
				return nil, false, false, err
			}
			return workflowOutgoing(execution.WorkflowSnapshot, node.ID, ""), false, false, nil
		}
		approval, err := s.repo.FindWorkflowApproval(execution.ID, node.ID)
		if errors.Is(err, repository.ErrWorkflowApprovalNotFound) {
			now := time.Now().UTC()
			approval, err = s.repo.CreateWorkflowApproval(model.WorkflowApproval{
				OrganizationID: execution.OrganizationID, ExecutionID: execution.ID, NodeID: node.ID,
				Status: model.WorkflowApprovalPending, RequestedAt: now, CreatedAt: now, UpdatedAt: now,
			})
			if err != nil {
				return nil, false, false, err
			}
			if _, err := s.recordWorkflowNodeWaiting(execution.ID, node, 1, nil, nil); err != nil {
				return nil, false, false, err
			}
			execution.Status = model.WorkflowExecutionWaitingApproval
			execution.ResumeAt = nil
			s.workflowAudit(execution.OrganizationID, optionalWorkflowActor(actorUserID), "workflow.approval.requested", "workflow_approval", fmt.Sprint(approval.ID), map[string]any{"execution_id": execution.ID, "node_id": node.ID})
			return nil, true, false, nil
		}
		if err != nil {
			return nil, false, false, err
		}
		switch approval.Status {
		case model.WorkflowApprovalPending:
			execution.Status = model.WorkflowExecutionWaitingApproval
			execution.ResumeAt = nil
			return nil, true, false, nil
		case model.WorkflowApprovalRejected:
			execution.ErrorMessage = "workflow approval rejected at node " + node.ID
			return nil, false, true, nil
		case model.WorkflowApprovalApproved:
			run, err := s.repo.LatestWorkflowNodeExecution(execution.ID, node.ID)
			if err == nil && run.Status == model.WorkflowNodeExecutionWaiting {
				now := time.Now().UTC()
				run.Status = model.WorkflowNodeExecutionSucceeded
				run.Output = map[string]any{"approved": true, "approval_id": approval.ID}
				run.CompletedAt = &now
				run.UpdatedAt = now
				if _, err := s.repo.UpdateWorkflowNodeExecution(run); err != nil {
					return nil, false, false, err
				}
			}
			execution.Status = model.WorkflowExecutionRunning
			return workflowOutgoing(execution.WorkflowSnapshot, node.ID, ""), false, false, nil
		default:
			return nil, false, false, ErrInvalidWorkflowDecision
		}

	case model.WorkflowNodeDelay:
		if execution.DryRun {
			if _, err := s.recordWorkflowNodeSuccess(execution.ID, node, 1, map[string]any{}, map[string]any{"dry_run": true}); err != nil {
				return nil, false, false, err
			}
			return workflowOutgoing(execution.WorkflowSnapshot, node.ID, ""), false, false, nil
		}
		delay := workflowConfigDuration(node.Config, "seconds", time.Second)
		latest, err := s.repo.LatestWorkflowNodeExecution(execution.ID, node.ID)
		if errors.Is(err, repository.ErrWorkflowNodeRunNotFound) {
			now := time.Now().UTC()
			resumeAt := now.Add(delay)
			if _, err := s.recordWorkflowNodeWaiting(execution.ID, node, 1, &resumeAt, map[string]any{"delay_seconds": int(delay.Seconds())}); err != nil {
				return nil, false, false, err
			}
			execution.Status = model.WorkflowExecutionWaitingDelay
			execution.ResumeAt = &resumeAt
			return nil, true, false, nil
		}
		if err != nil {
			return nil, false, false, err
		}
		if latest.Status == model.WorkflowNodeExecutionWaiting {
			if latest.RetryAt != nil && latest.RetryAt.After(time.Now().UTC()) {
				execution.Status = model.WorkflowExecutionWaitingDelay
				execution.ResumeAt = latest.RetryAt
				return nil, true, false, nil
			}
			now := time.Now().UTC()
			latest.Status = model.WorkflowNodeExecutionSucceeded
			latest.CompletedAt = &now
			latest.UpdatedAt = now
			if _, err := s.repo.UpdateWorkflowNodeExecution(latest); err != nil {
				return nil, false, false, err
			}
		}
		execution.Status = model.WorkflowExecutionRunning
		execution.ResumeAt = nil
		return workflowOutgoing(execution.WorkflowSnapshot, node.ID, ""), false, false, nil

	case model.WorkflowNodeParallel:
		if _, err := s.recordWorkflowNodeSuccess(execution.ID, node, 1, map[string]any{}, map[string]any{"branches": len(workflowOutgoing(execution.WorkflowSnapshot, node.ID, ""))}); err != nil {
			return nil, false, false, err
		}
		return workflowOutgoing(execution.WorkflowSnapshot, node.ID, ""), false, false, nil

	case model.WorkflowNodeJoin:
		if _, err := s.recordWorkflowNodeSuccess(execution.ID, node, 1, map[string]any{}, map[string]any{"joined": true}); err != nil {
			return nil, false, false, err
		}
		return workflowOutgoing(execution.WorkflowSnapshot, node.ID, ""), false, false, nil

	case model.WorkflowNodeAction:
		return s.executeWorkflowActionNode(execution, actorUserID, node)

	case model.WorkflowNodeSubworkflow:
		return s.executeSubworkflowNode(execution, actorUserID, node, stack)

	case model.WorkflowNodeEnd:
		if _, err := s.recordWorkflowNodeSuccess(execution.ID, node, 1, map[string]any{}, map[string]any{"ended": true}); err != nil {
			return nil, false, false, err
		}
		return nil, false, false, nil
	default:
		execution.ErrorMessage = "unsupported workflow node type " + node.Type
		return nil, false, true, nil
	}
}

func (s *WorkflowService) executeWorkflowActionNode(execution *model.WorkflowExecution, actorUserID int64, node model.WorkflowNode) ([]string, bool, bool, error) {
	executorName := strings.ToLower(strings.TrimSpace(fmt.Sprint(node.Config["executor"])))
	executor, ok := s.executors.Get(executorName)
	if !ok {
		execution.ErrorMessage = ErrWorkflowExecutorNotAllowed.Error() + ": " + executorName
		return nil, false, true, nil
	}
	attempt := 1
	latest, err := s.repo.LatestWorkflowNodeExecution(execution.ID, node.ID)
	if err == nil {
		switch latest.Status {
		case model.WorkflowNodeExecutionSucceeded, model.WorkflowNodeExecutionCompensated:
			return workflowOutgoing(execution.WorkflowSnapshot, node.ID, ""), false, false, nil
		case model.WorkflowNodeExecutionRetryWaiting:
			if latest.RetryAt != nil && latest.RetryAt.After(time.Now().UTC()) {
				execution.Status = model.WorkflowExecutionWaitingDelay
				execution.ResumeAt = latest.RetryAt
				return nil, true, false, nil
			}
			attempt = latest.Attempt + 1
		case model.WorkflowNodeExecutionFailed:
			attempt = latest.Attempt + 1
		default:
			attempt = latest.Attempt + 1
		}
	} else if !errors.Is(err, repository.ErrWorkflowNodeRunNotFound) {
		return nil, false, false, err
	}

	input := workflowResolveMapping(node.InputMapping, execution.Variables)
	params := make(map[string]any)
	if raw, ok := node.Config["params"].(map[string]any); ok {
		for key, value := range raw {
			params[key] = workflowMaterialize(value, execution.Variables)
		}
	}
	for key, value := range input {
		params[key] = value
	}
	now := time.Now().UTC()
	run, err := s.repo.CreateWorkflowNodeExecution(model.WorkflowNodeExecution{
		ExecutionID: execution.ID, NodeID: node.ID, NodeType: node.Type,
		Status: model.WorkflowNodeExecutionRunning, Attempt: attempt, Input: input,
		Output: map[string]any{}, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return nil, false, false, err
	}
	timeout := time.Duration(node.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	output, actionErr := executor.Execute(ctx, WorkflowActionContext{
		OrganizationID: execution.OrganizationID, ExecutionID: execution.ID, ActorUserID: actorUserID,
		DryRun: execution.DryRun, Params: params, Variables: execution.Variables,
	})
	completed := time.Now().UTC()
	run.CompletedAt = &completed
	run.UpdatedAt = completed
	if actionErr == nil {
		run.Status = model.WorkflowNodeExecutionSucceeded
		run.Output = output
		if _, err := s.repo.UpdateWorkflowNodeExecution(run); err != nil {
			return nil, false, false, err
		}
		workflowApplyOutputMapping(execution.Variables, node.OutputMapping, output)
		return workflowOutgoing(execution.WorkflowSnapshot, node.ID, ""), false, false, nil
	}
	run.ErrorMessage = actionErr.Error()
	maxAttempts := node.Retry.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	if attempt < maxAttempts {
		backoff := node.Retry.BackoffSeconds
		if backoff <= 0 {
			backoff = 1
		}
		retryAt := completed.Add(time.Duration(backoff) * time.Second)
		run.Status = model.WorkflowNodeExecutionRetryWaiting
		run.RetryAt = &retryAt
		if _, err := s.repo.UpdateWorkflowNodeExecution(run); err != nil {
			return nil, false, false, err
		}
		execution.Status = model.WorkflowExecutionWaitingDelay
		execution.ResumeAt = &retryAt
		execution.ErrorMessage = actionErr.Error()
		return nil, true, false, nil
	}
	run.Status = model.WorkflowNodeExecutionFailed
	if _, err := s.repo.UpdateWorkflowNodeExecution(run); err != nil {
		return nil, false, false, err
	}
	execution.ErrorMessage = actionErr.Error()
	return nil, false, true, nil
}

func (s *WorkflowService) executeSubworkflowNode(execution *model.WorkflowExecution, actorUserID int64, node model.WorkflowNode, stack map[int64]bool) ([]string, bool, bool, error) {
	childID, ok := workflowInt64(node.Config["workflow_id"])
	if !ok || childID <= 0 || childID == execution.WorkflowID {
		execution.ErrorMessage = ErrWorkflowRecursion.Error()
		return nil, false, true, nil
	}
	latest, err := s.repo.LatestWorkflowNodeExecution(execution.ID, node.ID)
	if errors.Is(err, repository.ErrWorkflowNodeRunNotFound) {
		if execution.DryRun {
			if _, err := s.recordWorkflowNodeSuccess(execution.ID, node, 1, map[string]any{}, map[string]any{"dry_run": true, "workflow_id": childID}); err != nil {
				return nil, false, false, err
			}
			return workflowOutgoing(execution.WorkflowSnapshot, node.ID, ""), false, false, nil
		}
		child, err := s.startWorkflow(actorUserID, execution.OrganizationID, childID, model.StartWorkflowExecutionRequest{
			TriggerType: model.WorkflowTriggerInternal, TriggerKey: "subworkflow",
			TriggerPayload: map[string]any{"parent_execution_id": execution.ID, "parent_node_id": node.ID},
			Variables: cloneWorkflowVariables(execution.Variables),
		}, stack)
		if err != nil {
			execution.ErrorMessage = err.Error()
			return nil, false, true, nil
		}
		now := time.Now().UTC()
		if child.Status == model.WorkflowExecutionSucceeded {
			if _, err := s.recordWorkflowNodeSuccess(execution.ID, node, 1, map[string]any{}, map[string]any{"child_execution_id": child.ID}); err != nil {
				return nil, false, false, err
			}
			return workflowOutgoing(execution.WorkflowSnapshot, node.ID, ""), false, false, nil
		}
		if workflowExecutionTerminal(child.Status) {
			execution.ErrorMessage = "subworkflow execution failed"
			return nil, false, true, nil
		}
		resumeAt := now.Add(5 * time.Second)
		if _, err := s.recordWorkflowNodeWaiting(execution.ID, node, 1, &resumeAt, map[string]any{"child_execution_id": child.ID}); err != nil {
			return nil, false, false, err
		}
		execution.Status = model.WorkflowExecutionWaitingDelay
		execution.ResumeAt = &resumeAt
		return nil, true, false, nil
	}
	if err != nil {
		return nil, false, false, err
	}
	childIDValue, ok := workflowInt64(latest.Output["child_execution_id"])
	if !ok {
		execution.ErrorMessage = "subworkflow checkpoint missing child execution"
		return nil, false, true, nil
	}
	child, err := s.repo.GetWorkflowExecution(execution.OrganizationID, childIDValue)
	if err != nil {
		return nil, false, false, err
	}
	if child.Status == model.WorkflowExecutionSucceeded {
		now := time.Now().UTC()
		latest.Status = model.WorkflowNodeExecutionSucceeded
		latest.CompletedAt = &now
		latest.UpdatedAt = now
		if _, err := s.repo.UpdateWorkflowNodeExecution(latest); err != nil {
			return nil, false, false, err
		}
		execution.Status = model.WorkflowExecutionRunning
		execution.ResumeAt = nil
		return workflowOutgoing(execution.WorkflowSnapshot, node.ID, ""), false, false, nil
	}
	if workflowExecutionTerminal(child.Status) {
		execution.ErrorMessage = "subworkflow execution ended with status " + child.Status
		return nil, false, true, nil
	}
	resumeAt := time.Now().UTC().Add(5 * time.Second)
	latest.RetryAt = &resumeAt
	latest.UpdatedAt = time.Now().UTC()
	if _, err := s.repo.UpdateWorkflowNodeExecution(latest); err != nil {
		return nil, false, false, err
	}
	execution.Status = model.WorkflowExecutionWaitingDelay
	execution.ResumeAt = &resumeAt
	return nil, true, false, nil
}

func (s *WorkflowService) failWorkflowExecution(execution model.WorkflowExecution, actorUserID int64, failedNodeID string) (model.WorkflowExecution, error) {
	runs, err := s.repo.ListWorkflowNodeExecutions(execution.ID)
	if err != nil {
		return model.WorkflowExecution{}, err
	}
	compensations := make([]model.WorkflowNode, 0)
	seen := make(map[string]bool)
	for i := len(runs) - 1; i >= 0; i-- {
		run := runs[i]
		if run.Status != model.WorkflowNodeExecutionSucceeded || seen[run.NodeID] {
			continue
		}
		seen[run.NodeID] = true
		node := workflowNodeByID(execution.WorkflowSnapshot, run.NodeID)
		if node == nil || strings.TrimSpace(node.CompensationNodeID) == "" {
			continue
		}
		comp := workflowNodeByID(execution.WorkflowSnapshot, node.CompensationNodeID)
		if comp != nil && comp.Type == model.WorkflowNodeAction {
			compensations = append(compensations, *comp)
		}
	}
	if len(compensations) > 0 {
		execution.Status = model.WorkflowExecutionCompensating
		execution.NextNodeIDs = []string{}
		execution.ResumeAt = nil
		execution.UpdatedAt = time.Now().UTC()
		execution, err = s.repo.UpdateWorkflowExecution(execution)
		if err != nil {
			return model.WorkflowExecution{}, err
		}
		for _, comp := range compensations {
			if err := s.executeCompensation(&execution, actorUserID, comp); err != nil {
				execution.Status = model.WorkflowExecutionFailed
				execution.ErrorMessage = strings.TrimSpace(execution.ErrorMessage + "; compensation failed: " + err.Error())
				now := time.Now().UTC()
				execution.CompletedAt = &now
				execution.UpdatedAt = now
				updated, updateErr := s.repo.UpdateWorkflowExecution(execution)
				if updateErr != nil {
					return model.WorkflowExecution{}, updateErr
				}
				_ = s.checkpoint(updated, failedNodeID)
				return updated, nil
			}
		}
		now := time.Now().UTC()
		execution.Status = model.WorkflowExecutionCompensated
		execution.CompletedAt = &now
		execution.UpdatedAt = now
		updated, err := s.repo.UpdateWorkflowExecution(execution)
		if err != nil {
			return model.WorkflowExecution{}, err
		}
		_ = s.checkpoint(updated, failedNodeID)
		s.workflowAudit(updated.OrganizationID, optionalWorkflowActor(actorUserID), "workflow.execution.compensated", "workflow_execution", fmt.Sprint(updated.ID), map[string]any{"failed_node_id": failedNodeID})
		return updated, nil
	}
	now := time.Now().UTC()
	execution.Status = model.WorkflowExecutionFailed
	execution.NextNodeIDs = []string{}
	execution.ResumeAt = nil
	execution.CompletedAt = &now
	execution.UpdatedAt = now
	updated, err := s.repo.UpdateWorkflowExecution(execution)
	if err != nil {
		return model.WorkflowExecution{}, err
	}
	_ = s.checkpoint(updated, failedNodeID)
	s.workflowAudit(updated.OrganizationID, optionalWorkflowActor(actorUserID), "workflow.execution.failed", "workflow_execution", fmt.Sprint(updated.ID), map[string]any{"failed_node_id": failedNodeID, "error": updated.ErrorMessage})
	return updated, nil
}

func (s *WorkflowService) executeCompensation(execution *model.WorkflowExecution, actorUserID int64, node model.WorkflowNode) error {
	executorName := strings.ToLower(strings.TrimSpace(fmt.Sprint(node.Config["executor"])))
	executor, ok := s.executors.Get(executorName)
	if !ok {
		return ErrWorkflowExecutorNotAllowed
	}
	input := workflowResolveMapping(node.InputMapping, execution.Variables)
	params := make(map[string]any)
	if raw, ok := node.Config["params"].(map[string]any); ok {
		for key, value := range raw {
			params[key] = workflowMaterialize(value, execution.Variables)
		}
	}
	for key, value := range input {
		params[key] = value
	}
	timeout := time.Duration(node.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	output, err := executor.Execute(ctx, WorkflowActionContext{
		OrganizationID: execution.OrganizationID, ExecutionID: execution.ID,
		ActorUserID: actorUserID, DryRun: execution.DryRun, Params: params, Variables: execution.Variables,
	})
	now := time.Now().UTC()
	status := model.WorkflowNodeExecutionCompensated
	message := ""
	if err != nil {
		status = model.WorkflowNodeExecutionFailed
		message = err.Error()
	}
	_, recordErr := s.repo.CreateWorkflowNodeExecution(model.WorkflowNodeExecution{
		ExecutionID: execution.ID, NodeID: node.ID, NodeType: node.Type, Status: status,
		Attempt: 1, Input: input, Output: output, ErrorMessage: message,
		StartedAt: now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	})
	if recordErr != nil {
		return recordErr
	}
	if err == nil {
		workflowApplyOutputMapping(execution.Variables, node.OutputMapping, output)
	}
	return err
}

func (s *WorkflowService) recordWorkflowNodeSuccess(executionID int64, node model.WorkflowNode, attempt int, input, output map[string]any) (model.WorkflowNodeExecution, error) {
	now := time.Now().UTC()
	if input == nil {
		input = map[string]any{}
	}
	if output == nil {
		output = map[string]any{}
	}
	return s.repo.CreateWorkflowNodeExecution(model.WorkflowNodeExecution{
		ExecutionID: executionID, NodeID: node.ID, NodeType: node.Type,
		Status: model.WorkflowNodeExecutionSucceeded, Attempt: attempt, Input: input, Output: output,
		StartedAt: now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	})
}

func (s *WorkflowService) recordWorkflowNodeWaiting(executionID int64, node model.WorkflowNode, attempt int, retryAt *time.Time, output map[string]any) (model.WorkflowNodeExecution, error) {
	now := time.Now().UTC()
	if output == nil {
		output = map[string]any{}
	}
	return s.repo.CreateWorkflowNodeExecution(model.WorkflowNodeExecution{
		ExecutionID: executionID, NodeID: node.ID, NodeType: node.Type,
		Status: model.WorkflowNodeExecutionWaiting, Attempt: attempt, Input: map[string]any{},
		Output: output, RetryAt: retryAt, StartedAt: now, CreatedAt: now, UpdatedAt: now,
	})
}

func (s *WorkflowService) checkpoint(execution model.WorkflowExecution, nodeID string) error {
	_, err := s.repo.CreateWorkflowCheckpoint(model.WorkflowCheckpoint{
		ExecutionID: execution.ID, Status: execution.Status, NodeID: nodeID,
		Variables: cloneWorkflowVariables(execution.Variables),
		NextNodeIDs: append([]string(nil), execution.NextNodeIDs...), CreatedAt: time.Now().UTC(),
	})
	return err
}

func (s *WorkflowService) workflowJoinReady(executionID int64, graph model.WorkflowGraph, node model.WorkflowNode) (bool, error) {
	incoming := make([]string, 0)
	for _, edge := range graph.Edges {
		if edge.To == node.ID {
			incoming = append(incoming, edge.From)
		}
	}
	if len(incoming) == 0 {
		return true, nil
	}
	mode := strings.ToLower(strings.TrimSpace(fmt.Sprint(node.Config["mode"])))
	if mode == "" {
		mode = "all"
	}
	satisfied := 0
	for _, predecessor := range incoming {
		run, err := s.repo.LatestWorkflowNodeExecution(executionID, predecessor)
		if errors.Is(err, repository.ErrWorkflowNodeRunNotFound) {
			continue
		}
		if err != nil {
			return false, err
		}
		if run.Status == model.WorkflowNodeExecutionSucceeded || run.Status == model.WorkflowNodeExecutionSkipped || run.Status == model.WorkflowNodeExecutionCompensated {
			satisfied++
		}
	}
	if mode == "any" {
		return satisfied > 0, nil
	}
	return satisfied == len(incoming), nil
}

func validateWorkflowGraph(graph model.WorkflowGraph, executors *WorkflowExecutorRegistry) error {
	if len(graph.Nodes) < 2 || len(graph.Nodes) > 250 || len(graph.Edges) > 1000 {
		return ErrInvalidWorkflow
	}
	nodes := make(map[string]model.WorkflowNode, len(graph.Nodes))
	triggerCount := 0
	endCount := 0
	for _, node := range graph.Nodes {
		node.ID = strings.TrimSpace(node.ID)
		node.Type = strings.ToLower(strings.TrimSpace(node.Type))
		if node.ID == "" || len(node.ID) > 100 || nodes[node.ID].ID != "" || !validWorkflowNodeType(node.Type) {
			return ErrInvalidWorkflow
		}
		nodes[node.ID] = node
		if node.Type == model.WorkflowNodeTrigger {
			triggerCount++
			source := normalizeWorkflowTrigger(fmt.Sprint(node.Config["source"]))
			if source == "" {
				source = model.WorkflowTriggerManual
			}
			if !validWorkflowTrigger(source) {
				return ErrInvalidWorkflow
			}
		}
		if node.Type == model.WorkflowNodeEnd {
			endCount++
		}
		if node.Retry.MaxAttempts < 0 || node.Retry.MaxAttempts > 10 || node.Retry.BackoffSeconds < 0 || node.Retry.BackoffSeconds > 86400 ||
			node.TimeoutSeconds < 0 || node.TimeoutSeconds > 3600 {
			return ErrInvalidWorkflow
		}
		if node.Type == model.WorkflowNodeAction {
			name := strings.ToLower(strings.TrimSpace(fmt.Sprint(node.Config["executor"])))
			if _, ok := executors.Get(name); !ok {
				return ErrWorkflowExecutorNotAllowed
			}
		}
		if node.Type == model.WorkflowNodeDelay {
			seconds, ok := workflowInt64(node.Config["seconds"])
			if !ok || seconds < 1 || seconds > 2592000 {
				return ErrInvalidWorkflow
			}
		}
		if node.Type == model.WorkflowNodeSubworkflow {
			id, ok := workflowInt64(node.Config["workflow_id"])
			if !ok || id <= 0 {
				return ErrInvalidWorkflow
			}
		}
	}
	if triggerCount != 1 || endCount < 1 {
		return ErrInvalidWorkflow
	}
	for _, node := range graph.Nodes {
		if node.CompensationNodeID == "" {
			continue
		}
		comp, ok := nodes[node.CompensationNodeID]
		if !ok || comp.Type != model.WorkflowNodeAction || node.Type != model.WorkflowNodeAction {
			return ErrInvalidWorkflow
		}
	}
	indegree := make(map[string]int, len(nodes))
	outgoing := make(map[string][]string, len(nodes))
	for id := range nodes {
		indegree[id] = 0
	}
	for _, edge := range graph.Edges {
		from := strings.TrimSpace(edge.From)
		to := strings.TrimSpace(edge.To)
		if from == "" || to == "" || from == to {
			return ErrInvalidWorkflow
		}
		fromNode, fromOK := nodes[from]
		toNode, toOK := nodes[to]
		if !fromOK || !toOK || toNode.Type == model.WorkflowNodeTrigger || fromNode.Type == model.WorkflowNodeEnd {
			return ErrInvalidWorkflow
		}
		indegree[to]++
		outgoing[from] = append(outgoing[from], to)
	}
	queue := make([]string, 0)
	for id, degree := range indegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range outgoing[id] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if visited != len(nodes) {
		return ErrInvalidWorkflow
	}
	trigger := workflowTriggerNode(graph)
	if trigger == nil || len(workflowOutgoing(graph, trigger.ID, "")) == 0 {
		return ErrInvalidWorkflow
	}
	return nil
}

func validWorkflowNodeType(value string) bool {
	switch value {
	case model.WorkflowNodeTrigger, model.WorkflowNodeCondition, model.WorkflowNodeTransform,
		model.WorkflowNodeApproval, model.WorkflowNodeAction, model.WorkflowNodeDelay,
		model.WorkflowNodeBranch, model.WorkflowNodeParallel, model.WorkflowNodeJoin,
		model.WorkflowNodeSubworkflow, model.WorkflowNodeEnd:
		return true
	default:
		return false
	}
}

func validWorkflowTrigger(value string) bool {
	switch value {
	case model.WorkflowTriggerManual, model.WorkflowTriggerEvent, model.WorkflowTriggerScheduled,
		model.WorkflowTriggerConnector, model.WorkflowTriggerInternal:
		return true
	default:
		return false
	}
}

func normalizeWorkflowTrigger(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func workflowTriggerNode(graph model.WorkflowGraph) *model.WorkflowNode {
	for i := range graph.Nodes {
		if graph.Nodes[i].Type == model.WorkflowNodeTrigger {
			return &graph.Nodes[i]
		}
	}
	return nil
}

func workflowTriggerMatches(graph model.WorkflowGraph, triggerType, triggerKey string) bool {
	node := workflowTriggerNode(graph)
	if node == nil {
		return false
	}
	source := normalizeWorkflowTrigger(fmt.Sprint(node.Config["source"]))
	if source == "" {
		source = model.WorkflowTriggerManual
	}
	if source != normalizeWorkflowTrigger(triggerType) {
		return false
	}
	key := strings.TrimSpace(fmt.Sprint(node.Config["key"]))
	if key == "" || key == "*" {
		return true
	}
	return key == strings.TrimSpace(triggerKey)
}

func workflowNodeByID(graph model.WorkflowGraph, nodeID string) *model.WorkflowNode {
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == nodeID {
			return &graph.Nodes[i]
		}
	}
	return nil
}

func workflowOutgoing(graph model.WorkflowGraph, nodeID, when string) []string {
	items := make([]string, 0)
	for _, edge := range graph.Edges {
		if edge.From != nodeID {
			continue
		}
		edgeWhen := strings.ToLower(strings.TrimSpace(edge.When))
		if when == "" {
			if edgeWhen == "" || edgeWhen == "always" {
				items = append(items, edge.To)
			}
			continue
		}
		if edgeWhen == "" || edgeWhen == "always" || edgeWhen == strings.ToLower(when) {
			items = append(items, edge.To)
		}
	}
	return workflowDedupeNodeIDs(items)
}

func workflowDedupeNodeIDs(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func workflowResolveMapping(mapping map[string]string, variables map[string]any) map[string]any {
	output := make(map[string]any)
	for key, expr := range mapping {
		output[key] = workflowResolveExpression(expr, variables)
	}
	return output
}

func workflowResolveExpression(expr string, variables map[string]any) any {
	expr = strings.TrimSpace(expr)
	expr = strings.TrimPrefix(expr, "$.")
	expr = strings.TrimPrefix(expr, "$")
	if expr == "" {
		return nil
	}
	parts := strings.Split(expr, ".")
	var current any = variables
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = m[part]
		if !ok {
			return nil
		}
	}
	return current
}

func workflowMaterialize(value any, variables map[string]any) any {
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(strings.TrimSpace(typed), "$") {
			return workflowResolveExpression(typed, variables)
		}
		return typed
	case map[string]any:
		output := make(map[string]any, len(typed))
		for key, child := range typed {
			output[key] = workflowMaterialize(child, variables)
		}
		return output
	case []any:
		output := make([]any, len(typed))
		for i, child := range typed {
			output[i] = workflowMaterialize(child, variables)
		}
		return output
	default:
		return value
	}
}

func workflowApplyOutputMapping(variables map[string]any, mapping map[string]string, output map[string]any) {
	for outputKey, variableName := range mapping {
		variableName = strings.TrimSpace(strings.TrimPrefix(variableName, "$"))
		if variableName == "" {
			continue
		}
		if value, ok := output[outputKey]; ok {
			variables[variableName] = value
		}
	}
}

func workflowEvaluateCondition(config map[string]any, variables, input map[string]any) bool {
	variable := strings.TrimSpace(fmt.Sprint(config["variable"]))
	var left any
	if variable != "" {
		left = workflowResolveExpression(variable, variables)
	} else if value, ok := input["value"]; ok {
		left = value
	}
	operator := strings.ToLower(strings.TrimSpace(fmt.Sprint(config["operator"])))
	if operator == "" {
		operator = "eq"
	}
	right := workflowMaterialize(config["value"], variables)
	switch operator {
	case "exists":
		return left != nil
	case "eq":
		return fmt.Sprint(left) == fmt.Sprint(right)
	case "neq":
		return fmt.Sprint(left) != fmt.Sprint(right)
	case "gt", "gte", "lt", "lte":
		l, lok := workflowNumber(left)
		r, rok := workflowNumber(right)
		if !lok || !rok {
			return false
		}
		switch operator {
		case "gt":
			return l > r
		case "gte":
			return l >= r
		case "lt":
			return l < r
		default:
			return l <= r
		}
	default:
		return false
	}
}

func workflowNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	case json.Number:
		value, err := typed.Float64()
		return value, err == nil
	case string:
		var number json.Number = json.Number(strings.TrimSpace(typed))
		value, err := number.Float64()
		return value, err == nil
	default:
		return 0, false
	}
}

func workflowConfigDuration(config map[string]any, key string, unit time.Duration) time.Duration {
	value, ok := workflowInt64(config[key])
	if !ok || value <= 0 {
		value = 1
	}
	return time.Duration(value) * unit
}

func cloneWorkflowVariables(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	raw, _ := json.Marshal(input)
	output := make(map[string]any)
	_ = json.Unmarshal(raw, &output)
	return output
}

func workflowExecutionTerminal(status string) bool {
	switch status {
	case model.WorkflowExecutionSucceeded, model.WorkflowExecutionFailed, model.WorkflowExecutionCancelled, model.WorkflowExecutionCompensated:
		return true
	default:
		return false
	}
}

func optionalWorkflowActor(actorUserID int64) *int64 {
	if actorUserID <= 0 {
		return nil
	}
	return &actorUserID
}

func workflowNodeSchemas(executors *WorkflowExecutorRegistry) []model.WorkflowNodeSchema {
	executorNames := executors.Names()
	sort.Strings(executorNames)
	return []model.WorkflowNodeSchema{
		{Type: model.WorkflowNodeTrigger, Description: "Starts a workflow from manual, event, scheduled, connector or internal triggers.", ConfigSchema: map[string]any{"source": []string{"manual", "event", "scheduled", "connector", "internal"}, "key": "string|*"}},
		{Type: model.WorkflowNodeCondition, Description: "Evaluates a typed variable condition.", ConfigSchema: map[string]any{"variable": "$path", "operator": []string{"eq", "neq", "gt", "gte", "lt", "lte", "exists"}, "value": "any"}},
		{Type: model.WorkflowNodeTransform, Description: "Maps and transforms workflow variables without arbitrary code.", ConfigSchema: map[string]any{"set": "object"}},
		{Type: model.WorkflowNodeApproval, Description: "Pauses execution until an organization admin approves or rejects.", ConfigSchema: map[string]any{}},
		{Type: model.WorkflowNodeAction, Description: "Runs an allowlisted executor.", ConfigSchema: map[string]any{"executor": executorNames, "params": "object"}},
		{Type: model.WorkflowNodeDelay, Description: "Creates a durable resumable delay.", ConfigSchema: map[string]any{"seconds": "1..2592000"}},
		{Type: model.WorkflowNodeBranch, Description: "Routes to true/false edges.", ConfigSchema: map[string]any{"variable": "$path", "operator": "comparator", "value": "any"}},
		{Type: model.WorkflowNodeParallel, Description: "Fans out to multiple DAG branches.", ConfigSchema: map[string]any{}},
		{Type: model.WorkflowNodeJoin, Description: "Joins predecessor branches.", ConfigSchema: map[string]any{"mode": []string{"all", "any"}}},
		{Type: model.WorkflowNodeSubworkflow, Description: "Runs another active workflow and resumes after it completes.", ConfigSchema: map[string]any{"workflow_id": "integer"}},
		{Type: model.WorkflowNodeEnd, Description: "Ends a workflow path.", ConfigSchema: map[string]any{}},
	}
}
