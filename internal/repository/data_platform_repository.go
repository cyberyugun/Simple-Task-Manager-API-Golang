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
	ErrDataPlatformConnectionNotFound = errors.New("data platform connection not found")
	ErrDatasetSchemaNotFound          = errors.New("dataset schema not found")
	ErrDataExportJobNotFound          = errors.New("data export job not found")
	ErrDataExportCheckpointNotFound   = errors.New("data export checkpoint not found")
	ErrReverseETLHookNotFound         = errors.New("reverse etl hook not found")
)

type DataPlatformRepository interface {
	CreateConnection(item model.DataPlatformConnection) (model.DataPlatformConnection, error)
	GetConnection(organizationID, connectionID int64) (model.DataPlatformConnection, error)
	ListConnections(organizationID int64) ([]model.DataPlatformConnection, error)

	CreateSchema(item model.DatasetSchemaVersion) (model.DatasetSchemaVersion, error)
	ListSchemas(organizationID, connectionID int64) ([]model.DatasetSchemaVersion, error)
	LatestSchema(organizationID, connectionID int64) (model.DatasetSchemaVersion, error)
	DeprecateSchema(organizationID, schemaID int64, now time.Time) error

	CreateExportJob(item model.DataExportJob) (model.DataExportJob, error)
	CompleteExportJob(item model.DataExportJob) (model.DataExportJob, error)
	ListExportJobs(organizationID, connectionID int64, limit int) ([]model.DataExportJob, error)

	GetCheckpoint(organizationID, connectionID int64) (model.DataExportCheckpoint, error)
	UpsertCheckpoint(item model.DataExportCheckpoint) (model.DataExportCheckpoint, error)

	CreateLineage(item model.DataLineageRecord) (model.DataLineageRecord, error)
	ListLineage(organizationID int64, limit int) ([]model.DataLineageRecord, error)

	CreateReverseETLHook(item model.ReverseETLHook) (model.ReverseETLHook, error)
	ListReverseETLHooks(organizationID int64) ([]model.ReverseETLHook, error)
}

type InMemoryDataPlatformRepository struct {
	mu sync.Mutex

	connections map[int64]model.DataPlatformConnection
	schemas     map[int64]model.DatasetSchemaVersion
	jobs        map[int64]model.DataExportJob
	checkpoints map[int64]model.DataExportCheckpoint
	lineage     map[int64]model.DataLineageRecord
	hooks       map[int64]model.ReverseETLHook

	nextConnection int64
	nextSchema     int64
	nextJob        int64
	nextLineage    int64
	nextHook       int64
}

func NewInMemoryDataPlatformRepository() *InMemoryDataPlatformRepository {
	return &InMemoryDataPlatformRepository{
		connections:    map[int64]model.DataPlatformConnection{},
		schemas:        map[int64]model.DatasetSchemaVersion{},
		jobs:           map[int64]model.DataExportJob{},
		checkpoints:    map[int64]model.DataExportCheckpoint{},
		lineage:        map[int64]model.DataLineageRecord{},
		hooks:          map[int64]model.ReverseETLHook{},
		nextConnection: 1,
		nextSchema:     1,
		nextJob:        1,
		nextLineage:    1,
		nextHook:       1,
	}
}

func (r *InMemoryDataPlatformRepository) CreateConnection(item model.DataPlatformConnection) (model.DataPlatformConnection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextConnection
	r.nextConnection++
	item = cloneDataPlatformConnection(item)
	r.connections[item.ID] = item
	return cloneDataPlatformConnection(item), nil
}

func (r *InMemoryDataPlatformRepository) GetConnection(organizationID, connectionID int64) (model.DataPlatformConnection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.connections[connectionID]
	if !ok || item.OrganizationID != organizationID {
		return model.DataPlatformConnection{}, ErrDataPlatformConnectionNotFound
	}
	return cloneDataPlatformConnection(item), nil
}

func (r *InMemoryDataPlatformRepository) ListConnections(organizationID int64) ([]model.DataPlatformConnection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.DataPlatformConnection, 0)
	for _, item := range r.connections {
		if item.OrganizationID == organizationID {
			items = append(items, cloneDataPlatformConnection(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryDataPlatformRepository) CreateSchema(item model.DatasetSchemaVersion) (model.DatasetSchemaVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextSchema
	r.nextSchema++
	item = cloneDatasetSchema(item)
	r.schemas[item.ID] = item
	return cloneDatasetSchema(item), nil
}

func (r *InMemoryDataPlatformRepository) ListSchemas(organizationID, connectionID int64) ([]model.DatasetSchemaVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.DatasetSchemaVersion, 0)
	for _, item := range r.schemas {
		if item.OrganizationID == organizationID && item.ConnectionID == connectionID {
			items = append(items, cloneDatasetSchema(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Version > items[j].Version })
	return items, nil
}

func (r *InMemoryDataPlatformRepository) LatestSchema(organizationID, connectionID int64) (model.DatasetSchemaVersion, error) {
	items, err := r.ListSchemas(organizationID, connectionID)
	if err != nil {
		return model.DatasetSchemaVersion{}, err
	}
	for _, item := range items {
		if item.Status == model.SchemaStatusActive {
			return item, nil
		}
	}
	return model.DatasetSchemaVersion{}, ErrDatasetSchemaNotFound
}

func (r *InMemoryDataPlatformRepository) DeprecateSchema(organizationID, schemaID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.schemas[schemaID]
	if !ok || item.OrganizationID != organizationID {
		return ErrDatasetSchemaNotFound
	}
	item.Status = model.SchemaStatusDeprecated
	item.DeprecatedAt = &now
	r.schemas[schemaID] = item
	return nil
}

func (r *InMemoryDataPlatformRepository) CreateExportJob(item model.DataExportJob) (model.DataExportJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextJob
	r.nextJob++
	r.jobs[item.ID] = item
	return item, nil
}

func (r *InMemoryDataPlatformRepository) CompleteExportJob(item model.DataExportJob) (model.DataExportJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.jobs[item.ID]; !ok {
		return model.DataExportJob{}, ErrDataExportJobNotFound
	}
	r.jobs[item.ID] = item
	return item, nil
}

func (r *InMemoryDataPlatformRepository) ListExportJobs(organizationID, connectionID int64, limit int) ([]model.DataExportJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.DataExportJob, 0)
	for _, item := range r.jobs {
		if item.OrganizationID == organizationID && (connectionID == 0 || item.ConnectionID == connectionID) {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryDataPlatformRepository) GetCheckpoint(organizationID, connectionID int64) (model.DataExportCheckpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.checkpoints[connectionID]
	if !ok || item.OrganizationID != organizationID {
		return model.DataExportCheckpoint{}, ErrDataExportCheckpointNotFound
	}
	return item, nil
}

func (r *InMemoryDataPlatformRepository) UpsertCheckpoint(item model.DataExportCheckpoint) (model.DataExportCheckpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkpoints[item.ConnectionID] = item
	return item, nil
}

func (r *InMemoryDataPlatformRepository) CreateLineage(item model.DataLineageRecord) (model.DataLineageRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextLineage
	r.nextLineage++
	item = cloneDataLineage(item)
	r.lineage[item.ID] = item
	return cloneDataLineage(item), nil
}

func (r *InMemoryDataPlatformRepository) ListLineage(organizationID int64, limit int) ([]model.DataLineageRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.DataLineageRecord, 0)
	for _, item := range r.lineage {
		if item.OrganizationID == organizationID {
			items = append(items, cloneDataLineage(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryDataPlatformRepository) CreateReverseETLHook(item model.ReverseETLHook) (model.ReverseETLHook, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextHook
	r.nextHook++
	item = cloneReverseETLHook(item)
	r.hooks[item.ID] = item
	return cloneReverseETLHook(item), nil
}

func (r *InMemoryDataPlatformRepository) ListReverseETLHooks(organizationID int64) ([]model.ReverseETLHook, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.ReverseETLHook, 0)
	for _, item := range r.hooks {
		if item.OrganizationID == organizationID {
			items = append(items, cloneReverseETLHook(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func cloneDataPlatformConnection(item model.DataPlatformConnection) model.DataPlatformConnection {
	item.BIContracts = append([]string(nil), item.BIContracts...)
	item.Config = cloneStringMap(item.Config)
	item.Masking.Fields = append([]string(nil), item.Masking.Fields...)
	return item
}

func cloneDatasetSchema(item model.DatasetSchemaVersion) model.DatasetSchemaVersion {
	item.Fields = append([]model.DatasetField(nil), item.Fields...)
	for i := range item.Fields {
		item.Fields[i].Tags = append([]string(nil), item.Fields[i].Tags...)
	}
	return item
}

func cloneDataLineage(item model.DataLineageRecord) model.DataLineageRecord {
	item.SourceDatasets = append([]string(nil), item.SourceDatasets...)
	item.GovernanceTags = append([]string(nil), item.GovernanceTags...)
	item.Fields = cloneDatasetSchema(model.DatasetSchemaVersion{Fields: item.Fields}).Fields
	return item
}

func cloneReverseETLHook(item model.ReverseETLHook) model.ReverseETLHook {
	item.FieldMapping = cloneStringMap(item.FieldMapping)
	return item
}

func cloneStringMap(value map[string]string) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	raw, _ := json.Marshal(value)
	var out map[string]string
	_ = json.Unmarshal(raw, &out)
	return out
}
