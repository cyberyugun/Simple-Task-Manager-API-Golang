package repository

import (
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrWorkflowNotFound          = errors.New("workflow not found")
	ErrWorkflowVersionNotFound   = errors.New("workflow version not found")
	ErrWorkflowExecutionNotFound = errors.New("workflow execution not found")
	ErrWorkflowNodeRunNotFound   = errors.New("workflow node execution not found")
	ErrWorkflowApprovalNotFound  = errors.New("workflow approval not found")
)

type WorkflowRepository interface {
	CreateWorkflow(model.WorkflowDefinition) (model.WorkflowDefinition, error)
	UpdateWorkflow(model.WorkflowDefinition) (model.WorkflowDefinition, error)
	GetWorkflow(organizationID, workflowID int64) (model.WorkflowDefinition, error)
	ListWorkflows(organizationID int64) ([]model.WorkflowDefinition, error)

	CreateWorkflowVersion(model.WorkflowVersion) (model.WorkflowVersion, error)
	UpdateWorkflowVersion(model.WorkflowVersion) (model.WorkflowVersion, error)
	GetWorkflowVersion(organizationID, versionID int64) (model.WorkflowVersion, error)
	ListWorkflowVersions(organizationID, workflowID int64) ([]model.WorkflowVersion, error)
	NextWorkflowVersionNumber(workflowID int64) (int, error)

	CreateWorkflowExecution(model.WorkflowExecution) (model.WorkflowExecution, error)
	UpdateWorkflowExecution(model.WorkflowExecution) (model.WorkflowExecution, error)
	GetWorkflowExecution(organizationID, executionID int64) (model.WorkflowExecution, error)
	ListWorkflowExecutions(organizationID int64, limit int) ([]model.WorkflowExecution, error)
	ListDueWorkflowExecutions(now time.Time, limit int) ([]model.WorkflowExecution, error)

	CreateWorkflowNodeExecution(model.WorkflowNodeExecution) (model.WorkflowNodeExecution, error)
	UpdateWorkflowNodeExecution(model.WorkflowNodeExecution) (model.WorkflowNodeExecution, error)
	ListWorkflowNodeExecutions(executionID int64) ([]model.WorkflowNodeExecution, error)
	LatestWorkflowNodeExecution(executionID int64, nodeID string) (model.WorkflowNodeExecution, error)

	CreateWorkflowApproval(model.WorkflowApproval) (model.WorkflowApproval, error)
	UpdateWorkflowApproval(model.WorkflowApproval) (model.WorkflowApproval, error)
	GetWorkflowApproval(organizationID, approvalID int64) (model.WorkflowApproval, error)
	FindWorkflowApproval(executionID int64, nodeID string) (model.WorkflowApproval, error)
	ListWorkflowApprovals(executionID int64) ([]model.WorkflowApproval, error)

	CreateWorkflowCheckpoint(model.WorkflowCheckpoint) (model.WorkflowCheckpoint, error)
	ListWorkflowCheckpoints(executionID int64) ([]model.WorkflowCheckpoint, error)
}

type InMemoryWorkflowRepository struct {
	mu                                                                                              sync.Mutex
	workflows                                                                                       map[int64]model.WorkflowDefinition
	versions                                                                                        map[int64]model.WorkflowVersion
	executions                                                                                      map[int64]model.WorkflowExecution
	nodeRuns                                                                                        map[int64]model.WorkflowNodeExecution
	approvals                                                                                       map[int64]model.WorkflowApproval
	checkpoints                                                                                     map[int64]model.WorkflowCheckpoint
	nextWorkflowID, nextVersionID, nextExecutionID, nextNodeRunID, nextApprovalID, nextCheckpointID int64
}

func NewInMemoryWorkflowRepository() *InMemoryWorkflowRepository {
	return &InMemoryWorkflowRepository{
		workflows: map[int64]model.WorkflowDefinition{}, versions: map[int64]model.WorkflowVersion{},
		executions: map[int64]model.WorkflowExecution{}, nodeRuns: map[int64]model.WorkflowNodeExecution{},
		approvals: map[int64]model.WorkflowApproval{}, checkpoints: map[int64]model.WorkflowCheckpoint{},
		nextWorkflowID: 1, nextVersionID: 1, nextExecutionID: 1, nextNodeRunID: 1, nextApprovalID: 1, nextCheckpointID: 1,
	}
}

func (r *InMemoryWorkflowRepository) CreateWorkflow(item model.WorkflowDefinition) (model.WorkflowDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextWorkflowID
	r.nextWorkflowID++
	r.workflows[item.ID] = item
	return item, nil
}
func (r *InMemoryWorkflowRepository) UpdateWorkflow(item model.WorkflowDefinition) (model.WorkflowDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.workflows[item.ID]
	if !ok || old.OrganizationID != item.OrganizationID {
		return model.WorkflowDefinition{}, ErrWorkflowNotFound
	}
	item.CreatedAt, item.CreatedByUserID = old.CreatedAt, old.CreatedByUserID
	r.workflows[item.ID] = item
	return item, nil
}
func (r *InMemoryWorkflowRepository) GetWorkflow(orgID, id int64) (model.WorkflowDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.workflows[id]
	if !ok || item.OrganizationID != orgID {
		return model.WorkflowDefinition{}, ErrWorkflowNotFound
	}
	return item, nil
}
func (r *InMemoryWorkflowRepository) ListWorkflows(orgID int64) ([]model.WorkflowDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.WorkflowDefinition{}
	for _, item := range r.workflows {
		if item.OrganizationID == orgID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryWorkflowRepository) CreateWorkflowVersion(item model.WorkflowVersion) (model.WorkflowVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.workflows[item.WorkflowID]
	if !ok || w.OrganizationID != item.OrganizationID {
		return model.WorkflowVersion{}, ErrWorkflowNotFound
	}
	item.ID = r.nextVersionID
	r.nextVersionID++
	item.Graph = cloneWorkflowGraph(item.Graph)
	r.versions[item.ID] = item
	return cloneWorkflowVersion(item), nil
}
func (r *InMemoryWorkflowRepository) UpdateWorkflowVersion(item model.WorkflowVersion) (model.WorkflowVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.versions[item.ID]
	if !ok || old.OrganizationID != item.OrganizationID || old.WorkflowID != item.WorkflowID {
		return model.WorkflowVersion{}, ErrWorkflowVersionNotFound
	}
	item.CreatedAt, item.CreatedByUserID = old.CreatedAt, old.CreatedByUserID
	item.Graph = cloneWorkflowGraph(item.Graph)
	r.versions[item.ID] = item
	return cloneWorkflowVersion(item), nil
}
func (r *InMemoryWorkflowRepository) GetWorkflowVersion(orgID, id int64) (model.WorkflowVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.versions[id]
	if !ok || item.OrganizationID != orgID {
		return model.WorkflowVersion{}, ErrWorkflowVersionNotFound
	}
	return cloneWorkflowVersion(item), nil
}
func (r *InMemoryWorkflowRepository) ListWorkflowVersions(orgID, workflowID int64) ([]model.WorkflowVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.WorkflowVersion{}
	for _, item := range r.versions {
		if item.OrganizationID == orgID && item.WorkflowID == workflowID {
			items = append(items, cloneWorkflowVersion(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Version < items[j].Version })
	return items, nil
}
func (r *InMemoryWorkflowRepository) NextWorkflowVersionNumber(workflowID int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.workflows[workflowID]; !ok {
		return 0, ErrWorkflowNotFound
	}
	next := 1
	for _, item := range r.versions {
		if item.WorkflowID == workflowID && item.Version >= next {
			next = item.Version + 1
		}
	}
	return next, nil
}

func (r *InMemoryWorkflowRepository) CreateWorkflowExecution(item model.WorkflowExecution) (model.WorkflowExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextExecutionID
	r.nextExecutionID++
	item = cloneWorkflowExecution(item)
	r.executions[item.ID] = item
	return cloneWorkflowExecution(item), nil
}
func (r *InMemoryWorkflowRepository) UpdateWorkflowExecution(item model.WorkflowExecution) (model.WorkflowExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.executions[item.ID]
	if !ok || old.OrganizationID != item.OrganizationID {
		return model.WorkflowExecution{}, ErrWorkflowExecutionNotFound
	}
	item.CreatedAt = old.CreatedAt
	item = cloneWorkflowExecution(item)
	r.executions[item.ID] = item
	return cloneWorkflowExecution(item), nil
}
func (r *InMemoryWorkflowRepository) GetWorkflowExecution(orgID, id int64) (model.WorkflowExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.executions[id]
	if !ok || item.OrganizationID != orgID {
		return model.WorkflowExecution{}, ErrWorkflowExecutionNotFound
	}
	return cloneWorkflowExecution(item), nil
}
func (r *InMemoryWorkflowRepository) ListWorkflowExecutions(orgID int64, limit int) ([]model.WorkflowExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.WorkflowExecution{}
	for _, item := range r.executions {
		if item.OrganizationID == orgID {
			items = append(items, cloneWorkflowExecution(item))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].StartedAt.Equal(items[j].StartedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].StartedAt.After(items[j].StartedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
func (r *InMemoryWorkflowRepository) ListDueWorkflowExecutions(now time.Time, limit int) ([]model.WorkflowExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.WorkflowExecution{}
	for _, item := range r.executions {
		if item.Status == model.WorkflowExecutionWaitingDelay && item.ResumeAt != nil && !item.ResumeAt.After(now) {
			items = append(items, cloneWorkflowExecution(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ResumeAt.Before(*items[j].ResumeAt) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryWorkflowRepository) CreateWorkflowNodeExecution(item model.WorkflowNodeExecution) (model.WorkflowNodeExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextNodeRunID
	r.nextNodeRunID++
	item = cloneWorkflowNodeRun(item)
	r.nodeRuns[item.ID] = item
	return cloneWorkflowNodeRun(item), nil
}
func (r *InMemoryWorkflowRepository) UpdateWorkflowNodeExecution(item model.WorkflowNodeExecution) (model.WorkflowNodeExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.nodeRuns[item.ID]
	if !ok || old.ExecutionID != item.ExecutionID {
		return model.WorkflowNodeExecution{}, ErrWorkflowNodeRunNotFound
	}
	item.CreatedAt = old.CreatedAt
	item = cloneWorkflowNodeRun(item)
	r.nodeRuns[item.ID] = item
	return cloneWorkflowNodeRun(item), nil
}
func (r *InMemoryWorkflowRepository) ListWorkflowNodeExecutions(executionID int64) ([]model.WorkflowNodeExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.WorkflowNodeExecution{}
	for _, item := range r.nodeRuns {
		if item.ExecutionID == executionID {
			items = append(items, cloneWorkflowNodeRun(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}
func (r *InMemoryWorkflowRepository) LatestWorkflowNodeExecution(executionID int64, nodeID string) (model.WorkflowNodeExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found model.WorkflowNodeExecution
	ok := false
	for _, item := range r.nodeRuns {
		if item.ExecutionID == executionID && item.NodeID == nodeID && (!ok || item.Attempt > found.Attempt || (item.Attempt == found.Attempt && item.ID > found.ID)) {
			found, ok = item, true
		}
	}
	if !ok {
		return model.WorkflowNodeExecution{}, ErrWorkflowNodeRunNotFound
	}
	return cloneWorkflowNodeRun(found), nil
}

func (r *InMemoryWorkflowRepository) CreateWorkflowApproval(item model.WorkflowApproval) (model.WorkflowApproval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextApprovalID
	r.nextApprovalID++
	r.approvals[item.ID] = item
	return item, nil
}
func (r *InMemoryWorkflowRepository) UpdateWorkflowApproval(item model.WorkflowApproval) (model.WorkflowApproval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.approvals[item.ID]
	if !ok || old.OrganizationID != item.OrganizationID {
		return model.WorkflowApproval{}, ErrWorkflowApprovalNotFound
	}
	item.CreatedAt = old.CreatedAt
	r.approvals[item.ID] = item
	return item, nil
}
func (r *InMemoryWorkflowRepository) GetWorkflowApproval(orgID, id int64) (model.WorkflowApproval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.approvals[id]
	if !ok || item.OrganizationID != orgID {
		return model.WorkflowApproval{}, ErrWorkflowApprovalNotFound
	}
	return item, nil
}
func (r *InMemoryWorkflowRepository) FindWorkflowApproval(executionID int64, nodeID string) (model.WorkflowApproval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found model.WorkflowApproval
	ok := false
	for _, item := range r.approvals {
		if item.ExecutionID == executionID && item.NodeID == nodeID && (!ok || item.ID > found.ID) {
			found, ok = item, true
		}
	}
	if !ok {
		return model.WorkflowApproval{}, ErrWorkflowApprovalNotFound
	}
	return found, nil
}
func (r *InMemoryWorkflowRepository) ListWorkflowApprovals(executionID int64) ([]model.WorkflowApproval, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.WorkflowApproval{}
	for _, item := range r.approvals {
		if item.ExecutionID == executionID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryWorkflowRepository) CreateWorkflowCheckpoint(item model.WorkflowCheckpoint) (model.WorkflowCheckpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextCheckpointID
	r.nextCheckpointID++
	if item.Sequence <= 0 {
		maxSeq := 0
		for _, existing := range r.checkpoints {
			if existing.ExecutionID == item.ExecutionID && existing.Sequence > maxSeq {
				maxSeq = existing.Sequence
			}
		}
		item.Sequence = maxSeq + 1
	}
	item = cloneWorkflowCheckpoint(item)
	r.checkpoints[item.ID] = item
	return cloneWorkflowCheckpoint(item), nil
}
func (r *InMemoryWorkflowRepository) ListWorkflowCheckpoints(executionID int64) ([]model.WorkflowCheckpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.WorkflowCheckpoint{}
	for _, item := range r.checkpoints {
		if item.ExecutionID == executionID {
			items = append(items, cloneWorkflowCheckpoint(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Sequence < items[j].Sequence })
	return items, nil
}

func cloneWorkflowVersion(v model.WorkflowVersion) model.WorkflowVersion {
	v.Graph = cloneWorkflowGraph(v.Graph)
	return v
}
func cloneWorkflowExecution(v model.WorkflowExecution) model.WorkflowExecution {
	v.TriggerPayload = cloneWorkflowMap(v.TriggerPayload)
	v.Variables = cloneWorkflowMap(v.Variables)
	v.WorkflowSnapshot = cloneWorkflowGraph(v.WorkflowSnapshot)
	v.NextNodeIDs = append([]string(nil), v.NextNodeIDs...)
	return v
}
func cloneWorkflowNodeRun(v model.WorkflowNodeExecution) model.WorkflowNodeExecution {
	v.Input = cloneWorkflowMap(v.Input)
	v.Output = cloneWorkflowMap(v.Output)
	return v
}
func cloneWorkflowCheckpoint(v model.WorkflowCheckpoint) model.WorkflowCheckpoint {
	v.Variables = cloneWorkflowMap(v.Variables)
	v.NextNodeIDs = append([]string(nil), v.NextNodeIDs...)
	return v
}
func cloneWorkflowGraph(v model.WorkflowGraph) model.WorkflowGraph {
	raw, _ := json.Marshal(v)
	var out model.WorkflowGraph
	_ = json.Unmarshal(raw, &out)
	if out.Nodes == nil {
		out.Nodes = []model.WorkflowNode{}
	}
	if out.Edges == nil {
		out.Edges = []model.WorkflowEdge{}
	}
	return out
}
func cloneWorkflowMap(v map[string]any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	raw, _ := json.Marshal(v)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}
