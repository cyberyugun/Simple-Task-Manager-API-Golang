package repository

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrOperationsPolicyNotFound    = errors.New("operations policy not found")
	ErrOperationalAlertNotFound    = errors.New("operational alert not found")
	ErrMaintenanceWindowNotFound   = errors.New("maintenance window not found")
	ErrOperationalIncidentNotFound = errors.New("operational incident not found")
)

type OperationsRepository interface {
	GetOperationsPolicy(organizationID int64) (model.OperationsPolicy, error)
	UpsertOperationsPolicy(policy model.OperationsPolicy) (model.OperationsPolicy, error)

	CreateCostAllocation(item model.CostAllocation) (model.CostAllocation, error)
	ListCostAllocations(organizationID int64, periodStart, periodEnd time.Time) ([]model.CostAllocation, error)

	UpsertOperationalAlert(item model.OperationalAlert) (model.OperationalAlert, error)
	ListOperationalAlerts(organizationID int64, status string, limit int) ([]model.OperationalAlert, error)
	AcknowledgeOperationalAlert(organizationID, alertID, actorUserID int64, at time.Time) (model.OperationalAlert, error)

	CreateMaintenanceWindow(item model.MaintenanceWindow) (model.MaintenanceWindow, error)
	ListMaintenanceWindows(organizationID int64, from, to time.Time) ([]model.MaintenanceWindow, error)
	UpdateMaintenanceWindowStatus(organizationID, windowID int64, status string, at time.Time) (model.MaintenanceWindow, error)

	CreateOperationalIncident(item model.OperationalIncident) (model.OperationalIncident, error)
	ListOperationalIncidents(organizationID int64, status string, from, to time.Time) ([]model.OperationalIncident, error)
	UpdateOperationalIncident(organizationID, incidentID int64, status, summary string, resolvedAt *time.Time, at time.Time) (model.OperationalIncident, error)
}

type InMemoryOperationsRepository struct {
	mu             sync.Mutex
	policies       map[int64]model.OperationsPolicy
	costs          map[int64]model.CostAllocation
	alerts         map[int64]model.OperationalAlert
	alertByKey     map[string]int64
	maintenance    map[int64]model.MaintenanceWindow
	incidents      map[int64]model.OperationalIncident
	nextCostID     int64
	nextAlertID    int64
	nextWindowID   int64
	nextIncidentID int64
}

func NewInMemoryOperationsRepository() *InMemoryOperationsRepository {
	return &InMemoryOperationsRepository{
		policies:       make(map[int64]model.OperationsPolicy),
		costs:          make(map[int64]model.CostAllocation),
		alerts:         make(map[int64]model.OperationalAlert),
		alertByKey:     make(map[string]int64),
		maintenance:    make(map[int64]model.MaintenanceWindow),
		incidents:      make(map[int64]model.OperationalIncident),
		nextCostID:     1,
		nextAlertID:    1,
		nextWindowID:   1,
		nextIncidentID: 1,
	}
}

func (r *InMemoryOperationsRepository) GetOperationsPolicy(organizationID int64) (model.OperationsPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.policies[organizationID]
	if !ok {
		return model.OperationsPolicy{}, ErrOperationsPolicyNotFound
	}
	return item, nil
}

func (r *InMemoryOperationsRepository) UpsertOperationsPolicy(policy model.OperationsPolicy) (model.OperationsPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.policies[policy.OrganizationID]; ok {
		policy.CreatedAt = existing.CreatedAt
	}
	r.policies[policy.OrganizationID] = policy
	return policy, nil
}

func (r *InMemoryOperationsRepository) CreateCostAllocation(item model.CostAllocation) (model.CostAllocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextCostID
	r.nextCostID++
	item.Metadata = cloneAnyMap(item.Metadata)
	r.costs[item.ID] = item
	return item, nil
}

func (r *InMemoryOperationsRepository) ListCostAllocations(organizationID int64, periodStart, periodEnd time.Time) ([]model.CostAllocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.CostAllocation, 0)
	for _, item := range r.costs {
		if item.OrganizationID == organizationID && item.PeriodStart.Before(periodEnd) && item.PeriodEnd.After(periodStart) {
			item.Metadata = cloneAnyMap(item.Metadata)
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].PeriodStart.Equal(items[j].PeriodStart) {
			return items[i].ID > items[j].ID
		}
		return items[i].PeriodStart.After(items[j].PeriodStart)
	})
	return items, nil
}

func (r *InMemoryOperationsRepository) UpsertOperationalAlert(item model.OperationalAlert) (model.OperationalAlert, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := fmt.Sprintf("%d|%s", item.OrganizationID, item.Fingerprint)
	if id, ok := r.alertByKey[key]; ok {
		existing := r.alerts[id]
		item.ID = id
		item.Status = existing.Status
		item.AcknowledgedAt = existing.AcknowledgedAt
		item.AcknowledgedByUserID = existing.AcknowledgedByUserID
		r.alerts[id] = item
		return item, nil
	}
	item.ID = r.nextAlertID
	r.nextAlertID++
	r.alerts[item.ID] = item
	r.alertByKey[key] = item.ID
	return item, nil
}

func (r *InMemoryOperationsRepository) ListOperationalAlerts(organizationID int64, status string, limit int) ([]model.OperationalAlert, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.OperationalAlert, 0)
	for _, item := range r.alerts {
		if item.OrganizationID == organizationID && (status == "" || item.Status == status) {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].DetectedAt.Equal(items[j].DetectedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].DetectedAt.After(items[j].DetectedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryOperationsRepository) AcknowledgeOperationalAlert(organizationID, alertID, actorUserID int64, at time.Time) (model.OperationalAlert, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.alerts[alertID]
	if !ok || item.OrganizationID != organizationID {
		return model.OperationalAlert{}, ErrOperationalAlertNotFound
	}
	item.Status = model.OperationalAlertAcknowledged
	item.AcknowledgedAt = &at
	item.AcknowledgedByUserID = &actorUserID
	item.UpdatedAt = at
	r.alerts[alertID] = item
	return item, nil
}

func (r *InMemoryOperationsRepository) CreateMaintenanceWindow(item model.MaintenanceWindow) (model.MaintenanceWindow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextWindowID
	r.nextWindowID++
	r.maintenance[item.ID] = item
	return item, nil
}

func (r *InMemoryOperationsRepository) ListMaintenanceWindows(organizationID int64, from, to time.Time) ([]model.MaintenanceWindow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.MaintenanceWindow, 0)
	for _, item := range r.maintenance {
		if item.OrganizationID == organizationID && item.StartsAt.Before(to) && item.EndsAt.After(from) {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].StartsAt.Before(items[j].StartsAt) })
	return items, nil
}

func (r *InMemoryOperationsRepository) UpdateMaintenanceWindowStatus(organizationID, windowID int64, status string, at time.Time) (model.MaintenanceWindow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.maintenance[windowID]
	if !ok || item.OrganizationID != organizationID {
		return model.MaintenanceWindow{}, ErrMaintenanceWindowNotFound
	}
	item.Status = status
	item.UpdatedAt = at
	r.maintenance[windowID] = item
	return item, nil
}

func (r *InMemoryOperationsRepository) CreateOperationalIncident(item model.OperationalIncident) (model.OperationalIncident, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextIncidentID
	r.nextIncidentID++
	r.incidents[item.ID] = item
	return item, nil
}

func (r *InMemoryOperationsRepository) ListOperationalIncidents(organizationID int64, status string, from, to time.Time) ([]model.OperationalIncident, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.OperationalIncident, 0)
	for _, item := range r.incidents {
		end := to
		if item.ResolvedAt != nil {
			end = *item.ResolvedAt
		}
		if item.OrganizationID == organizationID && (status == "" || item.Status == status) && item.StartedAt.Before(to) && end.After(from) {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].StartedAt.Equal(items[j].StartedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].StartedAt.After(items[j].StartedAt)
	})
	return items, nil
}

func (r *InMemoryOperationsRepository) UpdateOperationalIncident(organizationID, incidentID int64, status, summary string, resolvedAt *time.Time, at time.Time) (model.OperationalIncident, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.incidents[incidentID]
	if !ok || item.OrganizationID != organizationID {
		return model.OperationalIncident{}, ErrOperationalIncidentNotFound
	}
	item.Status = status
	item.Summary = summary
	item.ResolvedAt = resolvedAt
	item.UpdatedAt = at
	r.incidents[incidentID] = item
	return item, nil
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func normalizeOperationsFilter(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
