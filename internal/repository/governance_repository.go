package repository

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrGovernancePolicyNotFound = errors.New("governance policy not found")
	ErrLegalHoldNotFound        = errors.New("legal hold not found")
	ErrPrivacyRequestNotFound   = errors.New("privacy request not found")
)

type GovernanceRepository interface {
	UpsertPolicy(policy model.GovernancePolicy) (model.GovernancePolicy, error)
	GetPolicy(workspaceID int64) (model.GovernancePolicy, error)

	UpsertDataInventory(entry model.DataInventoryEntry) (model.DataInventoryEntry, error)
	ListDataInventory(workspaceID int64) ([]model.DataInventoryEntry, error)

	CreateLegalHold(hold model.LegalHold) (model.LegalHold, error)
	ListLegalHolds(workspaceID int64, activeOnly bool) ([]model.LegalHold, error)
	ReleaseLegalHold(workspaceID, holdID, actorUserID int64, now time.Time) error
	HasActiveLegalHold(workspaceID int64) (bool, error)

	CreatePrivacyRequest(request model.PrivacyRequest) (model.PrivacyRequest, error)
	ListPrivacyRequests(workspaceID int64) ([]model.PrivacyRequest, error)
	CompletePrivacyRequest(workspaceID, requestID, actorUserID int64, status, reason string, now time.Time) (model.PrivacyRequest, error)

	CreateComplianceEvidence(evidence model.ComplianceEvidence) (model.ComplianceEvidence, error)
	ListComplianceEvidence(workspaceID int64, limit int) ([]model.ComplianceEvidence, error)
}

type InMemoryGovernanceRepository struct {
	mu            sync.Mutex
	policies      map[int64]model.GovernancePolicy
	inventory     map[int64]map[string]model.DataInventoryEntry
	holds         map[int64]model.LegalHold
	privacy       map[int64]model.PrivacyRequest
	evidence      map[int64]model.ComplianceEvidence
	nextInventory int64
	nextHold      int64
	nextPrivacy   int64
	nextEvidence  int64
}

func NewInMemoryGovernanceRepository() *InMemoryGovernanceRepository {
	return &InMemoryGovernanceRepository{
		policies:      make(map[int64]model.GovernancePolicy),
		inventory:     make(map[int64]map[string]model.DataInventoryEntry),
		holds:         make(map[int64]model.LegalHold),
		privacy:       make(map[int64]model.PrivacyRequest),
		evidence:      make(map[int64]model.ComplianceEvidence),
		nextInventory: 1,
		nextHold:      1,
		nextPrivacy:   1,
		nextEvidence:  1,
	}
}

func (r *InMemoryGovernanceRepository) UpsertPolicy(policy model.GovernancePolicy) (model.GovernancePolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policies[policy.WorkspaceID] = policy
	return policy, nil
}

func (r *InMemoryGovernanceRepository) GetPolicy(workspaceID int64) (model.GovernancePolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.policies[workspaceID]
	if !ok {
		return model.GovernancePolicy{}, ErrGovernancePolicyNotFound
	}
	return item, nil
}

func inventoryKey(resourceType, fieldName string) string {
	return strings.ToLower(strings.TrimSpace(resourceType)) + ":" + strings.ToLower(strings.TrimSpace(fieldName))
}

func (r *InMemoryGovernanceRepository) UpsertDataInventory(entry model.DataInventoryEntry) (model.DataInventoryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inventory[entry.WorkspaceID] == nil {
		r.inventory[entry.WorkspaceID] = make(map[string]model.DataInventoryEntry)
	}
	key := inventoryKey(entry.ResourceType, entry.FieldName)
	if existing, ok := r.inventory[entry.WorkspaceID][key]; ok {
		entry.ID = existing.ID
	} else {
		entry.ID = r.nextInventory
		r.nextInventory++
	}
	r.inventory[entry.WorkspaceID][key] = entry
	return entry, nil
}

func (r *InMemoryGovernanceRepository) ListDataInventory(workspaceID int64) ([]model.DataInventoryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.DataInventoryEntry, 0)
	for _, item := range r.inventory[workspaceID] {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryGovernanceRepository) CreateLegalHold(hold model.LegalHold) (model.LegalHold, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	hold.ID = r.nextHold
	r.nextHold++
	r.holds[hold.ID] = hold
	return hold, nil
}

func (r *InMemoryGovernanceRepository) ListLegalHolds(workspaceID int64, activeOnly bool) ([]model.LegalHold, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.LegalHold, 0)
	for _, hold := range r.holds {
		if hold.WorkspaceID != workspaceID || (activeOnly && hold.ReleasedAt != nil) {
			continue
		}
		items = append(items, hold)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryGovernanceRepository) ReleaseLegalHold(workspaceID, holdID, actorUserID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	hold, ok := r.holds[holdID]
	if !ok || hold.WorkspaceID != workspaceID {
		return ErrLegalHoldNotFound
	}
	if hold.ReleasedAt == nil {
		hold.ReleasedAt = &now
		hold.ReleasedByUserID = &actorUserID
		r.holds[holdID] = hold
	}
	return nil
}

func (r *InMemoryGovernanceRepository) HasActiveLegalHold(workspaceID int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, hold := range r.holds {
		if hold.WorkspaceID == workspaceID && hold.ReleasedAt == nil {
			return true, nil
		}
	}
	return false, nil
}

func (r *InMemoryGovernanceRepository) CreatePrivacyRequest(request model.PrivacyRequest) (model.PrivacyRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	request.ID = r.nextPrivacy
	r.nextPrivacy++
	r.privacy[request.ID] = request
	return request, nil
}

func (r *InMemoryGovernanceRepository) ListPrivacyRequests(workspaceID int64) ([]model.PrivacyRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.PrivacyRequest, 0)
	for _, item := range r.privacy {
		if item.WorkspaceID == workspaceID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryGovernanceRepository) CompletePrivacyRequest(workspaceID, requestID, actorUserID int64, status, reason string, now time.Time) (model.PrivacyRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.privacy[requestID]
	if !ok || item.WorkspaceID != workspaceID || item.CompletedAt != nil {
		return model.PrivacyRequest{}, ErrPrivacyRequestNotFound
	}
	item.Status = status
	item.Reason = reason
	item.CompletedByUserID = &actorUserID
	item.CompletedAt = &now
	r.privacy[requestID] = item
	return item, nil
}

func (r *InMemoryGovernanceRepository) CreateComplianceEvidence(evidence model.ComplianceEvidence) (model.ComplianceEvidence, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	evidence.ID = r.nextEvidence
	r.nextEvidence++
	r.evidence[evidence.ID] = evidence
	return evidence, nil
}

func (r *InMemoryGovernanceRepository) ListComplianceEvidence(workspaceID int64, limit int) ([]model.ComplianceEvidence, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.ComplianceEvidence, 0)
	for _, item := range r.evidence {
		if item.WorkspaceID == workspaceID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}
