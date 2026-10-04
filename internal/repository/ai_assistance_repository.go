package repository

import (
	"errors"
	"sort"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrAIPolicyNotFound         = errors.New("AI policy not found")
	ErrAIRequestNotFound        = errors.New("AI request not found")
	ErrAIEvaluationCaseNotFound = errors.New("AI evaluation case not found")
)

type AIAssistanceRepository interface {
	GetPolicy(organizationID int64) (model.AIPolicy, error)
	UpsertPolicy(policy model.AIPolicy) (model.AIPolicy, error)

	CreateRequest(item model.AIRequest) (model.AIRequest, error)
	GetRequest(organizationID, requestID int64) (model.AIRequest, error)
	ListRequests(organizationID int64, limit int) ([]model.AIRequest, error)
	UpdateRequest(item model.AIRequest) (model.AIRequest, error)
	MonthlyUsage(organizationID int64, from, to time.Time) (model.AIUsageSummary, error)

	CreateEvaluationCase(item model.AIEvaluationCase) (model.AIEvaluationCase, error)
	GetEvaluationCase(organizationID, caseID int64) (model.AIEvaluationCase, error)
	ListEvaluationCases(organizationID int64) ([]model.AIEvaluationCase, error)
	CreateEvaluationRun(item model.AIEvaluationRun) (model.AIEvaluationRun, error)
	ListEvaluationRuns(organizationID int64, limit int) ([]model.AIEvaluationRun, error)
}

type InMemoryAIAssistanceRepository struct {
	mu         sync.Mutex
	policies   map[int64]model.AIPolicy
	requests   map[int64]model.AIRequest
	cases      map[int64]model.AIEvaluationCase
	runs       map[int64]model.AIEvaluationRun
	nextReqID  int64
	nextCaseID int64
	nextRunID  int64
}

func NewInMemoryAIAssistanceRepository() *InMemoryAIAssistanceRepository {
	return &InMemoryAIAssistanceRepository{
		policies: make(map[int64]model.AIPolicy),
		requests: make(map[int64]model.AIRequest),
		cases: make(map[int64]model.AIEvaluationCase),
		runs: make(map[int64]model.AIEvaluationRun),
		nextReqID: 1,
		nextCaseID: 1,
		nextRunID: 1,
	}
}

func (r *InMemoryAIAssistanceRepository) GetPolicy(organizationID int64) (model.AIPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.policies[organizationID]
	if !ok {
		return model.AIPolicy{}, ErrAIPolicyNotFound
	}
	item.AllowedClassifications = append([]string(nil), item.AllowedClassifications...)
	return item, nil
}

func (r *InMemoryAIAssistanceRepository) UpsertPolicy(item model.AIPolicy) (model.AIPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.policies[item.OrganizationID]; ok {
		item.CreatedAt = existing.CreatedAt
	}
	item.AllowedClassifications = append([]string(nil), item.AllowedClassifications...)
	r.policies[item.OrganizationID] = item
	return item, nil
}

func (r *InMemoryAIAssistanceRepository) CreateRequest(item model.AIRequest) (model.AIRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextReqID
	r.nextReqID++
	item.PromptMetadata = cloneAIMap(item.PromptMetadata)
	item.StructuredResult = cloneAIMap(item.StructuredResult)
	item.ProposedAction = cloneAIMap(item.ProposedAction)
	r.requests[item.ID] = item
	return cloneAIRequest(item), nil
}

func (r *InMemoryAIAssistanceRepository) GetRequest(organizationID, requestID int64) (model.AIRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.requests[requestID]
	if !ok || item.OrganizationID != organizationID {
		return model.AIRequest{}, ErrAIRequestNotFound
	}
	return cloneAIRequest(item), nil
}

func (r *InMemoryAIAssistanceRepository) ListRequests(organizationID int64, limit int) ([]model.AIRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.AIRequest, 0)
	for _, item := range r.requests {
		if item.OrganizationID == organizationID {
			items = append(items, cloneAIRequest(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryAIAssistanceRepository) UpdateRequest(item model.AIRequest) (model.AIRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.requests[item.ID]
	if !ok || existing.OrganizationID != item.OrganizationID {
		return model.AIRequest{}, ErrAIRequestNotFound
	}
	item.PromptMetadata = cloneAIMap(item.PromptMetadata)
	item.StructuredResult = cloneAIMap(item.StructuredResult)
	item.ProposedAction = cloneAIMap(item.ProposedAction)
	r.requests[item.ID] = item
	return cloneAIRequest(item), nil
}

func (r *InMemoryAIAssistanceRepository) MonthlyUsage(organizationID int64, from, to time.Time) (model.AIUsageSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	summary := model.AIUsageSummary{OrganizationID: organizationID, Month: from.Format("2006-01")}
	for _, item := range r.requests {
		if item.OrganizationID != organizationID || item.CreatedAt.Before(from) || !item.CreatedAt.Before(to) {
			continue
		}
		if item.Status == model.AIRequestBlocked || item.Status == model.AIRequestFailed {
			continue
		}
		summary.RequestCount++
		summary.SpentCents += item.ActualCostCents
		summary.InputUnits += item.InputUnits
		summary.OutputUnits += item.OutputUnits
	}
	return summary, nil
}

func (r *InMemoryAIAssistanceRepository) CreateEvaluationCase(item model.AIEvaluationCase) (model.AIEvaluationCase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextCaseID
	r.nextCaseID++
	item.ExpectedKeywords = append([]string(nil), item.ExpectedKeywords...)
	item.Metadata = cloneAIMap(item.Metadata)
	r.cases[item.ID] = item
	return cloneAIEvaluationCase(item), nil
}

func (r *InMemoryAIAssistanceRepository) GetEvaluationCase(organizationID, caseID int64) (model.AIEvaluationCase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.cases[caseID]
	if !ok || item.OrganizationID != organizationID {
		return model.AIEvaluationCase{}, ErrAIEvaluationCaseNotFound
	}
	return cloneAIEvaluationCase(item), nil
}

func (r *InMemoryAIAssistanceRepository) ListEvaluationCases(organizationID int64) ([]model.AIEvaluationCase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.AIEvaluationCase, 0)
	for _, item := range r.cases {
		if item.OrganizationID == organizationID {
			items = append(items, cloneAIEvaluationCase(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryAIAssistanceRepository) CreateEvaluationRun(item model.AIEvaluationRun) (model.AIEvaluationRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextRunID
	r.nextRunID++
	item.Metadata = cloneAIMap(item.Metadata)
	r.runs[item.ID] = item
	return cloneAIEvaluationRun(item), nil
}

func (r *InMemoryAIAssistanceRepository) ListEvaluationRuns(organizationID int64, limit int) ([]model.AIEvaluationRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.AIEvaluationRun, 0)
	for _, item := range r.runs {
		if item.OrganizationID == organizationID {
			items = append(items, cloneAIEvaluationRun(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func cloneAIMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneAIRequest(item model.AIRequest) model.AIRequest {
	item.PromptMetadata = cloneAIMap(item.PromptMetadata)
	item.StructuredResult = cloneAIMap(item.StructuredResult)
	item.ProposedAction = cloneAIMap(item.ProposedAction)
	return item
}

func cloneAIEvaluationCase(item model.AIEvaluationCase) model.AIEvaluationCase {
	item.ExpectedKeywords = append([]string(nil), item.ExpectedKeywords...)
	item.Metadata = cloneAIMap(item.Metadata)
	return item
}

func cloneAIEvaluationRun(item model.AIEvaluationRun) model.AIEvaluationRun {
	item.Metadata = cloneAIMap(item.Metadata)
	return item
}
