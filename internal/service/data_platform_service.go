package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrDataPlatformForbidden          = errors.New("data platform administration requires organization owner or admin")
	ErrInvalidDataPlatformConnection  = errors.New("invalid data platform connection")
	ErrInvalidDatasetSchema           = errors.New("invalid dataset schema")
	ErrIncompatibleDatasetSchema      = errors.New("dataset schema is not backward compatible")
	ErrInvalidDataExportRequest       = errors.New("invalid data export request")
	ErrDataPlatformCostBudgetExceeded = errors.New("data platform monthly export cost budget exceeded")
	ErrInvalidReverseETLHook          = errors.New("invalid reverse etl hook")
)

type WarehouseAdapter interface {
	Capability() model.WarehouseAdapterCapability
	Deliver(connection model.DataPlatformConnection, rows []model.AnalyticsTaskRecord) (model.WarehouseDeliveryReceipt, error)
}

type DataPlatformService struct {
	repo       repository.DataPlatformRepository
	orgs       repository.OrganizationRepository
	analytics  repository.SearchAnalyticsRepository
	governance repository.GovernanceRepository
	adapters   map[string]WarehouseAdapter
}

func NewDataPlatformService(
	repo repository.DataPlatformRepository,
	orgs repository.OrganizationRepository,
	analytics repository.SearchAnalyticsRepository,
	governance repository.GovernanceRepository,
) *DataPlatformService {
	adapters := map[string]WarehouseAdapter{}
	for _, adapter := range []WarehouseAdapter{
		newContractWarehouseAdapter(model.DataWarehouseBigQuery, "Google BigQuery", "streaming_insert"),
		newContractWarehouseAdapter(model.DataWarehouseSnowflake, "Snowflake", "staged_merge"),
		newContractWarehouseAdapter(model.DataWarehouseRedshift, "Amazon Redshift", "copy_merge"),
		newContractWarehouseAdapter(model.DataWarehouseDatabricks, "Databricks", "delta_merge"),
	} {
		adapters[adapter.Capability().Key] = adapter
	}
	return &DataPlatformService{repo: repo, orgs: orgs, analytics: analytics, governance: governance, adapters: adapters}
}

func (s *DataPlatformService) Adapters() []model.WarehouseAdapterCapability {
	items := make([]model.WarehouseAdapterCapability, 0, len(s.adapters))
	for _, adapter := range s.adapters {
		items = append(items, adapter.Capability())
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })
	return items
}

func (s *DataPlatformService) BIContracts() []model.BIExportContract {
	fields := canonicalDataPlatformFields()
	return []model.BIExportContract{
		{
			Key: model.BIContractPowerBI, DisplayName: "Microsoft Power BI", Dataset: "task_analytics", Version: 1,
			Fields: cloneDataPlatformFields(fields),
			Notes:  []string{"Use organization_id and workspace_id as tenant keys.", "updated_at is the incremental refresh watermark."},
		},
		{
			Key: model.BIContractTableau, DisplayName: "Tableau", Dataset: "task_analytics", Version: 1,
			Fields: cloneDataPlatformFields(fields),
			Notes:  []string{"Use organization_id and workspace_id as row-level security dimensions.", "updated_at supports extract refresh filtering."},
		},
		{
			Key: model.BIContractLooker, DisplayName: "Looker", Dataset: "task_analytics", Version: 1,
			Fields: cloneDataPlatformFields(fields),
			Notes:  []string{"Model organization_id and workspace_id in access filters.", "task_id is unique only within a workspace."},
		},
	}
}

func (s *DataPlatformService) Connections(actorUserID, organizationID int64) ([]model.DataPlatformConnection, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListConnections(organizationID)
}

func (s *DataPlatformService) CreateConnection(actorUserID, organizationID int64, req model.CreateDataPlatformConnectionRequest) (model.DataPlatformConnection, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.DataPlatformConnection{}, err
	}
	name := strings.TrimSpace(req.Name)
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	target := strings.TrimSpace(req.Target)
	if name == "" || len(name) > 200 || target == "" || len(target) > 500 {
		return model.DataPlatformConnection{}, ErrInvalidDataPlatformConnection
	}
	if _, ok := s.adapters[provider]; !ok {
		return model.DataPlatformConnection{}, ErrInvalidDataPlatformConnection
	}
	contracts, ok := normalizeBIContracts(req.BIContracts)
	if !ok || len(contracts) == 0 {
		return model.DataPlatformConnection{}, ErrInvalidDataPlatformConnection
	}
	masking, ok := normalizeMasking(req.Masking)
	if !ok {
		return model.DataPlatformConnection{}, ErrInvalidDataPlatformConnection
	}
	if req.FreshnessSLOMinutes == 0 {
		req.FreshnessSLOMinutes = 1440
	}
	if req.FreshnessSLOMinutes < 1 || req.FreshnessSLOMinutes > 10080 || req.MaxMonthlyCostUSD < 0 {
		return model.DataPlatformConnection{}, ErrInvalidDataPlatformConnection
	}
	config := normalizeStringConfig(req.Config)
	if !validWarehouseConfig(provider, config) {
		return model.DataPlatformConnection{}, ErrInvalidDataPlatformConnection
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateConnection(model.DataPlatformConnection{
		OrganizationID: organizationID, Name: name, Provider: provider, Target: target,
		BIContracts: contracts, Config: config, SecretRef: strings.TrimSpace(req.SecretRef),
		Status: model.DataPlatformStatusActive, Masking: masking,
		FreshnessSLOMinutes: req.FreshnessSLOMinutes, MaxMonthlyCostUSD: req.MaxMonthlyCostUSD,
		CreatedByUserID: actorUserID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return model.DataPlatformConnection{}, err
	}
	_, err = s.repo.CreateSchema(model.DatasetSchemaVersion{
		OrganizationID: organizationID, ConnectionID: item.ID, Version: 1,
		Compatibility: model.SchemaCompatibilityBackward, Status: model.SchemaStatusActive,
		Fields: canonicalDataPlatformFields(), CreatedByUserID: actorUserID, CreatedAt: now,
	})
	if err != nil {
		return model.DataPlatformConnection{}, err
	}
	s.audit(organizationID, actorUserID, "data_platform.connection.created", "data_platform_connection", strconv.FormatInt(item.ID, 10), map[string]any{
		"provider": provider, "target": target, "bi_contracts": contracts,
	})
	return item, nil
}

func (s *DataPlatformService) Schemas(actorUserID, organizationID, connectionID int64) ([]model.DatasetSchemaVersion, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetConnection(organizationID, connectionID); err != nil {
		return nil, err
	}
	return s.repo.ListSchemas(organizationID, connectionID)
}

func (s *DataPlatformService) CreateSchema(actorUserID, organizationID, connectionID int64, req model.CreateDatasetSchemaRequest) (model.DatasetSchemaVersion, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.DatasetSchemaVersion{}, err
	}
	if _, err := s.repo.GetConnection(organizationID, connectionID); err != nil {
		return model.DatasetSchemaVersion{}, err
	}
	fields, ok := normalizeDatasetFields(req.Fields)
	if !ok {
		return model.DatasetSchemaVersion{}, ErrInvalidDatasetSchema
	}
	current, err := s.repo.LatestSchema(organizationID, connectionID)
	if err != nil {
		return model.DatasetSchemaVersion{}, err
	}
	if !backwardCompatibleFields(current.Fields, fields) {
		return model.DatasetSchemaVersion{}, ErrIncompatibleDatasetSchema
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateSchema(model.DatasetSchemaVersion{
		OrganizationID: organizationID, ConnectionID: connectionID, Version: current.Version + 1,
		Compatibility: model.SchemaCompatibilityBackward, Status: model.SchemaStatusActive,
		Fields: fields, CreatedByUserID: actorUserID, CreatedAt: now,
	})
	if err != nil {
		return model.DatasetSchemaVersion{}, err
	}
	if err := s.repo.DeprecateSchema(organizationID, current.ID, now); err != nil {
		return model.DatasetSchemaVersion{}, err
	}
	s.audit(organizationID, actorUserID, "data_platform.schema.published", "data_platform_schema", strconv.FormatInt(item.ID, 10), map[string]any{
		"connection_id": connectionID, "version": item.Version,
	})
	return item, nil
}

func (s *DataPlatformService) ExportJobs(actorUserID, organizationID, connectionID int64) ([]model.DataExportJob, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetConnection(organizationID, connectionID); err != nil {
		return nil, err
	}
	return s.repo.ListExportJobs(organizationID, connectionID, 100)
}

func (s *DataPlatformService) RunExport(actorUserID, organizationID, connectionID int64, req model.RunDataExportRequest) (model.DataExportJob, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.DataExportJob{}, err
	}
	connection, err := s.repo.GetConnection(organizationID, connectionID)
	if err != nil {
		return model.DataExportJob{}, err
	}
	if connection.Status != model.DataPlatformStatusActive {
		return model.DataExportJob{}, ErrInvalidDataExportRequest
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = model.DataExportModeIncremental
	}
	if mode != model.DataExportModeIncremental && mode != model.DataExportModeFull {
		return model.DataExportJob{}, ErrInvalidDataExportRequest
	}
	schema, err := s.repo.LatestSchema(organizationID, connectionID)
	if err != nil {
		return model.DataExportJob{}, err
	}
	now := time.Now().UTC()
	job, err := s.repo.CreateExportJob(model.DataExportJob{
		OrganizationID: organizationID, ConnectionID: connectionID, Mode: mode,
		Status: model.DataExportRunning, SchemaVersion: schema.Version, RequestedByUserID: actorUserID,
		StartedAt: now, CreatedAt: now,
	})
	if err != nil {
		return model.DataExportJob{}, err
	}
	checkpoint, checkpointErr := s.repo.GetCheckpoint(organizationID, connectionID)
	if checkpointErr != nil && !errors.Is(checkpointErr, repository.ErrDataExportCheckpointNotFound) {
		return s.failExport(job, checkpointErr)
	}
	if checkpointErr == nil {
		job.CheckpointBefore = checkpointString(checkpoint)
	}
	rows, sourceDatasets, governanceTags, err := s.collectRows(organizationID)
	if err != nil {
		return s.failExport(job, err)
	}
	if mode == model.DataExportModeIncremental && checkpointErr == nil {
		filtered := rows[:0]
		for _, row := range rows {
			key := analyticsRecordKey(row)
			if row.UpdatedAt.After(checkpoint.LastUpdatedAt) ||
				(row.UpdatedAt.Equal(checkpoint.LastUpdatedAt) && key > checkpoint.LastRecordKey) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].UpdatedAt.Equal(rows[j].UpdatedAt) {
			return analyticsRecordKey(rows[i]) < analyticsRecordKey(rows[j])
		}
		return rows[i].UpdatedAt.Before(rows[j].UpdatedAt)
	})
	for i := range rows {
		rows[i] = applyAnalyticsMasking(rows[i], connection.Masking)
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return s.failExport(job, err)
	}
	estimate := estimateWarehouseCost(connection.Provider, int64(len(payload)))
	if connection.MaxMonthlyCostUSD > 0 {
		jobs, listErr := s.repo.ListExportJobs(organizationID, connectionID, 1000)
		if listErr != nil {
			return s.failExport(job, listErr)
		}
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		spent := 0.0
		for _, previous := range jobs {
			if previous.Status == model.DataExportSucceeded && !previous.CreatedAt.Before(monthStart) {
				spent += previous.EstimatedCostUSD
			}
		}
		if spent+estimate > connection.MaxMonthlyCostUSD {
			return s.failExport(job, ErrDataPlatformCostBudgetExceeded)
		}
	}
	adapter := s.adapters[connection.Provider]
	receipt, err := adapter.Deliver(connection, rows)
	if err != nil {
		return s.failExport(job, err)
	}
	finished := time.Now().UTC()
	job.Status = model.DataExportSucceeded
	job.Rows = len(rows)
	job.Bytes = int64(len(payload))
	job.EstimatedCostUSD = estimate
	job.DeliveryURI = receipt.DeliveryURI
	job.PayloadHash = receipt.PayloadHash
	job.FinishedAt = &finished

	if len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCheckpoint := model.DataExportCheckpoint{
			OrganizationID: organizationID, ConnectionID: connectionID, Version: 1,
			LastUpdatedAt: last.UpdatedAt, LastRecordKey: analyticsRecordKey(last), LastJobID: job.ID, UpdatedAt: finished,
		}
		if checkpointErr == nil {
			nextCheckpoint.Version = checkpoint.Version + 1
		}
		if _, err := s.repo.UpsertCheckpoint(nextCheckpoint); err != nil {
			return s.failExport(job, err)
		}
		job.CheckpointAfter = checkpointString(nextCheckpoint)
	} else {
		job.CheckpointAfter = job.CheckpointBefore
	}
	job, err = s.repo.CompleteExportJob(job)
	if err != nil {
		return model.DataExportJob{}, err
	}
	if _, err := s.repo.CreateLineage(model.DataLineageRecord{
		OrganizationID: organizationID, ConnectionID: connectionID, ExportJobID: job.ID,
		SourceDatasets: sourceDatasets, TargetDataset: connection.Target,
		Fields: schema.Fields, GovernanceTags: governanceTags, CreatedAt: finished,
	}); err != nil {
		return model.DataExportJob{}, err
	}
	s.audit(organizationID, actorUserID, "data_platform.export.succeeded", "data_export_job", strconv.FormatInt(job.ID, 10), map[string]any{
		"provider": connection.Provider, "rows": job.Rows, "bytes": job.Bytes, "schema_version": job.SchemaVersion,
	})
	return job, nil
}

func (s *DataPlatformService) Checkpoint(actorUserID, organizationID, connectionID int64) (model.DataExportCheckpoint, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.DataExportCheckpoint{}, err
	}
	if _, err := s.repo.GetConnection(organizationID, connectionID); err != nil {
		return model.DataExportCheckpoint{}, err
	}
	return s.repo.GetCheckpoint(organizationID, connectionID)
}

func (s *DataPlatformService) Lineage(actorUserID, organizationID int64) ([]model.DataLineageRecord, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListLineage(organizationID, 200)
}

func (s *DataPlatformService) ReverseETLHooks(actorUserID, organizationID int64) ([]model.ReverseETLHook, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListReverseETLHooks(organizationID)
}

func (s *DataPlatformService) CreateReverseETLHook(actorUserID, organizationID int64, req model.CreateReverseETLHookRequest) (model.ReverseETLHook, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.ReverseETLHook{}, err
	}
	if _, err := s.repo.GetConnection(organizationID, req.ConnectionID); err != nil {
		return model.ReverseETLHook{}, err
	}
	name := strings.TrimSpace(req.Name)
	source := strings.TrimSpace(req.SourceObject)
	destination := strings.TrimSpace(req.Destination)
	if name == "" || source == "" || destination == "" || len(name) > 200 || len(source) > 300 || len(destination) > 300 ||
		len(req.FieldMapping) == 0 || len(req.FieldMapping) > 100 {
		return model.ReverseETLHook{}, ErrInvalidReverseETLHook
	}
	mapping := normalizeStringConfig(req.FieldMapping)
	now := time.Now().UTC()
	item, err := s.repo.CreateReverseETLHook(model.ReverseETLHook{
		OrganizationID: organizationID, ConnectionID: req.ConnectionID, Name: name,
		SourceObject: source, Destination: destination, FieldMapping: mapping, Enabled: req.Enabled,
		CreatedByUserID: actorUserID, CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		s.audit(organizationID, actorUserID, "data_platform.reverse_etl_hook.created", "reverse_etl_hook", strconv.FormatInt(item.ID, 10), map[string]any{
			"connection_id": req.ConnectionID, "destination": destination,
		})
	}
	return item, err
}

func (s *DataPlatformService) Dashboard(actorUserID, organizationID int64) (model.DataPlatformDashboard, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.DataPlatformDashboard{}, err
	}
	connections, err := s.repo.ListConnections(organizationID)
	if err != nil {
		return model.DataPlatformDashboard{}, err
	}
	jobs, err := s.repo.ListExportJobs(organizationID, 0, 200)
	if err != nil {
		return model.DataPlatformDashboard{}, err
	}
	now := time.Now().UTC()
	result := model.DataPlatformDashboard{
		OrganizationID: organizationID, Connections: len(connections),
		ConnectionFreshness: map[int64]time.Time{}, GeneratedAt: now,
	}
	for _, connection := range connections {
		if connection.Status == model.DataPlatformStatusActive {
			result.ActiveConnections++
		}
		cp, cpErr := s.repo.GetCheckpoint(organizationID, connection.ID)
		if cpErr == nil {
			result.ConnectionFreshness[connection.ID] = cp.UpdatedAt
			if connection.Status == model.DataPlatformStatusActive &&
				now.Sub(cp.UpdatedAt) > time.Duration(connection.FreshnessSLOMinutes)*time.Minute {
				result.FreshnessBreaches++
			}
		} else if errors.Is(cpErr, repository.ErrDataExportCheckpointNotFound) && connection.Status == model.DataPlatformStatusActive {
			result.FreshnessBreaches++
		} else if cpErr != nil {
			return model.DataPlatformDashboard{}, cpErr
		}
	}
	for _, job := range jobs {
		switch job.Status {
		case model.DataExportSucceeded:
			result.SucceededExports++
			result.RowsExported += int64(job.Rows)
			result.EstimatedCostUSD += job.EstimatedCostUSD
		case model.DataExportFailed:
			result.FailedExports++
		}
	}
	if len(jobs) > 20 {
		jobs = jobs[:20]
	}
	result.LatestJobs = jobs
	return result, nil
}

func (s *DataPlatformService) collectRows(organizationID int64) ([]model.AnalyticsTaskRecord, []string, []string, error) {
	links, err := s.orgs.ListWorkspaces(organizationID)
	if err != nil {
		return nil, nil, nil, err
	}
	rows := make([]model.AnalyticsTaskRecord, 0)
	sources := make([]string, 0, len(links))
	tagSet := map[string]bool{"tenant:organization": true, "lineage:task_analytics": true}
	for _, link := range links {
		exported, err := s.analytics.ExportRows(link.WorkspaceID, map[string]any{}, 10000)
		if err != nil {
			return nil, nil, nil, err
		}
		sources = append(sources, fmt.Sprintf("workspace:%d/tasks", link.WorkspaceID))
		policy, policyErr := s.governance.GetPolicy(link.WorkspaceID)
		if policyErr == nil {
			tagSet["classification:"+policy.DefaultClassification] = true
			if policy.RestrictCrossRegionTransfer {
				tagSet["governance:cross_region_restricted"] = true
			}
		} else if !errors.Is(policyErr, repository.ErrGovernancePolicyNotFound) {
			return nil, nil, nil, policyErr
		}
		inventory, invErr := s.governance.ListDataInventory(link.WorkspaceID)
		if invErr != nil {
			return nil, nil, nil, invErr
		}
		for _, entry := range inventory {
			if entry.ContainsPersonalData {
				tagSet["data:personal"] = true
			}
			tagSet["classification:"+entry.Classification] = true
		}
		for _, row := range exported {
			rows = append(rows, model.AnalyticsTaskRecord{
				OrganizationID: organizationID, WorkspaceID: link.WorkspaceID, TaskID: row.ID,
				Title: row.Title, Status: row.Status, Priority: row.Priority, ProjectID: row.ProjectID,
				DueAt: row.DueAt, CompletedAt: row.CompletedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			})
		}
	}
	tags := make([]string, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	sort.Strings(sources)
	return rows, sources, tags, nil
}

func (s *DataPlatformService) failExport(job model.DataExportJob, failure error) (model.DataExportJob, error) {
	now := time.Now().UTC()
	job.Status = model.DataExportFailed
	job.Error = failure.Error()
	job.FinishedAt = &now
	completed, err := s.repo.CompleteExportJob(job)
	if err != nil {
		return model.DataExportJob{}, err
	}
	return completed, failure
}

func (s *DataPlatformService) requireAdmin(userID, organizationID int64) (model.Organization, error) {
	org, err := s.orgs.GetOrganization(organizationID)
	if err != nil {
		return model.Organization{}, err
	}
	member, err := s.orgs.GetMember(organizationID, userID)
	if err != nil || org.Status != model.OrganizationStatusActive {
		return model.Organization{}, ErrDataPlatformForbidden
	}
	if member.Role != model.OrganizationRoleOwner && member.Role != model.OrganizationRoleAdmin &&
		member.Role != model.OrganizationRoleDelegatedAdmin {
		return model.Organization{}, ErrDataPlatformForbidden
	}
	return org, nil
}

func (s *DataPlatformService) audit(organizationID, actorUserID int64, action, resourceType, resourceID string, metadata map[string]any) {
	uid := actorUserID
	_ = s.orgs.RecordAudit(model.OrganizationAuditEvent{
		OrganizationID: organizationID, ActorUserID: &uid, Action: action,
		ResourceType: resourceType, ResourceID: resourceID, Metadata: metadata, CreatedAt: time.Now().UTC(),
	})
}

type contractWarehouseAdapter struct {
	key, displayName, deliveryMode string
}

func newContractWarehouseAdapter(key, displayName, deliveryMode string) WarehouseAdapter {
	return contractWarehouseAdapter{key: key, displayName: displayName, deliveryMode: deliveryMode}
}

func (a contractWarehouseAdapter) Capability() model.WarehouseAdapterCapability {
	return model.WarehouseAdapterCapability{
		Key: a.key, DisplayName: a.displayName, DeliveryMode: a.deliveryMode,
		BIContracts:      []string{model.BIContractPowerBI, model.BIContractTableau, model.BIContractLooker},
		SupportsRecovery: true,
	}
}

func (a contractWarehouseAdapter) Deliver(connection model.DataPlatformConnection, rows []model.AnalyticsTaskRecord) (model.WarehouseDeliveryReceipt, error) {
	if strings.EqualFold(connection.Config["fail_delivery"], "true") {
		return model.WarehouseDeliveryReceipt{}, errors.New("warehouse adapter delivery failed")
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return model.WarehouseDeliveryReceipt{}, err
	}
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	batchID := hash
	if len(batchID) > 16 {
		batchID = batchID[:16]
	}
	return model.WarehouseDeliveryReceipt{
		Provider: a.key, Target: connection.Target,
		DeliveryURI: fmt.Sprintf("warehouse://%s/%s/batches/%s", a.key, strings.Trim(strings.ReplaceAll(connection.Target, " ", "_"), "/"), batchID),
		BatchID:     batchID, RowCount: len(rows), Bytes: int64(len(payload)), PayloadHash: hash,
	}, nil
}

func canonicalDataPlatformFields() []model.DatasetField {
	return []model.DatasetField{
		{Name: "organization_id", Type: "int64", Nullable: false, Tags: []string{"tenant_key"}, SourcePath: "organization.id"},
		{Name: "workspace_id", Type: "int64", Nullable: false, Tags: []string{"tenant_key"}, SourcePath: "workspace.id"},
		{Name: "task_id", Type: "int64", Nullable: false, Tags: []string{"primary_key"}, SourcePath: "task.id"},
		{Name: "title", Type: "string", Nullable: false, Tags: []string{"maskable"}, SourcePath: "task.title"},
		{Name: "status", Type: "string", Nullable: false, SourcePath: "task.status"},
		{Name: "priority", Type: "string", Nullable: false, SourcePath: "task.priority"},
		{Name: "project_id", Type: "int64", Nullable: true, SourcePath: "task.project_id"},
		{Name: "due_at", Type: "timestamp", Nullable: true, SourcePath: "task.due_at"},
		{Name: "completed_at", Type: "timestamp", Nullable: true, SourcePath: "task.completed_at"},
		{Name: "created_at", Type: "timestamp", Nullable: false, SourcePath: "task.created_at"},
		{Name: "updated_at", Type: "timestamp", Nullable: false, Tags: []string{"incremental_watermark"}, SourcePath: "task.updated_at"},
	}
}

func cloneDataPlatformFields(fields []model.DatasetField) []model.DatasetField {
	out := append([]model.DatasetField(nil), fields...)
	for i := range out {
		out[i].Tags = append([]string(nil), out[i].Tags...)
	}
	return out
}

func normalizeDatasetFields(fields []model.DatasetField) ([]model.DatasetField, bool) {
	if len(fields) == 0 || len(fields) > 100 {
		return nil, false
	}
	seen := map[string]bool{}
	out := make([]model.DatasetField, 0, len(fields))
	for _, field := range fields {
		field.Name = strings.ToLower(strings.TrimSpace(field.Name))
		field.Type = strings.ToLower(strings.TrimSpace(field.Type))
		field.SourcePath = strings.TrimSpace(field.SourcePath)
		if field.Name == "" || seen[field.Name] || !validDatasetType(field.Type) {
			return nil, false
		}
		seen[field.Name] = true
		field.Tags = normalizeDataPlatformStrings(field.Tags)
		out = append(out, field)
	}
	return out, true
}

func backwardCompatibleFields(previous, next []model.DatasetField) bool {
	nextByName := map[string]model.DatasetField{}
	for _, field := range next {
		nextByName[field.Name] = field
	}
	previousNames := map[string]bool{}
	for _, field := range previous {
		previousNames[field.Name] = true
		candidate, ok := nextByName[field.Name]
		if !ok || candidate.Type != field.Type || (field.Nullable && !candidate.Nullable) {
			return false
		}
	}
	for _, field := range next {
		if !previousNames[field.Name] && !field.Nullable {
			return false
		}
	}
	return true
}

func validDatasetType(value string) bool {
	switch value {
	case "string", "int64", "float64", "bool", "timestamp", "date", "json":
		return true
	default:
		return false
	}
}

func normalizeBIContracts(values []string) ([]string, bool) {
	allowed := map[string]bool{model.BIContractPowerBI: true, model.BIContractTableau: true, model.BIContractLooker: true}
	out := normalizeDataPlatformStrings(values)
	for _, value := range out {
		if !allowed[value] {
			return nil, false
		}
	}
	return out, true
}

func normalizeMasking(masking model.DataMaskingPolicy) (model.DataMaskingPolicy, bool) {
	masking.Mode = strings.ToLower(strings.TrimSpace(masking.Mode))
	if masking.Mode == "" {
		masking.Mode = model.MaskingNone
	}
	if masking.Mode != model.MaskingNone && masking.Mode != model.MaskingRedact && masking.Mode != model.MaskingHash {
		return model.DataMaskingPolicy{}, false
	}
	masking.Fields = normalizeDataPlatformStrings(masking.Fields)
	for _, field := range masking.Fields {
		if field != "title" {
			return model.DataMaskingPolicy{}, false
		}
	}
	return masking, true
}

func applyAnalyticsMasking(row model.AnalyticsTaskRecord, masking model.DataMaskingPolicy) model.AnalyticsTaskRecord {
	if masking.Mode == model.MaskingNone {
		return row
	}
	for _, field := range masking.Fields {
		if field != "title" {
			continue
		}
		if masking.Mode == model.MaskingRedact {
			row.Title = "[REDACTED]"
		} else if masking.Mode == model.MaskingHash {
			sum := sha256.Sum256([]byte(row.Title))
			row.Title = "sha256:" + hex.EncodeToString(sum[:])
		}
	}
	return row
}

func normalizeStringConfig(input map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range input {
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if key != "" && len(key) <= 100 && len(value) <= 1000 {
			out[key] = value
		}
	}
	return out
}

func validWarehouseConfig(provider string, config map[string]string) bool {
	required := map[string][]string{
		model.DataWarehouseBigQuery:   {"project_id", "dataset"},
		model.DataWarehouseSnowflake:  {"account", "database", "schema"},
		model.DataWarehouseRedshift:   {"cluster", "database", "schema"},
		model.DataWarehouseDatabricks: {"workspace_url", "catalog", "schema"},
	}
	for _, key := range required[provider] {
		if strings.TrimSpace(config[key]) == "" {
			return false
		}
	}
	return true
}

func normalizeDataPlatformStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func analyticsRecordKey(row model.AnalyticsTaskRecord) string {
	return fmt.Sprintf("%020d:%020d", row.WorkspaceID, row.TaskID)
}

func checkpointString(item model.DataExportCheckpoint) string {
	return item.LastUpdatedAt.UTC().Format(time.RFC3339Nano) + "|" + item.LastRecordKey + "|v" + strconv.Itoa(item.Version)
}

func estimateWarehouseCost(provider string, bytes int64) float64 {
	perGB := map[string]float64{
		model.DataWarehouseBigQuery:   0.005,
		model.DataWarehouseSnowflake:  0.008,
		model.DataWarehouseRedshift:   0.006,
		model.DataWarehouseDatabricks: 0.007,
	}[provider]
	return float64(bytes) / (1024 * 1024 * 1024) * perGB
}
