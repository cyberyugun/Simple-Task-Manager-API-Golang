package repository

import (
	"database/sql"
	"encoding/json"
	"errors"

	"go-simple-task-api/internal/model"
)

type PostgresAutomationRepository struct {
	db *sql.DB
}

func NewPostgresAutomationRepository(db *sql.DB) *PostgresAutomationRepository {
	return &PostgresAutomationRepository{db: db}
}

func (r *PostgresAutomationRepository) CreateAutomationPolicy(policy model.AutomationPolicy) (model.AutomationPolicy, error) {
	raw, err := json.Marshal(policy.ActionConfig)
	if err != nil {
		return model.AutomationPolicy{}, err
	}
	var item model.AutomationPolicy
	var actionRaw []byte
	err = r.db.QueryRow(`
		INSERT INTO automation_policies (
			organization_id, name, enabled, trigger_type, trigger_key, comparator,
			threshold, action_type, action_config, approval_mode, cooldown_minutes,
			created_by_user_id, updated_by_user_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12,$13,$14,$15)
		RETURNING id, organization_id, name, enabled, trigger_type, trigger_key, comparator,
			threshold, action_type, action_config, approval_mode, cooldown_minutes,
			created_by_user_id, updated_by_user_id, created_at, updated_at
	`,
		policy.OrganizationID, policy.Name, policy.Enabled, policy.TriggerType, policy.TriggerKey,
		policy.Comparator, policy.Threshold, policy.ActionType, string(raw), policy.ApprovalMode,
		policy.CooldownMinutes, policy.CreatedByUserID, policy.UpdatedByUserID,
		policy.CreatedAt, policy.UpdatedAt,
	).Scan(
		&item.ID, &item.OrganizationID, &item.Name, &item.Enabled, &item.TriggerType,
		&item.TriggerKey, &item.Comparator, &item.Threshold, &item.ActionType, &actionRaw,
		&item.ApprovalMode, &item.CooldownMinutes, &item.CreatedByUserID,
		&item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.AutomationPolicy{}, err
	}
	if err := json.Unmarshal(actionRaw, &item.ActionConfig); err != nil {
		return model.AutomationPolicy{}, err
	}
	return item, nil
}

func (r *PostgresAutomationRepository) UpdateAutomationPolicy(policy model.AutomationPolicy) (model.AutomationPolicy, error) {
	raw, err := json.Marshal(policy.ActionConfig)
	if err != nil {
		return model.AutomationPolicy{}, err
	}
	var item model.AutomationPolicy
	var actionRaw []byte
	err = r.db.QueryRow(`
		UPDATE automation_policies
		SET name = $3, enabled = $4, trigger_type = $5, trigger_key = $6,
			comparator = $7, threshold = $8, action_type = $9, action_config = $10::jsonb,
			approval_mode = $11, cooldown_minutes = $12, updated_by_user_id = $13,
			updated_at = $14
		WHERE organization_id = $1 AND id = $2
		RETURNING id, organization_id, name, enabled, trigger_type, trigger_key, comparator,
			threshold, action_type, action_config, approval_mode, cooldown_minutes,
			created_by_user_id, updated_by_user_id, created_at, updated_at
	`,
		policy.OrganizationID, policy.ID, policy.Name, policy.Enabled, policy.TriggerType,
		policy.TriggerKey, policy.Comparator, policy.Threshold, policy.ActionType,
		string(raw), policy.ApprovalMode, policy.CooldownMinutes, policy.UpdatedByUserID,
		policy.UpdatedAt,
	).Scan(
		&item.ID, &item.OrganizationID, &item.Name, &item.Enabled, &item.TriggerType,
		&item.TriggerKey, &item.Comparator, &item.Threshold, &item.ActionType, &actionRaw,
		&item.ApprovalMode, &item.CooldownMinutes, &item.CreatedByUserID,
		&item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AutomationPolicy{}, ErrAutomationPolicyNotFound
	}
	if err != nil {
		return model.AutomationPolicy{}, err
	}
	if err := json.Unmarshal(actionRaw, &item.ActionConfig); err != nil {
		return model.AutomationPolicy{}, err
	}
	return item, nil
}

func (r *PostgresAutomationRepository) GetAutomationPolicy(organizationID, policyID int64) (model.AutomationPolicy, error) {
	var item model.AutomationPolicy
	var actionRaw []byte
	err := r.db.QueryRow(`
		SELECT id, organization_id, name, enabled, trigger_type, trigger_key, comparator,
			threshold, action_type, action_config, approval_mode, cooldown_minutes,
			created_by_user_id, updated_by_user_id, created_at, updated_at
		FROM automation_policies
		WHERE organization_id = $1 AND id = $2
	`, organizationID, policyID).Scan(
		&item.ID, &item.OrganizationID, &item.Name, &item.Enabled, &item.TriggerType,
		&item.TriggerKey, &item.Comparator, &item.Threshold, &item.ActionType, &actionRaw,
		&item.ApprovalMode, &item.CooldownMinutes, &item.CreatedByUserID,
		&item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AutomationPolicy{}, ErrAutomationPolicyNotFound
	}
	if err != nil {
		return model.AutomationPolicy{}, err
	}
	if err := json.Unmarshal(actionRaw, &item.ActionConfig); err != nil {
		return model.AutomationPolicy{}, err
	}
	return item, nil
}

func (r *PostgresAutomationRepository) ListAutomationPolicies(organizationID int64) ([]model.AutomationPolicy, error) {
	return r.listPolicies(`
		SELECT id, organization_id, name, enabled, trigger_type, trigger_key, comparator,
			threshold, action_type, action_config, approval_mode, cooldown_minutes,
			created_by_user_id, updated_by_user_id, created_at, updated_at
		FROM automation_policies
		WHERE organization_id = $1
		ORDER BY id
	`, organizationID)
}

func (r *PostgresAutomationRepository) ListEnabledAutomationPolicies() ([]model.AutomationPolicy, error) {
	return r.listPolicies(`
		SELECT id, organization_id, name, enabled, trigger_type, trigger_key, comparator,
			threshold, action_type, action_config, approval_mode, cooldown_minutes,
			created_by_user_id, updated_by_user_id, created_at, updated_at
		FROM automation_policies
		WHERE enabled = TRUE
		ORDER BY organization_id, id
	`)
}

func (r *PostgresAutomationRepository) listPolicies(query string, args ...any) ([]model.AutomationPolicy, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AutomationPolicy, 0)
	for rows.Next() {
		var item model.AutomationPolicy
		var actionRaw []byte
		if err := rows.Scan(
			&item.ID, &item.OrganizationID, &item.Name, &item.Enabled, &item.TriggerType,
			&item.TriggerKey, &item.Comparator, &item.Threshold, &item.ActionType, &actionRaw,
			&item.ApprovalMode, &item.CooldownMinutes, &item.CreatedByUserID,
			&item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(actionRaw, &item.ActionConfig); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresAutomationRepository) CreateAutomationExecution(execution model.AutomationExecution) (model.AutomationExecution, error) {
	triggerRaw, err := json.Marshal(execution.TriggerSnapshot)
	if err != nil {
		return model.AutomationExecution{}, err
	}
	resultRaw, err := json.Marshal(execution.ActionResult)
	if err != nil {
		return model.AutomationExecution{}, err
	}
	var item model.AutomationExecution
	var triggerOut, resultOut []byte
	err = r.db.QueryRow(`
		INSERT INTO automation_executions (
			organization_id, policy_id, dedupe_key, status, trigger_snapshot,
			action_result, error_message, requested_at, approved_at, approved_by_user_id,
			rejected_at, rejected_by_user_id, executed_at, completed_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		RETURNING id, organization_id, policy_id, dedupe_key, status, trigger_snapshot,
			action_result, error_message, requested_at, approved_at, approved_by_user_id,
			rejected_at, rejected_by_user_id, executed_at, completed_at, created_at, updated_at
	`,
		execution.OrganizationID, execution.PolicyID, execution.DedupeKey, execution.Status,
		string(triggerRaw), string(resultRaw), execution.ErrorMessage, execution.RequestedAt,
		execution.ApprovedAt, execution.ApprovedByUserID, execution.RejectedAt,
		execution.RejectedByUserID, execution.ExecutedAt, execution.CompletedAt,
		execution.CreatedAt, execution.UpdatedAt,
	).Scan(
		&item.ID, &item.OrganizationID, &item.PolicyID, &item.DedupeKey, &item.Status,
		&triggerOut, &resultOut, &item.ErrorMessage, &item.RequestedAt, &item.ApprovedAt,
		&item.ApprovedByUserID, &item.RejectedAt, &item.RejectedByUserID, &item.ExecutedAt,
		&item.CompletedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return model.AutomationExecution{}, ErrAutomationExecutionExists
		}
		return model.AutomationExecution{}, err
	}
	if err := decodeAutomationExecutionJSON(&item, triggerOut, resultOut); err != nil {
		return model.AutomationExecution{}, err
	}
	return item, nil
}

func (r *PostgresAutomationRepository) GetAutomationExecution(organizationID, executionID int64) (model.AutomationExecution, error) {
	var item model.AutomationExecution
	var triggerRaw, resultRaw []byte
	err := r.db.QueryRow(`
		SELECT id, organization_id, policy_id, dedupe_key, status, trigger_snapshot,
			action_result, error_message, requested_at, approved_at, approved_by_user_id,
			rejected_at, rejected_by_user_id, executed_at, completed_at, created_at, updated_at
		FROM automation_executions
		WHERE organization_id = $1 AND id = $2
	`, organizationID, executionID).Scan(
		&item.ID, &item.OrganizationID, &item.PolicyID, &item.DedupeKey, &item.Status,
		&triggerRaw, &resultRaw, &item.ErrorMessage, &item.RequestedAt, &item.ApprovedAt,
		&item.ApprovedByUserID, &item.RejectedAt, &item.RejectedByUserID, &item.ExecutedAt,
		&item.CompletedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AutomationExecution{}, ErrAutomationExecutionNotFound
	}
	if err != nil {
		return model.AutomationExecution{}, err
	}
	if err := decodeAutomationExecutionJSON(&item, triggerRaw, resultRaw); err != nil {
		return model.AutomationExecution{}, err
	}
	return item, nil
}

func (r *PostgresAutomationRepository) ListAutomationExecutions(organizationID int64, limit int) ([]model.AutomationExecution, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, policy_id, dedupe_key, status, trigger_snapshot,
			action_result, error_message, requested_at, approved_at, approved_by_user_id,
			rejected_at, rejected_by_user_id, executed_at, completed_at, created_at, updated_at
		FROM automation_executions
		WHERE organization_id = $1
		ORDER BY requested_at DESC, id DESC
		LIMIT $2
	`, organizationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AutomationExecution, 0)
	for rows.Next() {
		var item model.AutomationExecution
		var triggerRaw, resultRaw []byte
		if err := rows.Scan(
			&item.ID, &item.OrganizationID, &item.PolicyID, &item.DedupeKey, &item.Status,
			&triggerRaw, &resultRaw, &item.ErrorMessage, &item.RequestedAt, &item.ApprovedAt,
			&item.ApprovedByUserID, &item.RejectedAt, &item.RejectedByUserID, &item.ExecutedAt,
			&item.CompletedAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if err := decodeAutomationExecutionJSON(&item, triggerRaw, resultRaw); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresAutomationRepository) UpdateAutomationExecution(execution model.AutomationExecution) (model.AutomationExecution, error) {
	triggerRaw, err := json.Marshal(execution.TriggerSnapshot)
	if err != nil {
		return model.AutomationExecution{}, err
	}
	resultRaw, err := json.Marshal(execution.ActionResult)
	if err != nil {
		return model.AutomationExecution{}, err
	}
	var item model.AutomationExecution
	var triggerOut, resultOut []byte
	err = r.db.QueryRow(`
		UPDATE automation_executions
		SET status = $3, trigger_snapshot = $4::jsonb, action_result = $5::jsonb,
			error_message = $6, requested_at = $7, approved_at = $8,
			approved_by_user_id = $9, rejected_at = $10, rejected_by_user_id = $11,
			executed_at = $12, completed_at = $13, updated_at = $14
		WHERE organization_id = $1 AND id = $2
		RETURNING id, organization_id, policy_id, dedupe_key, status, trigger_snapshot,
			action_result, error_message, requested_at, approved_at, approved_by_user_id,
			rejected_at, rejected_by_user_id, executed_at, completed_at, created_at, updated_at
	`,
		execution.OrganizationID, execution.ID, execution.Status, string(triggerRaw),
		string(resultRaw), execution.ErrorMessage, execution.RequestedAt, execution.ApprovedAt,
		execution.ApprovedByUserID, execution.RejectedAt, execution.RejectedByUserID,
		execution.ExecutedAt, execution.CompletedAt, execution.UpdatedAt,
	).Scan(
		&item.ID, &item.OrganizationID, &item.PolicyID, &item.DedupeKey, &item.Status,
		&triggerOut, &resultOut, &item.ErrorMessage, &item.RequestedAt, &item.ApprovedAt,
		&item.ApprovedByUserID, &item.RejectedAt, &item.RejectedByUserID, &item.ExecutedAt,
		&item.CompletedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AutomationExecution{}, ErrAutomationExecutionNotFound
	}
	if err != nil {
		return model.AutomationExecution{}, err
	}
	if err := decodeAutomationExecutionJSON(&item, triggerOut, resultOut); err != nil {
		return model.AutomationExecution{}, err
	}
	return item, nil
}

func (r *PostgresAutomationRepository) LatestAutomationExecution(policyID int64) (model.AutomationExecution, error) {
	var item model.AutomationExecution
	var triggerRaw, resultRaw []byte
	err := r.db.QueryRow(`
		SELECT id, organization_id, policy_id, dedupe_key, status, trigger_snapshot,
			action_result, error_message, requested_at, approved_at, approved_by_user_id,
			rejected_at, rejected_by_user_id, executed_at, completed_at, created_at, updated_at
		FROM automation_executions
		WHERE policy_id = $1
		ORDER BY requested_at DESC, id DESC
		LIMIT 1
	`, policyID).Scan(
		&item.ID, &item.OrganizationID, &item.PolicyID, &item.DedupeKey, &item.Status,
		&triggerRaw, &resultRaw, &item.ErrorMessage, &item.RequestedAt, &item.ApprovedAt,
		&item.ApprovedByUserID, &item.RejectedAt, &item.RejectedByUserID, &item.ExecutedAt,
		&item.CompletedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AutomationExecution{}, ErrAutomationExecutionNotFound
	}
	if err != nil {
		return model.AutomationExecution{}, err
	}
	if err := decodeAutomationExecutionJSON(&item, triggerRaw, resultRaw); err != nil {
		return model.AutomationExecution{}, err
	}
	return item, nil
}

func decodeAutomationExecutionJSON(item *model.AutomationExecution, triggerRaw, resultRaw []byte) error {
	item.TriggerSnapshot = make(map[string]any)
	item.ActionResult = make(map[string]any)
	if len(triggerRaw) > 0 {
		if err := json.Unmarshal(triggerRaw, &item.TriggerSnapshot); err != nil {
			return err
		}
	}
	if len(resultRaw) > 0 {
		if err := json.Unmarshal(resultRaw, &item.ActionResult); err != nil {
			return err
		}
	}
	return nil
}
