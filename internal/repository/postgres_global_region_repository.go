package repository

import (
	"database/sql"
	"encoding/json"
	"errors"

	"go-simple-task-api/internal/model"
)

type PostgresGlobalRegionRepository struct{ db *sql.DB }

func NewPostgresGlobalRegionRepository(db *sql.DB) *PostgresGlobalRegionRepository {
	return &PostgresGlobalRegionRepository{db: db}
}

func (r *PostgresGlobalRegionRepository) GetPolicy(organizationID int64) (model.OrganizationRegionPolicy, error) {
	var item model.OrganizationRegionPolicy
	var allowedRaw, failoverRaw []byte
	err := r.db.QueryRow(`
		SELECT organization_id,home_region,allowed_regions,failover_regions,data_residency_enforced,
		       cross_region_approval_required,rpo_seconds,rto_seconds,updated_by_user_id,created_at,updated_at
		FROM organization_region_policies WHERE organization_id=$1
	`, organizationID).Scan(
		&item.OrganizationID, &item.HomeRegion, &allowedRaw, &failoverRaw, &item.DataResidencyEnforced,
		&item.CrossRegionApprovalRequired, &item.RPOSeconds, &item.RTOSeconds, &item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.OrganizationRegionPolicy{}, ErrRegionPolicyNotFound
	}
	if err != nil {
		return model.OrganizationRegionPolicy{}, err
	}
	if err := json.Unmarshal(allowedRaw, &item.AllowedRegions); err != nil {
		return model.OrganizationRegionPolicy{}, err
	}
	if err := json.Unmarshal(failoverRaw, &item.FailoverRegions); err != nil {
		return model.OrganizationRegionPolicy{}, err
	}
	return item, nil
}

func (r *PostgresGlobalRegionRepository) UpsertPolicy(item model.OrganizationRegionPolicy) (model.OrganizationRegionPolicy, error) {
	allowedRaw, err := json.Marshal(item.AllowedRegions)
	if err != nil {
		return model.OrganizationRegionPolicy{}, err
	}
	failoverRaw, err := json.Marshal(item.FailoverRegions)
	if err != nil {
		return model.OrganizationRegionPolicy{}, err
	}
	var allowedOut, failoverOut []byte
	err = r.db.QueryRow(`
		INSERT INTO organization_region_policies (
			organization_id,home_region,allowed_regions,failover_regions,data_residency_enforced,
			cross_region_approval_required,rpo_seconds,rto_seconds,updated_by_user_id,created_at,updated_at
		) VALUES ($1,$2,$3::jsonb,$4::jsonb,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (organization_id) DO UPDATE SET
			home_region=EXCLUDED.home_region,allowed_regions=EXCLUDED.allowed_regions,
			failover_regions=EXCLUDED.failover_regions,data_residency_enforced=EXCLUDED.data_residency_enforced,
			cross_region_approval_required=EXCLUDED.cross_region_approval_required,
			rpo_seconds=EXCLUDED.rpo_seconds,rto_seconds=EXCLUDED.rto_seconds,
			updated_by_user_id=EXCLUDED.updated_by_user_id,updated_at=EXCLUDED.updated_at
		RETURNING organization_id,home_region,allowed_regions,failover_regions,data_residency_enforced,
		          cross_region_approval_required,rpo_seconds,rto_seconds,updated_by_user_id,created_at,updated_at
	`, item.OrganizationID, item.HomeRegion, string(allowedRaw), string(failoverRaw), item.DataResidencyEnforced,
		item.CrossRegionApprovalRequired, item.RPOSeconds, item.RTOSeconds, item.UpdatedByUserID, item.CreatedAt, item.UpdatedAt,
	).Scan(&item.OrganizationID, &item.HomeRegion, &allowedOut, &failoverOut, &item.DataResidencyEnforced,
		&item.CrossRegionApprovalRequired, &item.RPOSeconds, &item.RTOSeconds, &item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return model.OrganizationRegionPolicy{}, err
	}
	if err := json.Unmarshal(allowedOut, &item.AllowedRegions); err != nil {
		return model.OrganizationRegionPolicy{}, err
	}
	if err := json.Unmarshal(failoverOut, &item.FailoverRegions); err != nil {
		return model.OrganizationRegionPolicy{}, err
	}
	return item, nil
}

func (r *PostgresGlobalRegionRepository) UpsertPlacement(item model.RegionalPlacement) (model.RegionalPlacement, error) {
	raw, err := json.Marshal(item.ReplicaRegions)
	if err != nil {
		return model.RegionalPlacement{}, err
	}
	var out []byte
	err = r.db.QueryRow(`
		INSERT INTO regional_placements (
			organization_id,resource_type,resource_id,primary_region,replica_regions,status,
			last_replicated_at,last_verified_at,updated_by_user_id,created_at,updated_at
		) VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (organization_id,resource_type,resource_id) DO UPDATE SET
			primary_region=EXCLUDED.primary_region,replica_regions=EXCLUDED.replica_regions,status=EXCLUDED.status,
			last_replicated_at=EXCLUDED.last_replicated_at,last_verified_at=EXCLUDED.last_verified_at,
			updated_by_user_id=EXCLUDED.updated_by_user_id,updated_at=EXCLUDED.updated_at
		RETURNING id,organization_id,resource_type,resource_id,primary_region,replica_regions,status,
		          last_replicated_at,last_verified_at,updated_by_user_id,created_at,updated_at
	`, item.OrganizationID, item.ResourceType, item.ResourceID, item.PrimaryRegion, string(raw), item.Status,
		item.LastReplicatedAt, item.LastVerifiedAt, item.UpdatedByUserID, item.CreatedAt, item.UpdatedAt,
	).Scan(&item.ID, &item.OrganizationID, &item.ResourceType, &item.ResourceID, &item.PrimaryRegion, &out, &item.Status,
		&item.LastReplicatedAt, &item.LastVerifiedAt, &item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return model.RegionalPlacement{}, err
	}
	if err := json.Unmarshal(out, &item.ReplicaRegions); err != nil {
		return model.RegionalPlacement{}, err
	}
	return item, nil
}

func (r *PostgresGlobalRegionRepository) ListPlacements(organizationID int64) ([]model.RegionalPlacement, error) {
	rows, err := r.db.Query(`
		SELECT id,organization_id,resource_type,resource_id,primary_region,replica_regions,status,
		       last_replicated_at,last_verified_at,updated_by_user_id,created_at,updated_at
		FROM regional_placements WHERE organization_id=$1 ORDER BY id
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.RegionalPlacement, 0)
	for rows.Next() {
		var item model.RegionalPlacement
		var raw []byte
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.ResourceType, &item.ResourceID, &item.PrimaryRegion, &raw, &item.Status,
			&item.LastReplicatedAt, &item.LastVerifiedAt, &item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.ReplicaRegions); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresGlobalRegionRepository) CreateMigration(item model.RegionMigration) (model.RegionMigration, error) {
	raw, err := json.Marshal(item.Checkpoint)
	if err != nil {
		return model.RegionMigration{}, err
	}
	var out []byte
	err = r.db.QueryRow(`
		INSERT INTO region_migrations (
			organization_id,scope,resource_type,resource_id,source_region,target_region,status,reason,checkpoint,
			requested_by_user_id,requested_at,decided_by_user_id,decision_comment,decided_at,completed_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12,$13,$14,$15,$16)
		RETURNING id,organization_id,scope,resource_type,resource_id,source_region,target_region,status,reason,checkpoint,
		          requested_by_user_id,requested_at,decided_by_user_id,decision_comment,decided_at,completed_at,updated_at
	`, item.OrganizationID, item.Scope, item.ResourceType, item.ResourceID, item.SourceRegion, item.TargetRegion, item.Status, item.Reason, string(raw),
		item.RequestedByUserID, item.RequestedAt, item.DecidedByUserID, item.DecisionComment, item.DecidedAt, item.CompletedAt, item.UpdatedAt,
	).Scan(&item.ID, &item.OrganizationID, &item.Scope, &item.ResourceType, &item.ResourceID, &item.SourceRegion, &item.TargetRegion, &item.Status, &item.Reason, &out,
		&item.RequestedByUserID, &item.RequestedAt, &item.DecidedByUserID, &item.DecisionComment, &item.DecidedAt, &item.CompletedAt, &item.UpdatedAt)
	if err != nil {
		return model.RegionMigration{}, err
	}
	if err := json.Unmarshal(out, &item.Checkpoint); err != nil {
		return model.RegionMigration{}, err
	}
	return item, nil
}

func (r *PostgresGlobalRegionRepository) GetMigration(organizationID, migrationID int64) (model.RegionMigration, error) {
	item, err := scanRegionMigration(r.db.QueryRow(`
		SELECT id,organization_id,scope,resource_type,resource_id,source_region,target_region,status,reason,checkpoint,
		       requested_by_user_id,requested_at,decided_by_user_id,decision_comment,decided_at,completed_at,updated_at
		FROM region_migrations WHERE organization_id=$1 AND id=$2
	`, organizationID, migrationID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.RegionMigration{}, ErrRegionMigrationNotFound
	}
	return item, err
}

func (r *PostgresGlobalRegionRepository) ListMigrations(organizationID int64, limit int) ([]model.RegionMigration, error) {
	rows, err := r.db.Query(`
		SELECT id,organization_id,scope,resource_type,resource_id,source_region,target_region,status,reason,checkpoint,
		       requested_by_user_id,requested_at,decided_by_user_id,decision_comment,decided_at,completed_at,updated_at
		FROM region_migrations WHERE organization_id=$1 ORDER BY id DESC LIMIT $2
	`, organizationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.RegionMigration, 0)
	for rows.Next() {
		item, err := scanRegionMigration(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresGlobalRegionRepository) UpdateMigration(item model.RegionMigration) (model.RegionMigration, error) {
	raw, err := json.Marshal(item.Checkpoint)
	if err != nil {
		return model.RegionMigration{}, err
	}
	var out []byte
	err = r.db.QueryRow(`
		UPDATE region_migrations SET status=$3,checkpoint=$4::jsonb,decided_by_user_id=$5,decision_comment=$6,
		       decided_at=$7,completed_at=$8,updated_at=$9
		WHERE organization_id=$1 AND id=$2
		RETURNING id,organization_id,scope,resource_type,resource_id,source_region,target_region,status,reason,checkpoint,
		          requested_by_user_id,requested_at,decided_by_user_id,decision_comment,decided_at,completed_at,updated_at
	`, item.OrganizationID, item.ID, item.Status, string(raw), item.DecidedByUserID, item.DecisionComment, item.DecidedAt, item.CompletedAt, item.UpdatedAt,
	).Scan(&item.ID, &item.OrganizationID, &item.Scope, &item.ResourceType, &item.ResourceID, &item.SourceRegion, &item.TargetRegion, &item.Status, &item.Reason, &out,
		&item.RequestedByUserID, &item.RequestedAt, &item.DecidedByUserID, &item.DecisionComment, &item.DecidedAt, &item.CompletedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.RegionMigration{}, ErrRegionMigrationNotFound
	}
	if err != nil {
		return model.RegionMigration{}, err
	}
	if err := json.Unmarshal(out, &item.Checkpoint); err != nil {
		return model.RegionMigration{}, err
	}
	return item, nil
}

type regionScanner interface{ Scan(dest ...any) error }

func scanRegionMigration(s regionScanner) (model.RegionMigration, error) {
	var item model.RegionMigration
	var raw []byte
	err := s.Scan(&item.ID, &item.OrganizationID, &item.Scope, &item.ResourceType, &item.ResourceID, &item.SourceRegion, &item.TargetRegion, &item.Status, &item.Reason, &raw,
		&item.RequestedByUserID, &item.RequestedAt, &item.DecidedByUserID, &item.DecisionComment, &item.DecidedAt, &item.CompletedAt, &item.UpdatedAt)
	if err != nil {
		return model.RegionMigration{}, err
	}
	if err := json.Unmarshal(raw, &item.Checkpoint); err != nil {
		return model.RegionMigration{}, err
	}
	return item, nil
}

func (r *PostgresGlobalRegionRepository) CreateTransfer(item model.CrossRegionTransfer) (model.CrossRegionTransfer, error) {
	err := r.db.QueryRow(`
		INSERT INTO cross_region_transfers (
			organization_id,resource_type,resource_id,classification,source_region,target_region,status,reason,
			requested_by_user_id,requested_at,decided_by_user_id,decision_comment,decided_at,completed_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id,organization_id,resource_type,resource_id,classification,source_region,target_region,status,reason,
		          requested_by_user_id,requested_at,decided_by_user_id,decision_comment,decided_at,completed_at,updated_at
	`, item.OrganizationID, item.ResourceType, item.ResourceID, item.Classification, item.SourceRegion, item.TargetRegion, item.Status, item.Reason,
		item.RequestedByUserID, item.RequestedAt, item.DecidedByUserID, item.DecisionComment, item.DecidedAt, item.CompletedAt, item.UpdatedAt,
	).Scan(&item.ID, &item.OrganizationID, &item.ResourceType, &item.ResourceID, &item.Classification, &item.SourceRegion, &item.TargetRegion, &item.Status, &item.Reason,
		&item.RequestedByUserID, &item.RequestedAt, &item.DecidedByUserID, &item.DecisionComment, &item.DecidedAt, &item.CompletedAt, &item.UpdatedAt)
	return item, err
}

func (r *PostgresGlobalRegionRepository) GetTransfer(organizationID, transferID int64) (model.CrossRegionTransfer, error) {
	item, err := scanTransfer(r.db.QueryRow(`
		SELECT id,organization_id,resource_type,resource_id,classification,source_region,target_region,status,reason,
		       requested_by_user_id,requested_at,decided_by_user_id,decision_comment,decided_at,completed_at,updated_at
		FROM cross_region_transfers WHERE organization_id=$1 AND id=$2
	`, organizationID, transferID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.CrossRegionTransfer{}, ErrCrossRegionTransferNotFound
	}
	return item, err
}
func (r *PostgresGlobalRegionRepository) ListTransfers(organizationID int64, limit int) ([]model.CrossRegionTransfer, error) {
	rows, err := r.db.Query(`
		SELECT id,organization_id,resource_type,resource_id,classification,source_region,target_region,status,reason,
		       requested_by_user_id,requested_at,decided_by_user_id,decision_comment,decided_at,completed_at,updated_at
		FROM cross_region_transfers WHERE organization_id=$1 ORDER BY id DESC LIMIT $2
	`, organizationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.CrossRegionTransfer, 0)
	for rows.Next() {
		item, err := scanTransfer(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (r *PostgresGlobalRegionRepository) UpdateTransfer(item model.CrossRegionTransfer) (model.CrossRegionTransfer, error) {
	err := r.db.QueryRow(`
		UPDATE cross_region_transfers SET status=$3,decided_by_user_id=$4,decision_comment=$5,decided_at=$6,completed_at=$7,updated_at=$8
		WHERE organization_id=$1 AND id=$2
		RETURNING id,organization_id,resource_type,resource_id,classification,source_region,target_region,status,reason,
		          requested_by_user_id,requested_at,decided_by_user_id,decision_comment,decided_at,completed_at,updated_at
	`, item.OrganizationID, item.ID, item.Status, item.DecidedByUserID, item.DecisionComment, item.DecidedAt, item.CompletedAt, item.UpdatedAt,
	).Scan(&item.ID, &item.OrganizationID, &item.ResourceType, &item.ResourceID, &item.Classification, &item.SourceRegion, &item.TargetRegion, &item.Status, &item.Reason,
		&item.RequestedByUserID, &item.RequestedAt, &item.DecidedByUserID, &item.DecisionComment, &item.DecidedAt, &item.CompletedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.CrossRegionTransfer{}, ErrCrossRegionTransferNotFound
	}
	return item, err
}
func scanTransfer(s regionScanner) (model.CrossRegionTransfer, error) {
	var item model.CrossRegionTransfer
	err := s.Scan(&item.ID, &item.OrganizationID, &item.ResourceType, &item.ResourceID, &item.Classification, &item.SourceRegion, &item.TargetRegion, &item.Status, &item.Reason,
		&item.RequestedByUserID, &item.RequestedAt, &item.DecidedByUserID, &item.DecisionComment, &item.DecidedAt, &item.CompletedAt, &item.UpdatedAt)
	return item, err
}

func (r *PostgresGlobalRegionRepository) CreateFailoverExercise(item model.FailoverExercise) (model.FailoverExercise, error) {
	err := r.db.QueryRow(`
		INSERT INTO region_failover_exercises (
			organization_id,source_region,target_region,status,notes,achieved_rpo_seconds,achieved_rto_seconds,
			meets_rpo,meets_rto,created_by_user_id,created_at,completed_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id,organization_id,source_region,target_region,status,notes,achieved_rpo_seconds,achieved_rto_seconds,
		          meets_rpo,meets_rto,created_by_user_id,created_at,completed_at
	`, item.OrganizationID, item.SourceRegion, item.TargetRegion, item.Status, item.Notes, item.AchievedRPOSeconds, item.AchievedRTOSeconds,
		item.MeetsRPO, item.MeetsRTO, item.CreatedByUserID, item.CreatedAt, item.CompletedAt,
	).Scan(&item.ID, &item.OrganizationID, &item.SourceRegion, &item.TargetRegion, &item.Status, &item.Notes, &item.AchievedRPOSeconds, &item.AchievedRTOSeconds,
		&item.MeetsRPO, &item.MeetsRTO, &item.CreatedByUserID, &item.CreatedAt, &item.CompletedAt)
	return item, err
}
func (r *PostgresGlobalRegionRepository) GetFailoverExercise(organizationID, exerciseID int64) (model.FailoverExercise, error) {
	item, err := scanExercise(r.db.QueryRow(`
		SELECT id,organization_id,source_region,target_region,status,notes,achieved_rpo_seconds,achieved_rto_seconds,
		       meets_rpo,meets_rto,created_by_user_id,created_at,completed_at
		FROM region_failover_exercises WHERE organization_id=$1 AND id=$2
	`, organizationID, exerciseID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.FailoverExercise{}, ErrFailoverExerciseNotFound
	}
	return item, err
}
func (r *PostgresGlobalRegionRepository) ListFailoverExercises(organizationID int64, limit int) ([]model.FailoverExercise, error) {
	rows, err := r.db.Query(`
		SELECT id,organization_id,source_region,target_region,status,notes,achieved_rpo_seconds,achieved_rto_seconds,
		       meets_rpo,meets_rto,created_by_user_id,created_at,completed_at
		FROM region_failover_exercises WHERE organization_id=$1 ORDER BY id DESC LIMIT $2
	`, organizationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.FailoverExercise, 0)
	for rows.Next() {
		item, err := scanExercise(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (r *PostgresGlobalRegionRepository) UpdateFailoverExercise(item model.FailoverExercise) (model.FailoverExercise, error) {
	err := r.db.QueryRow(`
		UPDATE region_failover_exercises SET status=$3,notes=$4,achieved_rpo_seconds=$5,achieved_rto_seconds=$6,
		       meets_rpo=$7,meets_rto=$8,completed_at=$9
		WHERE organization_id=$1 AND id=$2
		RETURNING id,organization_id,source_region,target_region,status,notes,achieved_rpo_seconds,achieved_rto_seconds,
		          meets_rpo,meets_rto,created_by_user_id,created_at,completed_at
	`, item.OrganizationID, item.ID, item.Status, item.Notes, item.AchievedRPOSeconds, item.AchievedRTOSeconds, item.MeetsRPO, item.MeetsRTO, item.CompletedAt,
	).Scan(&item.ID, &item.OrganizationID, &item.SourceRegion, &item.TargetRegion, &item.Status, &item.Notes, &item.AchievedRPOSeconds, &item.AchievedRTOSeconds,
		&item.MeetsRPO, &item.MeetsRTO, &item.CreatedByUserID, &item.CreatedAt, &item.CompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.FailoverExercise{}, ErrFailoverExerciseNotFound
	}
	return item, err
}
func scanExercise(s regionScanner) (model.FailoverExercise, error) {
	var item model.FailoverExercise
	err := s.Scan(&item.ID, &item.OrganizationID, &item.SourceRegion, &item.TargetRegion, &item.Status, &item.Notes, &item.AchievedRPOSeconds, &item.AchievedRTOSeconds,
		&item.MeetsRPO, &item.MeetsRTO, &item.CreatedByUserID, &item.CreatedAt, &item.CompletedAt)
	return item, err
}
