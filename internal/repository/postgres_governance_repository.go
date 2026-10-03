package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresGovernanceRepository struct {
	db *sql.DB
}

func NewPostgresGovernanceRepository(db *sql.DB) *PostgresGovernanceRepository {
	return &PostgresGovernanceRepository{db: db}
}

func (r *PostgresGovernanceRepository) UpsertPolicy(policy model.GovernancePolicy) (model.GovernancePolicy, error) {
	regions, err := json.Marshal(policy.AllowedDataRegions)
	if err != nil {
		return model.GovernancePolicy{}, err
	}
	return scanGovernancePolicy(r.db.QueryRow(`
		INSERT INTO workspace_governance_policies (
			workspace_id, default_classification, audit_retention_days, operational_retention_days,
			privacy_request_sla_hours, require_dpa, restrict_cross_region_transfer,
			allowed_data_regions, updated_by_user_id, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10)
		ON CONFLICT (workspace_id) DO UPDATE SET
			default_classification = EXCLUDED.default_classification,
			audit_retention_days = EXCLUDED.audit_retention_days,
			operational_retention_days = EXCLUDED.operational_retention_days,
			privacy_request_sla_hours = EXCLUDED.privacy_request_sla_hours,
			require_dpa = EXCLUDED.require_dpa,
			restrict_cross_region_transfer = EXCLUDED.restrict_cross_region_transfer,
			allowed_data_regions = EXCLUDED.allowed_data_regions,
			updated_by_user_id = EXCLUDED.updated_by_user_id,
			updated_at = EXCLUDED.updated_at
		RETURNING workspace_id, default_classification, audit_retention_days, operational_retention_days,
			privacy_request_sla_hours, require_dpa, restrict_cross_region_transfer,
			allowed_data_regions, updated_by_user_id, updated_at
	`, policy.WorkspaceID, policy.DefaultClassification, policy.AuditRetentionDays,
		policy.OperationalRetentionDays, policy.PrivacyRequestSLAHours, policy.RequireDPA,
		policy.RestrictCrossRegionTransfer, string(regions), policy.UpdatedByUserID, policy.UpdatedAt))
}

func (r *PostgresGovernanceRepository) GetPolicy(workspaceID int64) (model.GovernancePolicy, error) {
	item, err := scanGovernancePolicy(r.db.QueryRow(`
		SELECT workspace_id, default_classification, audit_retention_days, operational_retention_days,
			privacy_request_sla_hours, require_dpa, restrict_cross_region_transfer,
			allowed_data_regions, updated_by_user_id, updated_at
		FROM workspace_governance_policies WHERE workspace_id = $1
	`, workspaceID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.GovernancePolicy{}, ErrGovernancePolicyNotFound
	}
	return item, err
}

func (r *PostgresGovernanceRepository) UpsertDataInventory(entry model.DataInventoryEntry) (model.DataInventoryEntry, error) {
	var item model.DataInventoryEntry
	err := r.db.QueryRow(`
		INSERT INTO data_inventory_entries (
			workspace_id, resource_type, field_name, classification, contains_personal_data,
			data_region, retention_days, updated_by_user_id, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (workspace_id, resource_type, field_name) DO UPDATE SET
			classification = EXCLUDED.classification,
			contains_personal_data = EXCLUDED.contains_personal_data,
			data_region = EXCLUDED.data_region,
			retention_days = EXCLUDED.retention_days,
			updated_by_user_id = EXCLUDED.updated_by_user_id,
			updated_at = EXCLUDED.updated_at
		RETURNING id, workspace_id, resource_type, field_name, classification, contains_personal_data,
			data_region, retention_days, updated_by_user_id, updated_at
	`, entry.WorkspaceID, entry.ResourceType, entry.FieldName, entry.Classification,
		entry.ContainsPersonalData, entry.DataRegion, entry.RetentionDays, entry.UpdatedByUserID, entry.UpdatedAt).
		Scan(&item.ID, &item.WorkspaceID, &item.ResourceType, &item.FieldName, &item.Classification,
			&item.ContainsPersonalData, &item.DataRegion, &item.RetentionDays, &item.UpdatedByUserID, &item.UpdatedAt)
	return item, err
}

func (r *PostgresGovernanceRepository) ListDataInventory(workspaceID int64) ([]model.DataInventoryEntry, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, resource_type, field_name, classification, contains_personal_data,
			data_region, retention_days, updated_by_user_id, updated_at
		FROM data_inventory_entries WHERE workspace_id = $1 ORDER BY resource_type, field_name, id
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.DataInventoryEntry, 0)
	for rows.Next() {
		var item model.DataInventoryEntry
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.ResourceType, &item.FieldName, &item.Classification,
			&item.ContainsPersonalData, &item.DataRegion, &item.RetentionDays, &item.UpdatedByUserID, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresGovernanceRepository) CreateLegalHold(hold model.LegalHold) (model.LegalHold, error) {
	var item model.LegalHold
	err := r.db.QueryRow(`
		INSERT INTO legal_holds (workspace_id, name, reason, resource_type, resource_id, created_by_user_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, workspace_id, name, reason, resource_type, resource_id, created_by_user_id, created_at, released_by_user_id, released_at
	`, hold.WorkspaceID, hold.Name, hold.Reason, hold.ResourceType, hold.ResourceID, hold.CreatedByUserID, hold.CreatedAt).
		Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Reason, &item.ResourceType, &item.ResourceID,
			&item.CreatedByUserID, &item.CreatedAt, &item.ReleasedByUserID, &item.ReleasedAt)
	return item, err
}

func (r *PostgresGovernanceRepository) ListLegalHolds(workspaceID int64, activeOnly bool) ([]model.LegalHold, error) {
	query := `
		SELECT id, workspace_id, name, reason, resource_type, resource_id, created_by_user_id, created_at, released_by_user_id, released_at
		FROM legal_holds WHERE workspace_id = $1`
	if activeOnly {
		query += " AND released_at IS NULL"
	}
	query += " ORDER BY created_at DESC, id DESC"
	rows, err := r.db.Query(query, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LegalHold, 0)
	for rows.Next() {
		var item model.LegalHold
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Reason, &item.ResourceType, &item.ResourceID,
			&item.CreatedByUserID, &item.CreatedAt, &item.ReleasedByUserID, &item.ReleasedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresGovernanceRepository) ReleaseLegalHold(workspaceID, holdID, actorUserID int64, now time.Time) error {
	result, err := r.db.Exec(`
		UPDATE legal_holds SET released_by_user_id = $3, released_at = $4
		WHERE workspace_id = $1 AND id = $2 AND released_at IS NULL
	`, workspaceID, holdID, actorUserID, now)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		var exists bool
		if err := r.db.QueryRow("SELECT EXISTS (SELECT 1 FROM legal_holds WHERE workspace_id = $1 AND id = $2)", workspaceID, holdID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrLegalHoldNotFound
		}
	}
	return nil
}

func (r *PostgresGovernanceRepository) HasActiveLegalHold(workspaceID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRow("SELECT EXISTS (SELECT 1 FROM legal_holds WHERE workspace_id = $1 AND released_at IS NULL)", workspaceID).Scan(&exists)
	return exists, err
}

func (r *PostgresGovernanceRepository) CreatePrivacyRequest(request model.PrivacyRequest) (model.PrivacyRequest, error) {
	var item model.PrivacyRequest
	err := r.db.QueryRow(`
		INSERT INTO privacy_requests (
			workspace_id, subject_user_id, request_type, status, reason,
			requested_by_user_id, requested_at, due_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, workspace_id, subject_user_id, request_type, status, reason,
			requested_by_user_id, requested_at, due_at, completed_by_user_id, completed_at
	`, request.WorkspaceID, request.SubjectUserID, request.Type, request.Status, request.Reason,
		request.RequestedByUserID, request.RequestedAt, request.DueAt).
		Scan(&item.ID, &item.WorkspaceID, &item.SubjectUserID, &item.Type, &item.Status, &item.Reason,
			&item.RequestedByUserID, &item.RequestedAt, &item.DueAt, &item.CompletedByUserID, &item.CompletedAt)
	return item, err
}

func (r *PostgresGovernanceRepository) ListPrivacyRequests(workspaceID int64) ([]model.PrivacyRequest, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, subject_user_id, request_type, status, reason,
			requested_by_user_id, requested_at, due_at, completed_by_user_id, completed_at
		FROM privacy_requests WHERE workspace_id = $1 ORDER BY requested_at DESC, id DESC
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.PrivacyRequest, 0)
	for rows.Next() {
		var item model.PrivacyRequest
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.SubjectUserID, &item.Type, &item.Status, &item.Reason,
			&item.RequestedByUserID, &item.RequestedAt, &item.DueAt, &item.CompletedByUserID, &item.CompletedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresGovernanceRepository) CompletePrivacyRequest(workspaceID, requestID, actorUserID int64, status, reason string, now time.Time) (model.PrivacyRequest, error) {
	var item model.PrivacyRequest
	err := r.db.QueryRow(`
		UPDATE privacy_requests SET status = $3, reason = $4, completed_by_user_id = $5, completed_at = $6
		WHERE workspace_id = $1 AND id = $2 AND completed_at IS NULL
		RETURNING id, workspace_id, subject_user_id, request_type, status, reason,
			requested_by_user_id, requested_at, due_at, completed_by_user_id, completed_at
	`, workspaceID, requestID, status, reason, actorUserID, now).
		Scan(&item.ID, &item.WorkspaceID, &item.SubjectUserID, &item.Type, &item.Status, &item.Reason,
			&item.RequestedByUserID, &item.RequestedAt, &item.DueAt, &item.CompletedByUserID, &item.CompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.PrivacyRequest{}, ErrPrivacyRequestNotFound
	}
	return item, err
}

func (r *PostgresGovernanceRepository) CreateComplianceEvidence(evidence model.ComplianceEvidence) (model.ComplianceEvidence, error) {
	raw, err := json.Marshal(evidence.Metadata)
	if err != nil {
		return model.ComplianceEvidence{}, err
	}
	var item model.ComplianceEvidence
	var metadata []byte
	err = r.db.QueryRow(`
		INSERT INTO compliance_evidence (
			workspace_id, framework, control_id, evidence_type, description, metadata, created_by_user_id, created_at
		) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8)
		RETURNING id, workspace_id, framework, control_id, evidence_type, description, metadata, created_by_user_id, created_at
	`, evidence.WorkspaceID, evidence.Framework, evidence.Control, evidence.EvidenceType, evidence.Description,
		string(raw), evidence.CreatedByUserID, evidence.CreatedAt).
		Scan(&item.ID, &item.WorkspaceID, &item.Framework, &item.Control, &item.EvidenceType,
			&item.Description, &metadata, &item.CreatedByUserID, &item.CreatedAt)
	if err != nil {
		return model.ComplianceEvidence{}, err
	}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &item.Metadata); err != nil {
			return model.ComplianceEvidence{}, err
		}
	}
	return item, nil
}

func (r *PostgresGovernanceRepository) ListComplianceEvidence(workspaceID int64, limit int) ([]model.ComplianceEvidence, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, framework, control_id, evidence_type, description, metadata, created_by_user_id, created_at
		FROM compliance_evidence WHERE workspace_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2
	`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.ComplianceEvidence, 0)
	for rows.Next() {
		var item model.ComplianceEvidence
		var metadata []byte
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Framework, &item.Control, &item.EvidenceType,
			&item.Description, &metadata, &item.CreatedByUserID, &item.CreatedAt); err != nil {
			return nil, err
		}
		if len(metadata) > 0 {
			if err := json.Unmarshal(metadata, &item.Metadata); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type governancePolicyScanner interface {
	Scan(dest ...any) error
}

func scanGovernancePolicy(scanner governancePolicyScanner) (model.GovernancePolicy, error) {
	var item model.GovernancePolicy
	var raw []byte
	err := scanner.Scan(&item.WorkspaceID, &item.DefaultClassification, &item.AuditRetentionDays,
		&item.OperationalRetentionDays, &item.PrivacyRequestSLAHours, &item.RequireDPA,
		&item.RestrictCrossRegionTransfer, &raw, &item.UpdatedByUserID, &item.UpdatedAt)
	if err != nil {
		return model.GovernancePolicy{}, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &item.AllowedDataRegions); err != nil {
			return model.GovernancePolicy{}, err
		}
	}
	return item, nil
}
