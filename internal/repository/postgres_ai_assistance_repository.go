package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresAIAssistanceRepository struct {
	db *sql.DB
}

func NewPostgresAIAssistanceRepository(db *sql.DB) *PostgresAIAssistanceRepository {
	return &PostgresAIAssistanceRepository{db: db}
}

func (r *PostgresAIAssistanceRepository) GetPolicy(organizationID int64) (model.AIPolicy, error) {
	var item model.AIPolicy
	var allowedRaw []byte
	err := r.db.QueryRow(`
		SELECT organization_id,enabled,provider,monthly_budget_cents,redaction_enabled,max_input_chars,
		       allowed_classifications,external_max_classification,require_human_approval_for_actions,
		       updated_by_user_id,created_at,updated_at
		FROM ai_policies
		WHERE organization_id=$1
	`, organizationID).Scan(
		&item.OrganizationID, &item.Enabled, &item.Provider, &item.MonthlyBudgetCents, &item.RedactionEnabled,
		&item.MaxInputChars, &allowedRaw, &item.ExternalMaxClassification, &item.RequireHumanApprovalForActions,
		&item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AIPolicy{}, ErrAIPolicyNotFound
	}
	if err != nil {
		return model.AIPolicy{}, err
	}
	if err := json.Unmarshal(allowedRaw, &item.AllowedClassifications); err != nil {
		return model.AIPolicy{}, err
	}
	return item, nil
}

func (r *PostgresAIAssistanceRepository) UpsertPolicy(item model.AIPolicy) (model.AIPolicy, error) {
	raw, err := json.Marshal(item.AllowedClassifications)
	if err != nil {
		return model.AIPolicy{}, err
	}
	var allowedRaw []byte
	err = r.db.QueryRow(`
		INSERT INTO ai_policies (
			organization_id,enabled,provider,monthly_budget_cents,redaction_enabled,max_input_chars,
			allowed_classifications,external_max_classification,require_human_approval_for_actions,
			updated_by_user_id,created_at,updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11,$12)
		ON CONFLICT (organization_id) DO UPDATE SET
			enabled=EXCLUDED.enabled,
			provider=EXCLUDED.provider,
			monthly_budget_cents=EXCLUDED.monthly_budget_cents,
			redaction_enabled=EXCLUDED.redaction_enabled,
			max_input_chars=EXCLUDED.max_input_chars,
			allowed_classifications=EXCLUDED.allowed_classifications,
			external_max_classification=EXCLUDED.external_max_classification,
			require_human_approval_for_actions=EXCLUDED.require_human_approval_for_actions,
			updated_by_user_id=EXCLUDED.updated_by_user_id,
			updated_at=EXCLUDED.updated_at
		RETURNING organization_id,enabled,provider,monthly_budget_cents,redaction_enabled,max_input_chars,
			allowed_classifications,external_max_classification,require_human_approval_for_actions,
			updated_by_user_id,created_at,updated_at
	`, item.OrganizationID, item.Enabled, item.Provider, item.MonthlyBudgetCents, item.RedactionEnabled, item.MaxInputChars,
		string(raw), item.ExternalMaxClassification, item.RequireHumanApprovalForActions, item.UpdatedByUserID,
		item.CreatedAt, item.UpdatedAt,
	).Scan(
		&item.OrganizationID, &item.Enabled, &item.Provider, &item.MonthlyBudgetCents, &item.RedactionEnabled,
		&item.MaxInputChars, &allowedRaw, &item.ExternalMaxClassification, &item.RequireHumanApprovalForActions,
		&item.UpdatedByUserID, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.AIPolicy{}, err
	}
	if err := json.Unmarshal(allowedRaw, &item.AllowedClassifications); err != nil {
		return model.AIPolicy{}, err
	}
	return item, nil
}

func (r *PostgresAIAssistanceRepository) CreateRequest(item model.AIRequest) (model.AIRequest, error) {
	promptRaw, err := json.Marshal(item.PromptMetadata)
	if err != nil {
		return model.AIRequest{}, err
	}
	resultRaw, err := json.Marshal(item.StructuredResult)
	if err != nil {
		return model.AIRequest{}, err
	}
	actionRaw, err := json.Marshal(item.ProposedAction)
	if err != nil {
		return model.AIRequest{}, err
	}
	var promptOut, resultOut, actionOut []byte
	err = r.db.QueryRow(`
		INSERT INTO ai_requests (
			organization_id,workspace_id,feature,status,provider,model,classification,input_hash,redaction_count,
			prompt_metadata,structured_result,proposed_action,requires_approval,destructive_action,input_units,
			output_units,estimated_cost_cents,actual_cost_cents,created_by_user_id,created_at,decided_by_user_id,
			decision_comment,decided_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb,$12::jsonb,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
		RETURNING id,organization_id,workspace_id,feature,status,provider,model,classification,input_hash,redaction_count,
			prompt_metadata,structured_result,proposed_action,requires_approval,destructive_action,input_units,
			output_units,estimated_cost_cents,actual_cost_cents,created_by_user_id,created_at,decided_by_user_id,
			decision_comment,decided_at
	`, item.OrganizationID, item.WorkspaceID, item.Feature, item.Status, item.Provider, item.Model, item.Classification,
		item.InputHash, item.RedactionCount, string(promptRaw), string(resultRaw), string(actionRaw), item.RequiresApproval,
		item.DestructiveAction, item.InputUnits, item.OutputUnits, item.EstimatedCostCents, item.ActualCostCents,
		item.CreatedByUserID, item.CreatedAt, item.DecidedByUserID, item.DecisionComment, item.DecidedAt,
	).Scan(
		&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.Feature, &item.Status, &item.Provider, &item.Model,
		&item.Classification, &item.InputHash, &item.RedactionCount, &promptOut, &resultOut, &actionOut,
		&item.RequiresApproval, &item.DestructiveAction, &item.InputUnits, &item.OutputUnits, &item.EstimatedCostCents,
		&item.ActualCostCents, &item.CreatedByUserID, &item.CreatedAt, &item.DecidedByUserID, &item.DecisionComment,
		&item.DecidedAt,
	)
	if err != nil {
		return model.AIRequest{}, err
	}
	if err := decodeAIRequestMaps(&item, promptOut, resultOut, actionOut); err != nil {
		return model.AIRequest{}, err
	}
	return item, nil
}

func (r *PostgresAIAssistanceRepository) GetRequest(organizationID, requestID int64) (model.AIRequest, error) {
	item, err := scanAIRequest(r.db.QueryRow(`
		SELECT id,organization_id,workspace_id,feature,status,provider,model,classification,input_hash,redaction_count,
			prompt_metadata,structured_result,proposed_action,requires_approval,destructive_action,input_units,
			output_units,estimated_cost_cents,actual_cost_cents,created_by_user_id,created_at,decided_by_user_id,
			decision_comment,decided_at
		FROM ai_requests WHERE organization_id=$1 AND id=$2
	`, organizationID, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.AIRequest{}, ErrAIRequestNotFound
	}
	return item, err
}

func (r *PostgresAIAssistanceRepository) ListRequests(organizationID int64, limit int) ([]model.AIRequest, error) {
	rows, err := r.db.Query(`
		SELECT id,organization_id,workspace_id,feature,status,provider,model,classification,input_hash,redaction_count,
			prompt_metadata,structured_result,proposed_action,requires_approval,destructive_action,input_units,
			output_units,estimated_cost_cents,actual_cost_cents,created_by_user_id,created_at,decided_by_user_id,
			decision_comment,decided_at
		FROM ai_requests WHERE organization_id=$1
		ORDER BY id DESC LIMIT $2
	`, organizationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AIRequest, 0)
	for rows.Next() {
		item, err := scanAIRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresAIAssistanceRepository) UpdateRequest(item model.AIRequest) (model.AIRequest, error) {
	promptRaw, err := json.Marshal(item.PromptMetadata)
	if err != nil {
		return model.AIRequest{}, err
	}
	resultRaw, err := json.Marshal(item.StructuredResult)
	if err != nil {
		return model.AIRequest{}, err
	}
	actionRaw, err := json.Marshal(item.ProposedAction)
	if err != nil {
		return model.AIRequest{}, err
	}
	var promptOut, resultOut, actionOut []byte
	err = r.db.QueryRow(`
		UPDATE ai_requests SET
			status=$3,provider=$4,model=$5,classification=$6,input_hash=$7,redaction_count=$8,
			prompt_metadata=$9::jsonb,structured_result=$10::jsonb,proposed_action=$11::jsonb,
			requires_approval=$12,destructive_action=$13,input_units=$14,output_units=$15,
			estimated_cost_cents=$16,actual_cost_cents=$17,decided_by_user_id=$18,
			decision_comment=$19,decided_at=$20
		WHERE organization_id=$1 AND id=$2
		RETURNING id,organization_id,workspace_id,feature,status,provider,model,classification,input_hash,redaction_count,
			prompt_metadata,structured_result,proposed_action,requires_approval,destructive_action,input_units,
			output_units,estimated_cost_cents,actual_cost_cents,created_by_user_id,created_at,decided_by_user_id,
			decision_comment,decided_at
	`, item.OrganizationID, item.ID, item.Status, item.Provider, item.Model, item.Classification, item.InputHash,
		item.RedactionCount, string(promptRaw), string(resultRaw), string(actionRaw), item.RequiresApproval,
		item.DestructiveAction, item.InputUnits, item.OutputUnits, item.EstimatedCostCents, item.ActualCostCents,
		item.DecidedByUserID, item.DecisionComment, item.DecidedAt,
	).Scan(
		&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.Feature, &item.Status, &item.Provider, &item.Model,
		&item.Classification, &item.InputHash, &item.RedactionCount, &promptOut, &resultOut, &actionOut,
		&item.RequiresApproval, &item.DestructiveAction, &item.InputUnits, &item.OutputUnits, &item.EstimatedCostCents,
		&item.ActualCostCents, &item.CreatedByUserID, &item.CreatedAt, &item.DecidedByUserID, &item.DecisionComment,
		&item.DecidedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AIRequest{}, ErrAIRequestNotFound
	}
	if err != nil {
		return model.AIRequest{}, err
	}
	if err := decodeAIRequestMaps(&item, promptOut, resultOut, actionOut); err != nil {
		return model.AIRequest{}, err
	}
	return item, nil
}

func (r *PostgresAIAssistanceRepository) MonthlyUsage(organizationID int64, from, to time.Time) (model.AIUsageSummary, error) {
	item := model.AIUsageSummary{OrganizationID: organizationID, Month: from.Format("2006-01")}
	err := r.db.QueryRow(`
		SELECT COUNT(*),COALESCE(SUM(actual_cost_cents),0),COALESCE(SUM(input_units),0),COALESCE(SUM(output_units),0)
		FROM ai_requests
		WHERE organization_id=$1 AND created_at >= $2 AND created_at < $3
		  AND status IN ('completed','pending_approval','approved')
	`, organizationID, from, to).Scan(&item.RequestCount, &item.SpentCents, &item.InputUnits, &item.OutputUnits)
	return item, err
}

func (r *PostgresAIAssistanceRepository) CreateEvaluationCase(item model.AIEvaluationCase) (model.AIEvaluationCase, error) {
	keywordsRaw, err := json.Marshal(item.ExpectedKeywords)
	if err != nil {
		return model.AIEvaluationCase{}, err
	}
	metadataRaw, err := json.Marshal(item.Metadata)
	if err != nil {
		return model.AIEvaluationCase{}, err
	}
	var keywordsOut, metadataOut []byte
	err = r.db.QueryRow(`
		INSERT INTO ai_evaluation_cases (
			organization_id,name,feature,input,classification,expected_keywords,metadata,created_by_user_id,created_at,updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,$10)
		RETURNING id,organization_id,name,feature,input,classification,expected_keywords,metadata,created_by_user_id,created_at,updated_at
	`, item.OrganizationID, item.Name, item.Feature, item.Input, item.Classification, string(keywordsRaw),
		string(metadataRaw), item.CreatedByUserID, item.CreatedAt, item.UpdatedAt,
	).Scan(&item.ID, &item.OrganizationID, &item.Name, &item.Feature, &item.Input, &item.Classification,
		&keywordsOut, &metadataOut, &item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return model.AIEvaluationCase{}, err
	}
	if err := json.Unmarshal(keywordsOut, &item.ExpectedKeywords); err != nil {
		return model.AIEvaluationCase{}, err
	}
	if err := json.Unmarshal(metadataOut, &item.Metadata); err != nil {
		return model.AIEvaluationCase{}, err
	}
	return item, nil
}

func (r *PostgresAIAssistanceRepository) GetEvaluationCase(organizationID, caseID int64) (model.AIEvaluationCase, error) {
	item, err := scanAIEvaluationCase(r.db.QueryRow(`
		SELECT id,organization_id,name,feature,input,classification,expected_keywords,metadata,created_by_user_id,created_at,updated_at
		FROM ai_evaluation_cases WHERE organization_id=$1 AND id=$2
	`, organizationID, caseID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.AIEvaluationCase{}, ErrAIEvaluationCaseNotFound
	}
	return item, err
}

func (r *PostgresAIAssistanceRepository) ListEvaluationCases(organizationID int64) ([]model.AIEvaluationCase, error) {
	rows, err := r.db.Query(`
		SELECT id,organization_id,name,feature,input,classification,expected_keywords,metadata,created_by_user_id,created_at,updated_at
		FROM ai_evaluation_cases WHERE organization_id=$1 ORDER BY id
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AIEvaluationCase, 0)
	for rows.Next() {
		item, err := scanAIEvaluationCase(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresAIAssistanceRepository) CreateEvaluationRun(item model.AIEvaluationRun) (model.AIEvaluationRun, error) {
	metadataRaw, err := json.Marshal(item.Metadata)
	if err != nil {
		return model.AIEvaluationRun{}, err
	}
	var metadataOut []byte
	err = r.db.QueryRow(`
		INSERT INTO ai_evaluation_runs (
			organization_id,case_id,provider,model,score_basis_points,passed,metadata,created_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8)
		RETURNING id,organization_id,case_id,provider,model,score_basis_points,passed,metadata,created_at
	`, item.OrganizationID, item.CaseID, item.Provider, item.Model, item.ScoreBasisPoints, item.Passed,
		string(metadataRaw), item.CreatedAt,
	).Scan(&item.ID, &item.OrganizationID, &item.CaseID, &item.Provider, &item.Model, &item.ScoreBasisPoints,
		&item.Passed, &metadataOut, &item.CreatedAt)
	if err != nil {
		return model.AIEvaluationRun{}, err
	}
	if err := json.Unmarshal(metadataOut, &item.Metadata); err != nil {
		return model.AIEvaluationRun{}, err
	}
	return item, nil
}

func (r *PostgresAIAssistanceRepository) ListEvaluationRuns(organizationID int64, limit int) ([]model.AIEvaluationRun, error) {
	rows, err := r.db.Query(`
		SELECT id,organization_id,case_id,provider,model,score_basis_points,passed,metadata,created_at
		FROM ai_evaluation_runs WHERE organization_id=$1
		ORDER BY id DESC LIMIT $2
	`, organizationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AIEvaluationRun, 0)
	for rows.Next() {
		item, err := scanAIEvaluationRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type aiScanner interface {
	Scan(dest ...any) error
}

func scanAIRequest(scanner aiScanner) (model.AIRequest, error) {
	var item model.AIRequest
	var promptRaw, resultRaw, actionRaw []byte
	err := scanner.Scan(
		&item.ID, &item.OrganizationID, &item.WorkspaceID, &item.Feature, &item.Status, &item.Provider, &item.Model,
		&item.Classification, &item.InputHash, &item.RedactionCount, &promptRaw, &resultRaw, &actionRaw,
		&item.RequiresApproval, &item.DestructiveAction, &item.InputUnits, &item.OutputUnits, &item.EstimatedCostCents,
		&item.ActualCostCents, &item.CreatedByUserID, &item.CreatedAt, &item.DecidedByUserID, &item.DecisionComment,
		&item.DecidedAt,
	)
	if err != nil {
		return model.AIRequest{}, err
	}
	if err := decodeAIRequestMaps(&item, promptRaw, resultRaw, actionRaw); err != nil {
		return model.AIRequest{}, err
	}
	return item, nil
}

func decodeAIRequestMaps(item *model.AIRequest, promptRaw, resultRaw, actionRaw []byte) error {
	if err := json.Unmarshal(promptRaw, &item.PromptMetadata); err != nil {
		return err
	}
	if err := json.Unmarshal(resultRaw, &item.StructuredResult); err != nil {
		return err
	}
	if err := json.Unmarshal(actionRaw, &item.ProposedAction); err != nil {
		return err
	}
	return nil
}

func scanAIEvaluationCase(scanner aiScanner) (model.AIEvaluationCase, error) {
	var item model.AIEvaluationCase
	var keywordsRaw, metadataRaw []byte
	err := scanner.Scan(&item.ID, &item.OrganizationID, &item.Name, &item.Feature, &item.Input, &item.Classification,
		&keywordsRaw, &metadataRaw, &item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return model.AIEvaluationCase{}, err
	}
	if err := json.Unmarshal(keywordsRaw, &item.ExpectedKeywords); err != nil {
		return model.AIEvaluationCase{}, err
	}
	if err := json.Unmarshal(metadataRaw, &item.Metadata); err != nil {
		return model.AIEvaluationCase{}, err
	}
	return item, nil
}

func scanAIEvaluationRun(scanner aiScanner) (model.AIEvaluationRun, error) {
	var item model.AIEvaluationRun
	var metadataRaw []byte
	err := scanner.Scan(&item.ID, &item.OrganizationID, &item.CaseID, &item.Provider, &item.Model,
		&item.ScoreBasisPoints, &item.Passed, &metadataRaw, &item.CreatedAt)
	if err != nil {
		return model.AIEvaluationRun{}, err
	}
	if err := json.Unmarshal(metadataRaw, &item.Metadata); err != nil {
		return model.AIEvaluationRun{}, err
	}
	return item, nil
}
