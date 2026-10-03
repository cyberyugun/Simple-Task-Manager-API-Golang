package repository

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrAutomationPolicyNotFound    = errors.New("automation policy not found")
	ErrAutomationExecutionNotFound = errors.New("automation execution not found")
	ErrAutomationExecutionExists   = errors.New("automation execution already exists")
)

type AutomationRepository interface {
	CreateAutomationPolicy(policy model.AutomationPolicy) (model.AutomationPolicy, error)
	UpdateAutomationPolicy(policy model.AutomationPolicy) (model.AutomationPolicy, error)
	GetAutomationPolicy(organizationID, policyID int64) (model.AutomationPolicy, error)
	ListAutomationPolicies(organizationID int64) ([]model.AutomationPolicy, error)
	ListEnabledAutomationPolicies() ([]model.AutomationPolicy, error)

	CreateAutomationExecution(execution model.AutomationExecution) (model.AutomationExecution, error)
	GetAutomationExecution(organizationID, executionID int64) (model.AutomationExecution, error)
	ListAutomationExecutions(organizationID int64, limit int) ([]model.AutomationExecution, error)
	UpdateAutomationExecution(execution model.AutomationExecution) (model.AutomationExecution, error)
	LatestAutomationExecution(policyID int64) (model.AutomationExecution, error)
}

type InMemoryAutomationRepository struct {
	mu            sync.Mutex
	policies      map[int64]model.AutomationPolicy
	executions    map[int64]model.AutomationExecution
	executionKeys map[string]int64
	nextPolicyID  int64
	nextExecID    int64
}

func NewInMemoryAutomationRepository() *InMemoryAutomationRepository {
	return &InMemoryAutomationRepository{
		policies:      make(map[int64]model.AutomationPolicy),
		executions:    make(map[int64]model.AutomationExecution),
		executionKeys: make(map[string]int64),
		nextPolicyID:  1,
		nextExecID:    1,
	}
}

func (r *InMemoryAutomationRepository) CreateAutomationPolicy(policy model.AutomationPolicy) (model.AutomationPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	policy.ID = r.nextPolicyID
	r.nextPolicyID++
	policy.ActionConfig = cloneAutomationMap(policy.ActionConfig)
	r.policies[policy.ID] = policy
	return cloneAutomationPolicy(policy), nil
}

func (r *InMemoryAutomationRepository) UpdateAutomationPolicy(policy model.AutomationPolicy) (model.AutomationPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.policies[policy.ID]
	if !ok || existing.OrganizationID != policy.OrganizationID {
		return model.AutomationPolicy{}, ErrAutomationPolicyNotFound
	}
	policy.CreatedAt = existing.CreatedAt
	policy.CreatedByUserID = existing.CreatedByUserID
	policy.ActionConfig = cloneAutomationMap(policy.ActionConfig)
	r.policies[policy.ID] = policy
	return cloneAutomationPolicy(policy), nil
}

func (r *InMemoryAutomationRepository) GetAutomationPolicy(organizationID, policyID int64) (model.AutomationPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.policies[policyID]
	if !ok || item.OrganizationID != organizationID {
		return model.AutomationPolicy{}, ErrAutomationPolicyNotFound
	}
	return cloneAutomationPolicy(item), nil
}

func (r *InMemoryAutomationRepository) ListAutomationPolicies(organizationID int64) ([]model.AutomationPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.AutomationPolicy, 0)
	for _, item := range r.policies {
		if item.OrganizationID == organizationID {
			items = append(items, cloneAutomationPolicy(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryAutomationRepository) ListEnabledAutomationPolicies() ([]model.AutomationPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.AutomationPolicy, 0)
	for _, item := range r.policies {
		if item.Enabled {
			items = append(items, cloneAutomationPolicy(item))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].OrganizationID == items[j].OrganizationID {
			return items[i].ID < items[j].ID
		}
		return items[i].OrganizationID < items[j].OrganizationID
	})
	return items, nil
}

func (r *InMemoryAutomationRepository) CreateAutomationExecution(execution model.AutomationExecution) (model.AutomationExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := fmt.Sprintf("%d|%s", execution.OrganizationID, execution.DedupeKey)
	if _, exists := r.executionKeys[key]; exists {
		return model.AutomationExecution{}, ErrAutomationExecutionExists
	}
	execution.ID = r.nextExecID
	r.nextExecID++
	execution.TriggerSnapshot = cloneAutomationMap(execution.TriggerSnapshot)
	execution.ActionResult = cloneAutomationMap(execution.ActionResult)
	r.executions[execution.ID] = execution
	r.executionKeys[key] = execution.ID
	return cloneAutomationExecution(execution), nil
}

func (r *InMemoryAutomationRepository) GetAutomationExecution(organizationID, executionID int64) (model.AutomationExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.executions[executionID]
	if !ok || item.OrganizationID != organizationID {
		return model.AutomationExecution{}, ErrAutomationExecutionNotFound
	}
	return cloneAutomationExecution(item), nil
}

func (r *InMemoryAutomationRepository) ListAutomationExecutions(organizationID int64, limit int) ([]model.AutomationExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.AutomationExecution, 0)
	for _, item := range r.executions {
		if item.OrganizationID == organizationID {
			items = append(items, cloneAutomationExecution(item))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].RequestedAt.Equal(items[j].RequestedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].RequestedAt.After(items[j].RequestedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryAutomationRepository) UpdateAutomationExecution(execution model.AutomationExecution) (model.AutomationExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.executions[execution.ID]
	if !ok || existing.OrganizationID != execution.OrganizationID {
		return model.AutomationExecution{}, ErrAutomationExecutionNotFound
	}
	execution.CreatedAt = existing.CreatedAt
	execution.DedupeKey = existing.DedupeKey
	execution.TriggerSnapshot = cloneAutomationMap(execution.TriggerSnapshot)
	execution.ActionResult = cloneAutomationMap(execution.ActionResult)
	r.executions[execution.ID] = execution
	return cloneAutomationExecution(execution), nil
}

func (r *InMemoryAutomationRepository) LatestAutomationExecution(policyID int64) (model.AutomationExecution, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var latest model.AutomationExecution
	found := false
	for _, item := range r.executions {
		if item.PolicyID != policyID {
			continue
		}
		if !found || item.RequestedAt.After(latest.RequestedAt) {
			latest = item
			found = true
		}
	}
	if !found {
		return model.AutomationExecution{}, ErrAutomationExecutionNotFound
	}
	return cloneAutomationExecution(latest), nil
}

func cloneAutomationPolicy(item model.AutomationPolicy) model.AutomationPolicy {
	clone := item
	clone.ActionConfig = cloneAutomationMap(item.ActionConfig)
	return clone
}

func cloneAutomationExecution(item model.AutomationExecution) model.AutomationExecution {
	clone := item
	clone.TriggerSnapshot = cloneAutomationMap(item.TriggerSnapshot)
	clone.ActionResult = cloneAutomationMap(item.ActionResult)
	return clone
}

func cloneAutomationMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func automationPeriodKey(at time.Time) string {
	return at.UTC().Format("2006-01-02T15")
}
