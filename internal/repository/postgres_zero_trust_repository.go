package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresZeroTrustRepository struct {
	db *sql.DB
}

func NewPostgresZeroTrustRepository(db *sql.DB) *PostgresZeroTrustRepository {
	return &PostgresZeroTrustRepository{db: db}
}

func (r *PostgresZeroTrustRepository) GetPolicy(organizationID int64) (model.ZeroTrustPolicy, error) {
	var item model.ZeroTrustPolicy
	var allowedRaw, deniedRaw []byte
	err := r.db.QueryRow(`
		SELECT organization_id, enabled, require_workload_mtls, require_bound_tokens, step_up_risk_score,
		       revoke_risk_score, impossible_travel_kph, waf_block_score, allowed_cidrs, denied_cidrs,
		       auto_revoke_high_risk, require_trusted_device, siem_federation_enabled,
		       audit_checkpoint_interval, updated_by_user_id, created_at, updated_at
		FROM zero_trust_policies
		WHERE organization_id = $1
	`, organizationID).Scan(
		&item.OrganizationID, &item.Enabled, &item.RequireWorkloadMTLS, &item.RequireBoundTokens,
		&item.StepUpRiskScore, &item.RevokeRiskScore, &item.ImpossibleTravelKPH, &item.WAFBlockScore,
		&allowedRaw, &deniedRaw, &item.AutoRevokeHighRisk, &item.RequireTrustedDevice,
		&item.SIEMFederationEnabled, &item.AuditCheckpointInterval, &item.UpdatedByUserID,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ZeroTrustPolicy{}, ErrZeroTrustPolicyNotFound
	}
	if err != nil {
		return model.ZeroTrustPolicy{}, err
	}
	if err := json.Unmarshal(allowedRaw, &item.AllowedCIDRs); err != nil {
		return model.ZeroTrustPolicy{}, err
	}
	if err := json.Unmarshal(deniedRaw, &item.DeniedCIDRs); err != nil {
		return model.ZeroTrustPolicy{}, err
	}
	return item, nil
}

func (r *PostgresZeroTrustRepository) UpsertPolicy(item model.ZeroTrustPolicy) (model.ZeroTrustPolicy, error) {
	allowedRaw, err := json.Marshal(item.AllowedCIDRs)
	if err != nil {
		return model.ZeroTrustPolicy{}, err
	}
	deniedRaw, err := json.Marshal(item.DeniedCIDRs)
	if err != nil {
		return model.ZeroTrustPolicy{}, err
	}
	var allowedOut, deniedOut []byte
	err = r.db.QueryRow(`
		INSERT INTO zero_trust_policies (
			organization_id, enabled, require_workload_mtls, require_bound_tokens, step_up_risk_score,
			revoke_risk_score, impossible_travel_kph, waf_block_score, allowed_cidrs, denied_cidrs,
			auto_revoke_high_risk, require_trusted_device, siem_federation_enabled,
			audit_checkpoint_interval, updated_by_user_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (organization_id) DO UPDATE SET
			enabled=EXCLUDED.enabled,
			require_workload_mtls=EXCLUDED.require_workload_mtls,
			require_bound_tokens=EXCLUDED.require_bound_tokens,
			step_up_risk_score=EXCLUDED.step_up_risk_score,
			revoke_risk_score=EXCLUDED.revoke_risk_score,
			impossible_travel_kph=EXCLUDED.impossible_travel_kph,
			waf_block_score=EXCLUDED.waf_block_score,
			allowed_cidrs=EXCLUDED.allowed_cidrs,
			denied_cidrs=EXCLUDED.denied_cidrs,
			auto_revoke_high_risk=EXCLUDED.auto_revoke_high_risk,
			require_trusted_device=EXCLUDED.require_trusted_device,
			siem_federation_enabled=EXCLUDED.siem_federation_enabled,
			audit_checkpoint_interval=EXCLUDED.audit_checkpoint_interval,
			updated_by_user_id=EXCLUDED.updated_by_user_id,
			updated_at=EXCLUDED.updated_at
		RETURNING organization_id, enabled, require_workload_mtls, require_bound_tokens, step_up_risk_score,
		          revoke_risk_score, impossible_travel_kph, waf_block_score, allowed_cidrs, denied_cidrs,
		          auto_revoke_high_risk, require_trusted_device, siem_federation_enabled,
		          audit_checkpoint_interval, updated_by_user_id, created_at, updated_at
	`, item.OrganizationID, item.Enabled, item.RequireWorkloadMTLS, item.RequireBoundTokens,
		item.StepUpRiskScore, item.RevokeRiskScore, item.ImpossibleTravelKPH, item.WAFBlockScore,
		string(allowedRaw), string(deniedRaw), item.AutoRevokeHighRisk, item.RequireTrustedDevice,
		item.SIEMFederationEnabled, item.AuditCheckpointInterval, item.UpdatedByUserID,
		item.CreatedAt, item.UpdatedAt,
	).Scan(
		&item.OrganizationID, &item.Enabled, &item.RequireWorkloadMTLS, &item.RequireBoundTokens,
		&item.StepUpRiskScore, &item.RevokeRiskScore, &item.ImpossibleTravelKPH, &item.WAFBlockScore,
		&allowedOut, &deniedOut, &item.AutoRevokeHighRisk, &item.RequireTrustedDevice,
		&item.SIEMFederationEnabled, &item.AuditCheckpointInterval, &item.UpdatedByUserID,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.ZeroTrustPolicy{}, err
	}
	if err := json.Unmarshal(allowedOut, &item.AllowedCIDRs); err != nil {
		return model.ZeroTrustPolicy{}, err
	}
	if err := json.Unmarshal(deniedOut, &item.DeniedCIDRs); err != nil {
		return model.ZeroTrustPolicy{}, err
	}
	return item, nil
}

func (r *PostgresZeroTrustRepository) CreateWorkload(item model.WorkloadIdentity) (model.WorkloadIdentity, error) {
	raw, err := json.Marshal(item.AllowedScopes)
	if err != nil {
		return model.WorkloadIdentity{}, err
	}
	var out []byte
	err = r.db.QueryRow(`
		INSERT INTO workload_identities (
			organization_id, workspace_id, name, spiffe_id, status, allowed_scopes, mtls_required,
			created_by_user_id, created_at, updated_at, revoked_at, last_authenticated_at
		) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11,$12)
		RETURNING id, organization_id, workspace_id, name, spiffe_id, status, allowed_scopes, mtls_required,
		          created_by_user_id, created_at, updated_at, revoked_at, last_authenticated_at
	`, item.OrganizationID, item.WorkspaceID, item.Name, item.SPIFFEID, item.Status, string(raw), item.MTLSRequired,
		item.CreatedByUserID, item.CreatedAt, item.UpdatedAt, item.RevokedAt, item.LastAuthenticatedAt,
	).Scan(
		&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.Name, &item.SPIFFEID, &item.Status, &out, &item.MTLSRequired,
		&item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt, &item.RevokedAt, &item.LastAuthenticatedAt,
	)
	if err != nil {
		return model.WorkloadIdentity{}, err
	}
	if err := json.Unmarshal(out, &item.AllowedScopes); err != nil {
		return model.WorkloadIdentity{}, err
	}
	return item, nil
}

func (r *PostgresZeroTrustRepository) GetWorkload(organizationID, workloadID int64) (model.WorkloadIdentity, error) {
	item, err := scanWorkload(r.db.QueryRow(`
		SELECT id, organization_id, workspace_id, name, spiffe_id, status, allowed_scopes, mtls_required,
		       created_by_user_id, created_at, updated_at, revoked_at, last_authenticated_at
		FROM workload_identities
		WHERE organization_id=$1 AND id=$2
	`, organizationID, workloadID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkloadIdentity{}, ErrWorkloadIdentityNotFound
	}
	return item, err
}

func (r *PostgresZeroTrustRepository) ListWorkloads(organizationID int64) ([]model.WorkloadIdentity, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, workspace_id, name, spiffe_id, status, allowed_scopes, mtls_required,
		       created_by_user_id, created_at, updated_at, revoked_at, last_authenticated_at
		FROM workload_identities
		WHERE organization_id=$1
		ORDER BY id
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.WorkloadIdentity, 0)
	for rows.Next() {
		item, err := scanWorkload(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresZeroTrustRepository) RevokeWorkload(organizationID, workloadID int64, now time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`
		UPDATE workload_identities
		SET status='revoked', revoked_at=$3, updated_at=$3
		WHERE organization_id=$1 AND id=$2 AND revoked_at IS NULL
	`, organizationID, workloadID, now)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrWorkloadIdentityNotFound
	}
	if _, err := tx.Exec(`
		UPDATE workload_certificates
		SET revoked_at=$2
		WHERE workload_id=$1 AND revoked_at IS NULL
	`, workloadID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *PostgresZeroTrustRepository) TouchWorkloadAuthentication(organizationID, workloadID int64, now time.Time) error {
	result, err := r.db.Exec(`
		UPDATE workload_identities
		SET last_authenticated_at=$3, updated_at=$3
		WHERE organization_id=$1 AND id=$2 AND status='active' AND revoked_at IS NULL
	`, organizationID, workloadID, now)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrWorkloadIdentityNotFound
	}
	return nil
}

type zeroTrustScanner interface {
	Scan(dest ...any) error
}

func scanWorkload(scanner zeroTrustScanner) (model.WorkloadIdentity, error) {
	var item model.WorkloadIdentity
	var raw []byte
	err := scanner.Scan(
		&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.Name, &item.SPIFFEID, &item.Status, &raw, &item.MTLSRequired,
		&item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt, &item.RevokedAt, &item.LastAuthenticatedAt,
	)
	if err != nil {
		return model.WorkloadIdentity{}, err
	}
	if err := json.Unmarshal(raw, &item.AllowedScopes); err != nil {
		return model.WorkloadIdentity{}, err
	}
	return item, nil
}

func (r *PostgresZeroTrustRepository) CreateCertificate(item model.WorkloadCertificate, replaceExisting bool) (model.WorkloadCertificate, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return model.WorkloadCertificate{}, err
	}
	defer tx.Rollback()

	err = tx.QueryRow(`
		INSERT INTO workload_certificates (
			organization_id, workload_id, serial_number, sha256_fingerprint, subject, not_before, not_after,
			created_by_user_id, created_at, revoked_at, replaced_by_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, organization_id, workload_id, serial_number, sha256_fingerprint, subject, not_before, not_after,
		          created_by_user_id, created_at, revoked_at, replaced_by_id
	`, item.OrganizationID, item.WorkloadID, item.SerialNumber, item.SHA256Fingerprint, item.Subject,
		item.NotBefore, item.NotAfter, item.CreatedByUserID, item.CreatedAt, item.RevokedAt, item.ReplacedByID,
	).Scan(
		&item.ID, &item.OrganizationID, &item.WorkloadID, &item.SerialNumber, &item.SHA256Fingerprint,
		&item.Subject, &item.NotBefore, &item.NotAfter, &item.CreatedByUserID, &item.CreatedAt,
		&item.RevokedAt, &item.ReplacedByID,
	)
	if err != nil {
		return model.WorkloadCertificate{}, err
	}
	if replaceExisting {
		if _, err := tx.Exec(`
			UPDATE workload_certificates
			SET revoked_at=$3, replaced_by_id=$4
			WHERE workload_id=$1 AND id<>$2 AND revoked_at IS NULL
		`, item.WorkloadID, item.ID, item.CreatedAt, item.ID); err != nil {
			return model.WorkloadCertificate{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.WorkloadCertificate{}, err
	}
	return item, nil
}

func (r *PostgresZeroTrustRepository) ListCertificates(organizationID, workloadID int64) ([]model.WorkloadCertificate, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, workload_id, serial_number, sha256_fingerprint, subject, not_before, not_after,
		       created_by_user_id, created_at, revoked_at, replaced_by_id
		FROM workload_certificates
		WHERE organization_id=$1 AND workload_id=$2
		ORDER BY id DESC
	`, organizationID, workloadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCertificates(rows)
}

func (r *PostgresZeroTrustRepository) ListOrganizationCertificates(organizationID int64) ([]model.WorkloadCertificate, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, workload_id, serial_number, sha256_fingerprint, subject, not_before, not_after,
		       created_by_user_id, created_at, revoked_at, replaced_by_id
		FROM workload_certificates
		WHERE organization_id=$1
		ORDER BY id DESC
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanCertificates(rows)
}

func scanCertificates(rows *sql.Rows) ([]model.WorkloadCertificate, error) {
	items := make([]model.WorkloadCertificate, 0)
	for rows.Next() {
		var item model.WorkloadCertificate
		if err := rows.Scan(
			&item.ID, &item.OrganizationID, &item.WorkloadID, &item.SerialNumber, &item.SHA256Fingerprint,
			&item.Subject, &item.NotBefore, &item.NotAfter, &item.CreatedByUserID, &item.CreatedAt,
			&item.RevokedAt, &item.ReplacedByID,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresZeroTrustRepository) FindActiveCertificateByFingerprint(fingerprint string, now time.Time) (model.WorkloadIdentity, model.WorkloadCertificate, error) {
	var workload model.WorkloadIdentity
	var cert model.WorkloadCertificate
	var scopesRaw []byte
	err := r.db.QueryRow(`
		SELECT w.id, w.organization_id, w.workspace_id, w.name, w.spiffe_id, w.status, w.allowed_scopes, w.mtls_required,
		       w.created_by_user_id, w.created_at, w.updated_at, w.revoked_at, w.last_authenticated_at,
		       c.id, c.organization_id, c.workload_id, c.serial_number, c.sha256_fingerprint, c.subject,
		       c.not_before, c.not_after, c.created_by_user_id, c.created_at, c.revoked_at, c.replaced_by_id
		FROM workload_certificates c
		JOIN workload_identities w ON w.id=c.workload_id
		WHERE c.sha256_fingerprint=$1
		  AND c.revoked_at IS NULL
		  AND c.not_before <= $2
		  AND c.not_after > $2
		  AND w.status='active'
		  AND w.revoked_at IS NULL
	`, fingerprint, now).Scan(
		&workload.ID, &workload.OrganizationID, &workload.WorkspaceID, &workload.Name, &workload.SPIFFEID, &workload.Status,
		&scopesRaw, &workload.MTLSRequired, &workload.CreatedByUserID, &workload.CreatedAt,
		&workload.UpdatedAt, &workload.RevokedAt, &workload.LastAuthenticatedAt,
		&cert.ID, &cert.OrganizationID, &cert.WorkloadID, &cert.SerialNumber, &cert.SHA256Fingerprint,
		&cert.Subject, &cert.NotBefore, &cert.NotAfter, &cert.CreatedByUserID, &cert.CreatedAt,
		&cert.RevokedAt, &cert.ReplacedByID,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkloadIdentity{}, model.WorkloadCertificate{}, ErrWorkloadCertificateNotFound
	}
	if err != nil {
		return model.WorkloadIdentity{}, model.WorkloadCertificate{}, err
	}
	if err := json.Unmarshal(scopesRaw, &workload.AllowedScopes); err != nil {
		return model.WorkloadIdentity{}, model.WorkloadCertificate{}, err
	}
	return workload, cert, nil
}

func (r *PostgresZeroTrustRepository) UpsertDeviceTrust(item model.DeviceTrust) (model.DeviceTrust, error) {
	err := r.db.QueryRow(`
		INSERT INTO zero_trust_device_trust (
			organization_id, user_id, device_hash, label, trust_level, last_seen_at, expires_at, created_at, revoked_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (organization_id, user_id, device_hash) DO UPDATE SET
			label=EXCLUDED.label,
			trust_level=EXCLUDED.trust_level,
			last_seen_at=EXCLUDED.last_seen_at,
			expires_at=EXCLUDED.expires_at,
			revoked_at=NULL
		RETURNING id, organization_id, user_id, device_hash, label, trust_level, last_seen_at, expires_at, created_at, revoked_at
	`, item.OrganizationID, item.UserID, item.DeviceHash, item.Label, item.TrustLevel, item.LastSeenAt,
		item.ExpiresAt, item.CreatedAt, item.RevokedAt,
	).Scan(
		&item.ID, &item.OrganizationID, &item.UserID, &item.DeviceHash, &item.Label, &item.TrustLevel,
		&item.LastSeenAt, &item.ExpiresAt, &item.CreatedAt, &item.RevokedAt,
	)
	return item, err
}

func (r *PostgresZeroTrustRepository) GetActiveDeviceTrust(organizationID, userID int64, deviceHash string, now time.Time) (model.DeviceTrust, error) {
	var item model.DeviceTrust
	err := r.db.QueryRow(`
		SELECT id, organization_id, user_id, device_hash, label, trust_level, last_seen_at, expires_at, created_at, revoked_at
		FROM zero_trust_device_trust
		WHERE organization_id=$1 AND user_id=$2 AND device_hash=$3
		  AND revoked_at IS NULL AND expires_at>$4
	`, organizationID, userID, deviceHash, now).Scan(
		&item.ID, &item.OrganizationID, &item.UserID, &item.DeviceHash, &item.Label, &item.TrustLevel,
		&item.LastSeenAt, &item.ExpiresAt, &item.CreatedAt, &item.RevokedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.DeviceTrust{}, ErrDeviceTrustNotFound
	}
	return item, err
}

func (r *PostgresZeroTrustRepository) ListDeviceTrust(organizationID int64, now time.Time) ([]model.DeviceTrust, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, user_id, device_hash, label, trust_level, last_seen_at, expires_at, created_at, revoked_at
		FROM zero_trust_device_trust
		WHERE organization_id=$1 AND revoked_at IS NULL AND expires_at>$2
		ORDER BY id DESC
	`, organizationID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.DeviceTrust, 0)
	for rows.Next() {
		var item model.DeviceTrust
		if err := rows.Scan(
			&item.ID, &item.OrganizationID, &item.UserID, &item.DeviceHash, &item.Label, &item.TrustLevel,
			&item.LastSeenAt, &item.ExpiresAt, &item.CreatedAt, &item.RevokedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresZeroTrustRepository) RevokeDeviceTrust(organizationID, deviceID int64, now time.Time) error {
	result, err := r.db.Exec(`
		UPDATE zero_trust_device_trust
		SET revoked_at=$3
		WHERE organization_id=$1 AND id=$2 AND revoked_at IS NULL
	`, organizationID, deviceID, now)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrDeviceTrustNotFound
	}
	return nil
}

func (r *PostgresZeroTrustRepository) CreateSecurityEvent(item model.SecurityEvent) (model.SecurityEvent, error) {
	indicatorsRaw, err := json.Marshal(item.Indicators)
	if err != nil {
		return model.SecurityEvent{}, err
	}
	metadataRaw, err := json.Marshal(item.Metadata)
	if err != nil {
		return model.SecurityEvent{}, err
	}
	var indicatorsOut, metadataOut []byte
	err = r.db.QueryRow(`
		INSERT INTO zero_trust_security_events (
			organization_id, user_id, session_id, workload_id, type, severity, risk_score, action,
			source_ip, indicators, metadata, occurred_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb,$12)
		RETURNING id, organization_id, user_id, session_id, workload_id, type, severity, risk_score, action,
		          source_ip, indicators, metadata, occurred_at
	`, item.OrganizationID, item.UserID, item.SessionID, item.WorkloadID, item.Type, item.Severity,
		item.RiskScore, item.Action, item.SourceIP, string(indicatorsRaw), string(metadataRaw), item.OccurredAt,
	).Scan(
		&item.ID, &item.OrganizationID, &item.UserID, &item.SessionID, &item.WorkloadID, &item.Type,
		&item.Severity, &item.RiskScore, &item.Action, &item.SourceIP, &indicatorsOut, &metadataOut,
		&item.OccurredAt,
	)
	if err != nil {
		return model.SecurityEvent{}, err
	}
	if err := json.Unmarshal(indicatorsOut, &item.Indicators); err != nil {
		return model.SecurityEvent{}, err
	}
	if err := json.Unmarshal(metadataOut, &item.Metadata); err != nil {
		return model.SecurityEvent{}, err
	}
	return item, nil
}

func (r *PostgresZeroTrustRepository) ListSecurityEvents(organizationID, afterID int64, limit int) ([]model.SecurityEvent, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, user_id, session_id, workload_id, type, severity, risk_score, action,
		       source_ip, indicators, metadata, occurred_at
		FROM zero_trust_security_events
		WHERE organization_id=$1 AND id>$2
		ORDER BY id
		LIMIT $3
	`, organizationID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.SecurityEvent, 0)
	for rows.Next() {
		var item model.SecurityEvent
		var indicatorsRaw, metadataRaw []byte
		if err := rows.Scan(
			&item.ID, &item.OrganizationID, &item.UserID, &item.SessionID, &item.WorkloadID, &item.Type,
			&item.Severity, &item.RiskScore, &item.Action, &item.SourceIP, &indicatorsRaw, &metadataRaw,
			&item.OccurredAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(indicatorsRaw, &item.Indicators); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(metadataRaw, &item.Metadata); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresZeroTrustRepository) CreateCheckpoint(item model.AuditCheckpoint) (model.AuditCheckpoint, error) {
	err := r.db.QueryRow(`
		INSERT INTO zero_trust_audit_checkpoints (
			organization_id, sequence, first_event_id, last_event_id, event_count, previous_hash,
			payload_hash, checkpoint_hash, signature, created_by_user_id, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, organization_id, sequence, first_event_id, last_event_id, event_count,
		          previous_hash, payload_hash, checkpoint_hash, signature, created_by_user_id, created_at
	`, item.OrganizationID, item.Sequence, item.FirstEventID, item.LastEventID, item.EventCount,
		item.PreviousHash, item.PayloadHash, item.CheckpointHash, item.Signature, item.CreatedByUserID,
		item.CreatedAt,
	).Scan(
		&item.ID, &item.OrganizationID, &item.Sequence, &item.FirstEventID, &item.LastEventID,
		&item.EventCount, &item.PreviousHash, &item.PayloadHash, &item.CheckpointHash,
		&item.Signature, &item.CreatedByUserID, &item.CreatedAt,
	)
	return item, err
}

func (r *PostgresZeroTrustRepository) LatestCheckpoint(organizationID int64) (model.AuditCheckpoint, error) {
	var item model.AuditCheckpoint
	err := r.db.QueryRow(`
		SELECT id, organization_id, sequence, first_event_id, last_event_id, event_count,
		       previous_hash, payload_hash, checkpoint_hash, signature, created_by_user_id, created_at
		FROM zero_trust_audit_checkpoints
		WHERE organization_id=$1
		ORDER BY sequence DESC
		LIMIT 1
	`, organizationID).Scan(
		&item.ID, &item.OrganizationID, &item.Sequence, &item.FirstEventID, &item.LastEventID,
		&item.EventCount, &item.PreviousHash, &item.PayloadHash, &item.CheckpointHash,
		&item.Signature, &item.CreatedByUserID, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AuditCheckpoint{}, ErrAuditCheckpointNotFound
	}
	return item, err
}

func (r *PostgresZeroTrustRepository) ListCheckpoints(organizationID int64, limit int) ([]model.AuditCheckpoint, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, sequence, first_event_id, last_event_id, event_count,
		       previous_hash, payload_hash, checkpoint_hash, signature, created_by_user_id, created_at
		FROM zero_trust_audit_checkpoints
		WHERE organization_id=$1
		ORDER BY sequence DESC
		LIMIT $2
	`, organizationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AuditCheckpoint, 0)
	for rows.Next() {
		var item model.AuditCheckpoint
		if err := rows.Scan(
			&item.ID, &item.OrganizationID, &item.Sequence, &item.FirstEventID, &item.LastEventID,
			&item.EventCount, &item.PreviousHash, &item.PayloadHash, &item.CheckpointHash,
			&item.Signature, &item.CreatedByUserID, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresZeroTrustRepository) CreateWORMExport(item model.WORMAuditExport) (model.WORMAuditExport, error) {
	err := r.db.QueryRow(`
		INSERT INTO zero_trust_worm_exports (
			organization_id, from_event_id, to_event_id, event_count, root_hash, checkpoint_hash,
			object_uri, created_by_user_id, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, organization_id, from_event_id, to_event_id, event_count, root_hash,
		          checkpoint_hash, object_uri, created_by_user_id, created_at
	`, item.OrganizationID, item.FromEventID, item.ToEventID, item.EventCount, item.RootHash,
		item.CheckpointHash, item.ObjectURI, item.CreatedByUserID, item.CreatedAt,
	).Scan(
		&item.ID, &item.OrganizationID, &item.FromEventID, &item.ToEventID, &item.EventCount,
		&item.RootHash, &item.CheckpointHash, &item.ObjectURI, &item.CreatedByUserID, &item.CreatedAt,
	)
	return item, err
}

func (r *PostgresZeroTrustRepository) CreateSIEMDestination(item model.SIEMDestination) (model.SIEMDestination, error) {
	raw, err := json.Marshal(item.EventTypes)
	if err != nil {
		return model.SIEMDestination{}, err
	}
	var out []byte
	err = r.db.QueryRow(`
		INSERT INTO zero_trust_siem_destinations (
			organization_id, name, provider, endpoint_url, event_types, enabled, secret_ref,
			created_by_user_id, created_at, updated_at, disabled_at
		) VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11)
		RETURNING id, organization_id, name, provider, endpoint_url, event_types, enabled, secret_ref,
		          created_by_user_id, created_at, updated_at, disabled_at
	`, item.OrganizationID, item.Name, item.Provider, item.EndpointURL, string(raw), item.Enabled,
		item.SecretRef, item.CreatedByUserID, item.CreatedAt, item.UpdatedAt, item.DisabledAt,
	).Scan(
		&item.ID, &item.OrganizationID, &item.Name, &item.Provider, &item.EndpointURL, &out,
		&item.Enabled, &item.SecretRef, &item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt,
		&item.DisabledAt,
	)
	if err != nil {
		return model.SIEMDestination{}, err
	}
	if err := json.Unmarshal(out, &item.EventTypes); err != nil {
		return model.SIEMDestination{}, err
	}
	return item, nil
}

func (r *PostgresZeroTrustRepository) ListSIEMDestinations(organizationID int64) ([]model.SIEMDestination, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, name, provider, endpoint_url, event_types, enabled, secret_ref,
		       created_by_user_id, created_at, updated_at, disabled_at
		FROM zero_trust_siem_destinations
		WHERE organization_id=$1
		ORDER BY id
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.SIEMDestination, 0)
	for rows.Next() {
		var item model.SIEMDestination
		var raw []byte
		if err := rows.Scan(
			&item.ID, &item.OrganizationID, &item.Name, &item.Provider, &item.EndpointURL, &raw,
			&item.Enabled, &item.SecretRef, &item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt,
			&item.DisabledAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.EventTypes); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func zeroTrustDebugKey(orgID, id int64) string {
	return fmt.Sprintf("%d:%d", orgID, id)
}
