package repository

import (
	"errors"
	"sort"
	"strings"
	"sync"

	"go-simple-task-api/internal/model"
)

var (
	ErrRegionPolicyNotFound        = errors.New("organization region policy not found")
	ErrRegionalPlacementNotFound   = errors.New("regional placement not found")
	ErrRegionMigrationNotFound     = errors.New("region migration not found")
	ErrCrossRegionTransferNotFound = errors.New("cross-region transfer not found")
	ErrFailoverExerciseNotFound    = errors.New("failover exercise not found")
)

type GlobalRegionRepository interface {
	GetPolicy(organizationID int64) (model.OrganizationRegionPolicy, error)
	UpsertPolicy(item model.OrganizationRegionPolicy) (model.OrganizationRegionPolicy, error)

	UpsertPlacement(item model.RegionalPlacement) (model.RegionalPlacement, error)
	ListPlacements(organizationID int64) ([]model.RegionalPlacement, error)

	CreateMigration(item model.RegionMigration) (model.RegionMigration, error)
	GetMigration(organizationID, migrationID int64) (model.RegionMigration, error)
	ListMigrations(organizationID int64, limit int) ([]model.RegionMigration, error)
	UpdateMigration(item model.RegionMigration) (model.RegionMigration, error)

	CreateTransfer(item model.CrossRegionTransfer) (model.CrossRegionTransfer, error)
	GetTransfer(organizationID, transferID int64) (model.CrossRegionTransfer, error)
	ListTransfers(organizationID int64, limit int) ([]model.CrossRegionTransfer, error)
	UpdateTransfer(item model.CrossRegionTransfer) (model.CrossRegionTransfer, error)

	CreateFailoverExercise(item model.FailoverExercise) (model.FailoverExercise, error)
	GetFailoverExercise(organizationID, exerciseID int64) (model.FailoverExercise, error)
	ListFailoverExercises(organizationID int64, limit int) ([]model.FailoverExercise, error)
	UpdateFailoverExercise(item model.FailoverExercise) (model.FailoverExercise, error)
}

type InMemoryGlobalRegionRepository struct {
	mu            sync.Mutex
	policies      map[int64]model.OrganizationRegionPolicy
	placements    map[string]model.RegionalPlacement
	migrations    map[int64]model.RegionMigration
	transfers     map[int64]model.CrossRegionTransfer
	exercises     map[int64]model.FailoverExercise
	nextPlacement int64
	nextMigration int64
	nextTransfer  int64
	nextExercise  int64
}

func NewInMemoryGlobalRegionRepository() *InMemoryGlobalRegionRepository {
	return &InMemoryGlobalRegionRepository{
		policies:      make(map[int64]model.OrganizationRegionPolicy),
		placements:    make(map[string]model.RegionalPlacement),
		migrations:    make(map[int64]model.RegionMigration),
		transfers:     make(map[int64]model.CrossRegionTransfer),
		exercises:     make(map[int64]model.FailoverExercise),
		nextPlacement: 1, nextMigration: 1, nextTransfer: 1, nextExercise: 1,
	}
}

func (r *InMemoryGlobalRegionRepository) GetPolicy(organizationID int64) (model.OrganizationRegionPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.policies[organizationID]
	if !ok {
		return model.OrganizationRegionPolicy{}, ErrRegionPolicyNotFound
	}
	return cloneRegionPolicy(item), nil
}

func (r *InMemoryGlobalRegionRepository) UpsertPolicy(item model.OrganizationRegionPolicy) (model.OrganizationRegionPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.policies[item.OrganizationID]; ok {
		item.CreatedAt = old.CreatedAt
	}
	item = cloneRegionPolicy(item)
	r.policies[item.OrganizationID] = item
	return cloneRegionPolicy(item), nil
}

func placementKey(orgID int64, resourceType, resourceID string) string {
	return strings.Join([]string{fmtInt64(orgID), strings.ToLower(resourceType), resourceID}, ":")
}

func fmtInt64(v int64) string {
	if v == 0 {
		return "0"
	}
	const digits = "0123456789"
	var b [20]byte
	i := len(b)
	n := v
	for n > 0 {
		i--
		b[i] = digits[n%10]
		n /= 10
	}
	return string(b[i:])
}

func (r *InMemoryGlobalRegionRepository) UpsertPlacement(item model.RegionalPlacement) (model.RegionalPlacement, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := placementKey(item.OrganizationID, item.ResourceType, item.ResourceID)
	if old, ok := r.placements[key]; ok {
		item.ID = old.ID
		item.CreatedAt = old.CreatedAt
	} else {
		item.ID = r.nextPlacement
		r.nextPlacement++
	}
	item.ReplicaRegions = append([]string(nil), item.ReplicaRegions...)
	r.placements[key] = item
	return clonePlacement(item), nil
}

func (r *InMemoryGlobalRegionRepository) ListPlacements(organizationID int64) ([]model.RegionalPlacement, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.RegionalPlacement, 0)
	for _, item := range r.placements {
		if item.OrganizationID == organizationID {
			items = append(items, clonePlacement(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryGlobalRegionRepository) CreateMigration(item model.RegionMigration) (model.RegionMigration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextMigration
	r.nextMigration++
	item.Checkpoint = cloneRegionMap(item.Checkpoint)
	r.migrations[item.ID] = item
	return cloneMigration(item), nil
}

func (r *InMemoryGlobalRegionRepository) GetMigration(organizationID, migrationID int64) (model.RegionMigration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.migrations[migrationID]
	if !ok || item.OrganizationID != organizationID {
		return model.RegionMigration{}, ErrRegionMigrationNotFound
	}
	return cloneMigration(item), nil
}

func (r *InMemoryGlobalRegionRepository) ListMigrations(organizationID int64, limit int) ([]model.RegionMigration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.RegionMigration, 0)
	for _, item := range r.migrations {
		if item.OrganizationID == organizationID {
			items = append(items, cloneMigration(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryGlobalRegionRepository) UpdateMigration(item model.RegionMigration) (model.RegionMigration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.migrations[item.ID]
	if !ok || old.OrganizationID != item.OrganizationID {
		return model.RegionMigration{}, ErrRegionMigrationNotFound
	}
	item.Checkpoint = cloneRegionMap(item.Checkpoint)
	r.migrations[item.ID] = item
	return cloneMigration(item), nil
}

func (r *InMemoryGlobalRegionRepository) CreateTransfer(item model.CrossRegionTransfer) (model.CrossRegionTransfer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextTransfer
	r.nextTransfer++
	r.transfers[item.ID] = item
	return item, nil
}

func (r *InMemoryGlobalRegionRepository) GetTransfer(organizationID, transferID int64) (model.CrossRegionTransfer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.transfers[transferID]
	if !ok || item.OrganizationID != organizationID {
		return model.CrossRegionTransfer{}, ErrCrossRegionTransferNotFound
	}
	return item, nil
}

func (r *InMemoryGlobalRegionRepository) ListTransfers(organizationID int64, limit int) ([]model.CrossRegionTransfer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.CrossRegionTransfer, 0)
	for _, item := range r.transfers {
		if item.OrganizationID == organizationID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryGlobalRegionRepository) UpdateTransfer(item model.CrossRegionTransfer) (model.CrossRegionTransfer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.transfers[item.ID]
	if !ok || old.OrganizationID != item.OrganizationID {
		return model.CrossRegionTransfer{}, ErrCrossRegionTransferNotFound
	}
	r.transfers[item.ID] = item
	return item, nil
}

func (r *InMemoryGlobalRegionRepository) CreateFailoverExercise(item model.FailoverExercise) (model.FailoverExercise, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextExercise
	r.nextExercise++
	r.exercises[item.ID] = item
	return item, nil
}

func (r *InMemoryGlobalRegionRepository) GetFailoverExercise(organizationID, exerciseID int64) (model.FailoverExercise, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.exercises[exerciseID]
	if !ok || item.OrganizationID != organizationID {
		return model.FailoverExercise{}, ErrFailoverExerciseNotFound
	}
	return item, nil
}

func (r *InMemoryGlobalRegionRepository) ListFailoverExercises(organizationID int64, limit int) ([]model.FailoverExercise, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.FailoverExercise, 0)
	for _, item := range r.exercises {
		if item.OrganizationID == organizationID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryGlobalRegionRepository) UpdateFailoverExercise(item model.FailoverExercise) (model.FailoverExercise, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.exercises[item.ID]
	if !ok || old.OrganizationID != item.OrganizationID {
		return model.FailoverExercise{}, ErrFailoverExerciseNotFound
	}
	r.exercises[item.ID] = item
	return item, nil
}

func cloneRegionPolicy(item model.OrganizationRegionPolicy) model.OrganizationRegionPolicy {
	item.AllowedRegions = append([]string(nil), item.AllowedRegions...)
	item.FailoverRegions = append([]string(nil), item.FailoverRegions...)
	return item
}
func clonePlacement(item model.RegionalPlacement) model.RegionalPlacement {
	item.ReplicaRegions = append([]string(nil), item.ReplicaRegions...)
	return item
}
func cloneMigration(item model.RegionMigration) model.RegionMigration {
	item.Checkpoint = cloneRegionMap(item.Checkpoint)
	return item
}
func cloneRegionMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}
