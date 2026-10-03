package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresOperationsRepository struct {
	db *sql.DB
}

func NewPostgresOperationsRepository(db *sql.DB) *PostgresOperationsRepository {
	return &PostgresOperationsRepository{db: db}
}

func (r *PostgresOperationsRepository) GetOperationsPolicy(organizationID int64) (model.OperationsPolicy, error) {
	var item model.OperationsPolicy
	err := r.db.QueryRow(`
		SELECT organization_id, monthly_budget_cents, budget_alert_threshold_percent,
			slo_target_basis_points, incident_escalation_minutes, support_tier,
			support_contact, updated_by_user_id, created_at, updated_at
		FROM organization_operations_policies
		WHERE organization_id = $1
	`, organizationID).Scan(
		&item.OrganizationID, &item.MonthlyBudgetCents, &item.BudgetAlertThresholdPercent,
		&item.SLOTargetBasisPoints, &item.IncidentEscalationMinutes, &item.SupportTier,
		&item.SupportContact, &item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.OperationsPolicy{}, ErrOperationsPolicyNotFound
	}
	return item, err
}

func (r *PostgresOperationsRepository) UpsertOperationsPolicy(policy model.OperationsPolicy) (model.OperationsPolicy, error) {
	var item model.OperationsPolicy
	err := r.db.QueryRow(`
		INSERT INTO organization_operations_policies (
			organization_id, monthly_budget_cents, budget_alert_threshold_percent,
			slo_target_basis_points, incident_escalation_minutes, support_tier,
			support_contact, updated_by_user_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (organization_id) DO UPDATE SET
			monthly_budget_cents = EXCLUDED.monthly_budget_cents,
			budget_alert_threshold_percent = EXCLUDED.budget_alert_threshold_percent,
			slo_target_basis_points = EXCLUDED.slo_target_basis_points,
			incident_escalation_minutes = EXCLUDED.incident_escalation_minutes,
			support_tier = EXCLUDED.support_tier,
			support_contact = EXCLUDED.support_contact,
			updated_by_user_id = EXCLUDED.updated_by_user_id,
			updated_at = EXCLUDED.updated_at
		RETURNING organization_id, monthly_budget_cents, budget_alert_threshold_percent,
			slo_target_basis_points, incident_escalation_minutes, support_tier,
			support_contact, updated_by_user_id, created_at, updated_at
	`,
		policy.OrganizationID, policy.MonthlyBudgetCents, policy.BudgetAlertThresholdPercent,
		policy.SLOTargetBasisPoints, policy.IncidentEscalationMinutes, policy.SupportTier,
		policy.SupportContact, policy.UpdatedByUserID, policy.CreatedAt, policy.UpdatedAt,
	).Scan(
		&item.OrganizationID, &item.MonthlyBudgetCents, &item.BudgetAlertThresholdPercent,
		&item.SLOTargetBasisPoints, &item.IncidentEscalationMinutes, &item.SupportTier,
		&item.SupportContact, &item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func (r *PostgresOperationsRepository) CreateCostAllocation(item model.CostAllocation) (model.CostAllocation, error) {
	metadata, err := json.Marshal(item.Metadata)
	if err != nil {
		return model.CostAllocation{}, err
	}
	var created model.CostAllocation
	var raw []byte
	err = r.db.QueryRow(`
		INSERT INTO organization_cost_allocations (
			organization_id, category, source, amount_cents, currency,
			period_start, period_end, metadata, created_by_user_id, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10)
		RETURNING id, organization_id, category, source, amount_cents, currency,
			period_start, period_end, metadata, created_by_user_id, created_at
	`,
		item.OrganizationID, item.Category, item.Source, item.AmountCents, item.Currency,
		item.PeriodStart, item.PeriodEnd, string(metadata), item.CreatedByUserID, item.CreatedAt,
	).Scan(
		&created.ID, &created.OrganizationID, &created.Category, &created.Source,
		&created.AmountCents, &created.Currency, &created.PeriodStart, &created.PeriodEnd,
		&raw, &created.CreatedByUserID, &created.CreatedAt,
	)
	if err != nil {
		return model.CostAllocation{}, err
	}
	created.Metadata = make(map[string]any)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &created.Metadata); err != nil {
			return model.CostAllocation{}, err
		}
	}
	return created, nil
}

func (r *PostgresOperationsRepository) ListCostAllocations(organizationID int64, periodStart, periodEnd time.Time) ([]model.CostAllocation, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, category, source, amount_cents, currency,
			period_start, period_end, metadata, created_by_user_id, created_at
		FROM organization_cost_allocations
		WHERE organization_id = $1
		  AND period_start < $3
		  AND period_end > $2
		ORDER BY period_start DESC, id DESC
	`, organizationID, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.CostAllocation, 0)
	for rows.Next() {
		var item model.CostAllocation
		var raw []byte
		if err := rows.Scan(
			&item.ID, &item.OrganizationID, &item.Category, &item.Source,
			&item.AmountCents, &item.Currency, &item.PeriodStart, &item.PeriodEnd,
			&raw, &item.CreatedByUserID, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.Metadata = make(map[string]any)
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &item.Metadata); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresOperationsRepository) UpsertOperationalAlert(item model.OperationalAlert) (model.OperationalAlert, error) {
	var created model.OperationalAlert
	err := r.db.QueryRow(`
		INSERT INTO organization_operational_alerts (
			organization_id, fingerprint, type, metric, status, message,
			current_value, threshold_value, detected_at, acknowledged_at,
			acknowledged_by_user_id, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (organization_id, fingerprint) DO UPDATE SET
			type = EXCLUDED.type,
			metric = EXCLUDED.metric,
			message = EXCLUDED.message,
			current_value = EXCLUDED.current_value,
			threshold_value = EXCLUDED.threshold_value,
			detected_at = EXCLUDED.detected_at,
			updated_at = EXCLUDED.updated_at
		RETURNING id, organization_id, fingerprint, type, metric, status, message,
			current_value, threshold_value, detected_at, acknowledged_at,
			acknowledged_by_user_id, updated_at
	`,
		item.OrganizationID, item.Fingerprint, item.Type, item.Metric, item.Status, item.Message,
		item.CurrentValue, item.ThresholdValue, item.DetectedAt, item.AcknowledgedAt,
		item.AcknowledgedByUserID, item.UpdatedAt,
	).Scan(
		&created.ID, &created.OrganizationID, &created.Fingerprint, &created.Type,
		&created.Metric, &created.Status, &created.Message, &created.CurrentValue,
		&created.ThresholdValue, &created.DetectedAt, &created.AcknowledgedAt,
		&created.AcknowledgedByUserID, &created.UpdatedAt,
	)
	return created, err
}

func (r *PostgresOperationsRepository) ListOperationalAlerts(organizationID int64, status string, limit int) ([]model.OperationalAlert, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, fingerprint, type, metric, status, message,
			current_value, threshold_value, detected_at, acknowledged_at,
			acknowledged_by_user_id, updated_at
		FROM organization_operational_alerts
		WHERE organization_id = $1
		  AND ($2 = '' OR status = $2)
		ORDER BY detected_at DESC, id DESC
		LIMIT $3
	`, organizationID, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.OperationalAlert, 0)
	for rows.Next() {
		var item model.OperationalAlert
		if err := rows.Scan(
			&item.ID, &item.OrganizationID, &item.Fingerprint, &item.Type,
			&item.Metric, &item.Status, &item.Message, &item.CurrentValue,
			&item.ThresholdValue, &item.DetectedAt, &item.AcknowledgedAt,
			&item.AcknowledgedByUserID, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresOperationsRepository) AcknowledgeOperationalAlert(organizationID, alertID, actorUserID int64, at time.Time) (model.OperationalAlert, error) {
	var item model.OperationalAlert
	err := r.db.QueryRow(`
		UPDATE organization_operational_alerts
		SET status = 'acknowledged', acknowledged_at = $4,
			acknowledged_by_user_id = $3, updated_at = $4
		WHERE organization_id = $1 AND id = $2
		RETURNING id, organization_id, fingerprint, type, metric, status, message,
			current_value, threshold_value, detected_at, acknowledged_at,
			acknowledged_by_user_id, updated_at
	`, organizationID, alertID, actorUserID, at).Scan(
		&item.ID, &item.OrganizationID, &item.Fingerprint, &item.Type,
		&item.Metric, &item.Status, &item.Message, &item.CurrentValue,
		&item.ThresholdValue, &item.DetectedAt, &item.AcknowledgedAt,
		&item.AcknowledgedByUserID, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.OperationalAlert{}, ErrOperationalAlertNotFound
	}
	return item, err
}

func (r *PostgresOperationsRepository) CreateMaintenanceWindow(item model.MaintenanceWindow) (model.MaintenanceWindow, error) {
	var created model.MaintenanceWindow
	err := r.db.QueryRow(`
		INSERT INTO organization_maintenance_windows (
			organization_id, title, description, status, starts_at, ends_at,
			created_by_user_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, organization_id, title, description, status, starts_at, ends_at,
			created_by_user_id, created_at, updated_at
	`,
		item.OrganizationID, item.Title, item.Description, item.Status,
		item.StartsAt, item.EndsAt, item.CreatedByUserID, item.CreatedAt, item.UpdatedAt,
	).Scan(
		&created.ID, &created.OrganizationID, &created.Title, &created.Description,
		&created.Status, &created.StartsAt, &created.EndsAt, &created.CreatedByUserID,
		&created.CreatedAt, &created.UpdatedAt,
	)
	return created, err
}

func (r *PostgresOperationsRepository) ListMaintenanceWindows(organizationID int64, from, to time.Time) ([]model.MaintenanceWindow, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, title, description, status, starts_at, ends_at,
			created_by_user_id, created_at, updated_at
		FROM organization_maintenance_windows
		WHERE organization_id = $1
		  AND starts_at < $3
		  AND ends_at > $2
		ORDER BY starts_at, id
	`, organizationID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.MaintenanceWindow, 0)
	for rows.Next() {
		var item model.MaintenanceWindow
		if err := rows.Scan(
			&item.ID, &item.OrganizationID, &item.Title, &item.Description,
			&item.Status, &item.StartsAt, &item.EndsAt, &item.CreatedByUserID,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresOperationsRepository) UpdateMaintenanceWindowStatus(organizationID, windowID int64, status string, at time.Time) (model.MaintenanceWindow, error) {
	var item model.MaintenanceWindow
	err := r.db.QueryRow(`
		UPDATE organization_maintenance_windows
		SET status = $3, updated_at = $4
		WHERE organization_id = $1 AND id = $2
		RETURNING id, organization_id, title, description, status, starts_at, ends_at,
			created_by_user_id, created_at, updated_at
	`, organizationID, windowID, status, at).Scan(
		&item.ID, &item.OrganizationID, &item.Title, &item.Description,
		&item.Status, &item.StartsAt, &item.EndsAt, &item.CreatedByUserID,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.MaintenanceWindow{}, ErrMaintenanceWindowNotFound
	}
	return item, err
}

func (r *PostgresOperationsRepository) CreateOperationalIncident(item model.OperationalIncident) (model.OperationalIncident, error) {
	var created model.OperationalIncident
	err := r.db.QueryRow(`
		INSERT INTO organization_operational_incidents (
			organization_id, title, severity, status, summary, started_at,
			resolved_at, created_by_user_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, organization_id, title, severity, status, summary, started_at,
			resolved_at, created_by_user_id, created_at, updated_at
	`,
		item.OrganizationID, item.Title, item.Severity, item.Status, item.Summary,
		item.StartedAt, item.ResolvedAt, item.CreatedByUserID, item.CreatedAt, item.UpdatedAt,
	).Scan(
		&created.ID, &created.OrganizationID, &created.Title, &created.Severity,
		&created.Status, &created.Summary, &created.StartedAt, &created.ResolvedAt,
		&created.CreatedByUserID, &created.CreatedAt, &created.UpdatedAt,
	)
	return created, err
}

func (r *PostgresOperationsRepository) ListOperationalIncidents(organizationID int64, status string, from, to time.Time) ([]model.OperationalIncident, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, title, severity, status, summary, started_at,
			resolved_at, created_by_user_id, created_at, updated_at
		FROM organization_operational_incidents
		WHERE organization_id = $1
		  AND ($2 = '' OR status = $2)
		  AND started_at < $4
		  AND COALESCE(resolved_at, $4) > $3
		ORDER BY started_at DESC, id DESC
	`, organizationID, status, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.OperationalIncident, 0)
	for rows.Next() {
		var item model.OperationalIncident
		if err := rows.Scan(
			&item.ID, &item.OrganizationID, &item.Title, &item.Severity,
			&item.Status, &item.Summary, &item.StartedAt, &item.ResolvedAt,
			&item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresOperationsRepository) UpdateOperationalIncident(organizationID, incidentID int64, status, summary string, resolvedAt *time.Time, at time.Time) (model.OperationalIncident, error) {
	var item model.OperationalIncident
	err := r.db.QueryRow(`
		UPDATE organization_operational_incidents
		SET status = $3, summary = $4, resolved_at = $5, updated_at = $6
		WHERE organization_id = $1 AND id = $2
		RETURNING id, organization_id, title, severity, status, summary, started_at,
			resolved_at, created_by_user_id, created_at, updated_at
	`, organizationID, incidentID, status, summary, resolvedAt, at).Scan(
		&item.ID, &item.OrganizationID, &item.Title, &item.Severity,
		&item.Status, &item.Summary, &item.StartedAt, &item.ResolvedAt,
		&item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.OperationalIncident{}, ErrOperationalIncidentNotFound
	}
	return item, err
}
