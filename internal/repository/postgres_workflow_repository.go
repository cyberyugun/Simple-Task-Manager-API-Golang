package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresWorkflowRepository struct{ db *sql.DB }

func NewPostgresWorkflowRepository(db *sql.DB) *PostgresWorkflowRepository {
	return &PostgresWorkflowRepository{db: db}
}

func (r *PostgresWorkflowRepository) CreateWorkflow(v model.WorkflowDefinition) (model.WorkflowDefinition, error) {
	err := r.db.QueryRow(`INSERT INTO workflow_definitions
		(organization_id,name,description,active_version_id,created_by_user_id,updated_by_user_id,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id,organization_id,name,description,active_version_id,created_by_user_id,updated_by_user_id,created_at,updated_at`,
		v.OrganizationID, v.Name, v.Description, v.ActiveVersionID, v.CreatedByUserID, v.UpdatedByUserID, v.CreatedAt, v.UpdatedAt).
		Scan(&v.ID, &v.OrganizationID, &v.Name, &v.Description, &v.ActiveVersionID, &v.CreatedByUserID, &v.UpdatedByUserID, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}

func (r *PostgresWorkflowRepository) UpdateWorkflow(v model.WorkflowDefinition) (model.WorkflowDefinition, error) {
	err := r.db.QueryRow(`UPDATE workflow_definitions SET name=$3,description=$4,active_version_id=$5,updated_by_user_id=$6,updated_at=$7
		WHERE organization_id=$1 AND id=$2
		RETURNING id,organization_id,name,description,active_version_id,created_by_user_id,updated_by_user_id,created_at,updated_at`,
		v.OrganizationID, v.ID, v.Name, v.Description, v.ActiveVersionID, v.UpdatedByUserID, v.UpdatedAt).
		Scan(&v.ID, &v.OrganizationID, &v.Name, &v.Description, &v.ActiveVersionID, &v.CreatedByUserID, &v.UpdatedByUserID, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkflowDefinition{}, ErrWorkflowNotFound
	}
	return v, err
}

func (r *PostgresWorkflowRepository) GetWorkflow(orgID, id int64) (model.WorkflowDefinition, error) {
	var v model.WorkflowDefinition
	err := r.db.QueryRow(`SELECT id,organization_id,name,description,active_version_id,created_by_user_id,updated_by_user_id,created_at,updated_at
		FROM workflow_definitions WHERE organization_id=$1 AND id=$2`, orgID, id).
		Scan(&v.ID, &v.OrganizationID, &v.Name, &v.Description, &v.ActiveVersionID, &v.CreatedByUserID, &v.UpdatedByUserID, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkflowDefinition{}, ErrWorkflowNotFound
	}
	return v, err
}

func (r *PostgresWorkflowRepository) ListWorkflows(orgID int64) ([]model.WorkflowDefinition, error) {
	rows, err := r.db.Query(`SELECT id,organization_id,name,description,active_version_id,created_by_user_id,updated_by_user_id,created_at,updated_at
		FROM workflow_definitions WHERE organization_id=$1 ORDER BY id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.WorkflowDefinition{}
	for rows.Next() {
		var v model.WorkflowDefinition
		if err := rows.Scan(&v.ID, &v.OrganizationID, &v.Name, &v.Description, &v.ActiveVersionID, &v.CreatedByUserID, &v.UpdatedByUserID, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *PostgresWorkflowRepository) CreateWorkflowVersion(v model.WorkflowVersion) (model.WorkflowVersion, error) {
	raw, err := json.Marshal(v.Graph)
	if err != nil {
		return model.WorkflowVersion{}, err
	}
	var graph []byte
	err = r.db.QueryRow(`INSERT INTO workflow_versions
		(organization_id,workflow_id,version,status,graph,checksum,created_by_user_id,published_by_user_id,published_at,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11)
		RETURNING id,organization_id,workflow_id,version,status,graph,checksum,created_by_user_id,published_by_user_id,published_at,created_at,updated_at`,
		v.OrganizationID, v.WorkflowID, v.Version, v.Status, string(raw), v.Checksum, v.CreatedByUserID, v.PublishedByID, v.PublishedAt, v.CreatedAt, v.UpdatedAt).
		Scan(&v.ID, &v.OrganizationID, &v.WorkflowID, &v.Version, &v.Status, &graph, &v.Checksum, &v.CreatedByUserID, &v.PublishedByID, &v.PublishedAt, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return model.WorkflowVersion{}, err
	}
	if err = json.Unmarshal(graph, &v.Graph); err != nil {
		return model.WorkflowVersion{}, err
	}
	return v, nil
}

func (r *PostgresWorkflowRepository) UpdateWorkflowVersion(v model.WorkflowVersion) (model.WorkflowVersion, error) {
	raw, err := json.Marshal(v.Graph)
	if err != nil {
		return model.WorkflowVersion{}, err
	}
	var graph []byte
	err = r.db.QueryRow(`UPDATE workflow_versions SET status=$4,graph=$5::jsonb,checksum=$6,published_by_user_id=$7,published_at=$8,updated_at=$9
		WHERE organization_id=$1 AND workflow_id=$2 AND id=$3
		RETURNING id,organization_id,workflow_id,version,status,graph,checksum,created_by_user_id,published_by_user_id,published_at,created_at,updated_at`,
		v.OrganizationID, v.WorkflowID, v.ID, v.Status, string(raw), v.Checksum, v.PublishedByID, v.PublishedAt, v.UpdatedAt).
		Scan(&v.ID, &v.OrganizationID, &v.WorkflowID, &v.Version, &v.Status, &graph, &v.Checksum, &v.CreatedByUserID, &v.PublishedByID, &v.PublishedAt, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkflowVersion{}, ErrWorkflowVersionNotFound
	}
	if err != nil {
		return model.WorkflowVersion{}, err
	}
	if err = json.Unmarshal(graph, &v.Graph); err != nil {
		return model.WorkflowVersion{}, err
	}
	return v, nil
}

func (r *PostgresWorkflowRepository) GetWorkflowVersion(orgID, id int64) (model.WorkflowVersion, error) {
	var v model.WorkflowVersion
	var graph []byte
	err := r.db.QueryRow(`SELECT id,organization_id,workflow_id,version,status,graph,checksum,created_by_user_id,published_by_user_id,published_at,created_at,updated_at
		FROM workflow_versions WHERE organization_id=$1 AND id=$2`, orgID, id).
		Scan(&v.ID, &v.OrganizationID, &v.WorkflowID, &v.Version, &v.Status, &graph, &v.Checksum, &v.CreatedByUserID, &v.PublishedByID, &v.PublishedAt, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkflowVersion{}, ErrWorkflowVersionNotFound
	}
	if err != nil {
		return model.WorkflowVersion{}, err
	}
	if err = json.Unmarshal(graph, &v.Graph); err != nil {
		return model.WorkflowVersion{}, err
	}
	return v, nil
}

func (r *PostgresWorkflowRepository) ListWorkflowVersions(orgID, workflowID int64) ([]model.WorkflowVersion, error) {
	rows, err := r.db.Query(`SELECT id,organization_id,workflow_id,version,status,graph,checksum,created_by_user_id,published_by_user_id,published_at,created_at,updated_at
		FROM workflow_versions WHERE organization_id=$1 AND workflow_id=$2 ORDER BY version`, orgID, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.WorkflowVersion{}
	for rows.Next() {
		var v model.WorkflowVersion
		var graph []byte
		if err := rows.Scan(&v.ID, &v.OrganizationID, &v.WorkflowID, &v.Version, &v.Status, &graph, &v.Checksum, &v.CreatedByUserID, &v.PublishedByID, &v.PublishedAt, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(graph, &v.Graph); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *PostgresWorkflowRepository) NextWorkflowVersionNumber(workflowID int64) (int, error) {
	var n int
	err := r.db.QueryRow(`SELECT COALESCE(MAX(version),0)+1 FROM workflow_versions WHERE workflow_id=$1`, workflowID).Scan(&n)
	return n, err
}

func encodeWorkflowExecution(v model.WorkflowExecution) (string, string, string, string, error) {
	a, e := json.Marshal(v.TriggerPayload)
	if e != nil {
		return "", "", "", "", e
	}
	b, e := json.Marshal(v.Variables)
	if e != nil {
		return "", "", "", "", e
	}
	c, e := json.Marshal(v.WorkflowSnapshot)
	if e != nil {
		return "", "", "", "", e
	}
	d, e := json.Marshal(v.NextNodeIDs)
	if e != nil {
		return "", "", "", "", e
	}
	return string(a), string(b), string(c), string(d), nil
}
func decodeWorkflowExecution(v *model.WorkflowExecution, a, b, c, d []byte) error {
	v.TriggerPayload = map[string]any{}
	v.Variables = map[string]any{}
	v.NextNodeIDs = []string{}
	if err := json.Unmarshal(a, &v.TriggerPayload); err != nil {
		return err
	}
	if err := json.Unmarshal(b, &v.Variables); err != nil {
		return err
	}
	if err := json.Unmarshal(c, &v.WorkflowSnapshot); err != nil {
		return err
	}
	return json.Unmarshal(d, &v.NextNodeIDs)
}

type workflowScanner interface{ Scan(...any) error }

func scanWorkflowExecution(s workflowScanner) (model.WorkflowExecution, error) {
	var v model.WorkflowExecution
	var a, b, c, d []byte
	err := s.Scan(&v.ID, &v.OrganizationID, &v.WorkflowID, &v.WorkflowVersionID, &v.Status, &v.TriggerType, &v.TriggerKey, &a, &b, &c, &d, &v.DryRun, &v.ErrorMessage, &v.RequestedByUserID, &v.ResumeAt, &v.StartedAt, &v.CompletedAt, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return model.WorkflowExecution{}, err
	}
	if err := decodeWorkflowExecution(&v, a, b, c, d); err != nil {
		return model.WorkflowExecution{}, err
	}
	return v, nil
}

const workflowExecutionColumns = `id,organization_id,workflow_id,workflow_version_id,status,trigger_type,trigger_key,trigger_payload,variables,workflow_snapshot,next_node_ids,dry_run,error_message,requested_by_user_id,resume_at,started_at,completed_at,created_at,updated_at`

func (r *PostgresWorkflowRepository) CreateWorkflowExecution(v model.WorkflowExecution) (model.WorkflowExecution, error) {
	a, b, c, d, err := encodeWorkflowExecution(v)
	if err != nil {
		return model.WorkflowExecution{}, err
	}
	row := r.db.QueryRow(`INSERT INTO workflow_executions
		(organization_id,workflow_id,workflow_version_id,status,trigger_type,trigger_key,trigger_payload,variables,workflow_snapshot,next_node_ids,dry_run,error_message,requested_by_user_id,resume_at,started_at,completed_at,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9::jsonb,$10::jsonb,$11,$12,$13,$14,$15,$16,$17,$18)
		RETURNING `+workflowExecutionColumns,
		v.OrganizationID, v.WorkflowID, v.WorkflowVersionID, v.Status, v.TriggerType, v.TriggerKey, a, b, c, d, v.DryRun, v.ErrorMessage, v.RequestedByUserID, v.ResumeAt, v.StartedAt, v.CompletedAt, v.CreatedAt, v.UpdatedAt)
	return scanWorkflowExecution(row)
}

func (r *PostgresWorkflowRepository) UpdateWorkflowExecution(v model.WorkflowExecution) (model.WorkflowExecution, error) {
	a, b, c, d, err := encodeWorkflowExecution(v)
	if err != nil {
		return model.WorkflowExecution{}, err
	}
	row := r.db.QueryRow(`UPDATE workflow_executions SET status=$3,trigger_type=$4,trigger_key=$5,trigger_payload=$6::jsonb,variables=$7::jsonb,workflow_snapshot=$8::jsonb,next_node_ids=$9::jsonb,dry_run=$10,error_message=$11,requested_by_user_id=$12,resume_at=$13,started_at=$14,completed_at=$15,updated_at=$16
		WHERE organization_id=$1 AND id=$2 RETURNING `+workflowExecutionColumns,
		v.OrganizationID, v.ID, v.Status, v.TriggerType, v.TriggerKey, a, b, c, d, v.DryRun, v.ErrorMessage, v.RequestedByUserID, v.ResumeAt, v.StartedAt, v.CompletedAt, v.UpdatedAt)
	out, err := scanWorkflowExecution(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkflowExecution{}, ErrWorkflowExecutionNotFound
	}
	return out, err
}

func (r *PostgresWorkflowRepository) GetWorkflowExecution(orgID, id int64) (model.WorkflowExecution, error) {
	out, err := scanWorkflowExecution(r.db.QueryRow(`SELECT `+workflowExecutionColumns+` FROM workflow_executions WHERE organization_id=$1 AND id=$2`, orgID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkflowExecution{}, ErrWorkflowExecutionNotFound
	}
	return out, err
}
func (r *PostgresWorkflowRepository) ListWorkflowExecutions(orgID int64, limit int) ([]model.WorkflowExecution, error) {
	rows, err := r.db.Query(`SELECT `+workflowExecutionColumns+` FROM workflow_executions WHERE organization_id=$1 ORDER BY started_at DESC,id DESC LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.WorkflowExecution{}
	for rows.Next() {
		v, e := scanWorkflowExecution(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgresWorkflowRepository) ListDueWorkflowExecutions(now time.Time, limit int) ([]model.WorkflowExecution, error) {
	rows, err := r.db.Query(`SELECT `+workflowExecutionColumns+` FROM workflow_executions WHERE status=$1 AND resume_at IS NOT NULL AND resume_at<=$2 ORDER BY resume_at,id LIMIT $3`, model.WorkflowExecutionWaitingDelay, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.WorkflowExecution{}
	for rows.Next() {
		v, e := scanWorkflowExecution(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func decodeNodeRun(v *model.WorkflowNodeExecution, a, b []byte) error {
	v.Input = map[string]any{}
	v.Output = map[string]any{}
	if err := json.Unmarshal(a, &v.Input); err != nil {
		return err
	}
	return json.Unmarshal(b, &v.Output)
}
func (r *PostgresWorkflowRepository) CreateWorkflowNodeExecution(v model.WorkflowNodeExecution) (model.WorkflowNodeExecution, error) {
	a, _ := json.Marshal(v.Input)
	b, _ := json.Marshal(v.Output)
	var ao, bo []byte
	err := r.db.QueryRow(`INSERT INTO workflow_node_executions(execution_id,node_id,node_type,status,attempt,input,output,error_message,retry_at,started_at,completed_at,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9,$10,$11,$12,$13)
		RETURNING id,execution_id,node_id,node_type,status,attempt,input,output,error_message,retry_at,started_at,completed_at,created_at,updated_at`,
		v.ExecutionID, v.NodeID, v.NodeType, v.Status, v.Attempt, string(a), string(b), v.ErrorMessage, v.RetryAt, v.StartedAt, v.CompletedAt, v.CreatedAt, v.UpdatedAt).
		Scan(&v.ID, &v.ExecutionID, &v.NodeID, &v.NodeType, &v.Status, &v.Attempt, &ao, &bo, &v.ErrorMessage, &v.RetryAt, &v.StartedAt, &v.CompletedAt, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return model.WorkflowNodeExecution{}, err
	}
	if err := decodeNodeRun(&v, ao, bo); err != nil {
		return model.WorkflowNodeExecution{}, err
	}
	return v, nil
}
func (r *PostgresWorkflowRepository) UpdateWorkflowNodeExecution(v model.WorkflowNodeExecution) (model.WorkflowNodeExecution, error) {
	a, _ := json.Marshal(v.Input)
	b, _ := json.Marshal(v.Output)
	var ao, bo []byte
	err := r.db.QueryRow(`UPDATE workflow_node_executions SET status=$3,attempt=$4,input=$5::jsonb,output=$6::jsonb,error_message=$7,retry_at=$8,started_at=$9,completed_at=$10,updated_at=$11
		WHERE execution_id=$1 AND id=$2 RETURNING id,execution_id,node_id,node_type,status,attempt,input,output,error_message,retry_at,started_at,completed_at,created_at,updated_at`,
		v.ExecutionID, v.ID, v.Status, v.Attempt, string(a), string(b), v.ErrorMessage, v.RetryAt, v.StartedAt, v.CompletedAt, v.UpdatedAt).
		Scan(&v.ID, &v.ExecutionID, &v.NodeID, &v.NodeType, &v.Status, &v.Attempt, &ao, &bo, &v.ErrorMessage, &v.RetryAt, &v.StartedAt, &v.CompletedAt, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkflowNodeExecution{}, ErrWorkflowNodeRunNotFound
	}
	if err != nil {
		return model.WorkflowNodeExecution{}, err
	}
	if err := decodeNodeRun(&v, ao, bo); err != nil {
		return model.WorkflowNodeExecution{}, err
	}
	return v, nil
}
func (r *PostgresWorkflowRepository) ListWorkflowNodeExecutions(executionID int64) ([]model.WorkflowNodeExecution, error) {
	rows, err := r.db.Query(`SELECT id,execution_id,node_id,node_type,status,attempt,input,output,error_message,retry_at,started_at,completed_at,created_at,updated_at FROM workflow_node_executions WHERE execution_id=$1 ORDER BY id`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.WorkflowNodeExecution{}
	for rows.Next() {
		var v model.WorkflowNodeExecution
		var a, b []byte
		if err := rows.Scan(&v.ID, &v.ExecutionID, &v.NodeID, &v.NodeType, &v.Status, &v.Attempt, &a, &b, &v.ErrorMessage, &v.RetryAt, &v.StartedAt, &v.CompletedAt, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		if err := decodeNodeRun(&v, a, b); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *PostgresWorkflowRepository) LatestWorkflowNodeExecution(executionID int64, nodeID string) (model.WorkflowNodeExecution, error) {
	var v model.WorkflowNodeExecution
	var a, b []byte
	err := r.db.QueryRow(`SELECT id,execution_id,node_id,node_type,status,attempt,input,output,error_message,retry_at,started_at,completed_at,created_at,updated_at FROM workflow_node_executions WHERE execution_id=$1 AND node_id=$2 ORDER BY attempt DESC,id DESC LIMIT 1`, executionID, nodeID).
		Scan(&v.ID, &v.ExecutionID, &v.NodeID, &v.NodeType, &v.Status, &v.Attempt, &a, &b, &v.ErrorMessage, &v.RetryAt, &v.StartedAt, &v.CompletedAt, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkflowNodeExecution{}, ErrWorkflowNodeRunNotFound
	}
	if err != nil {
		return model.WorkflowNodeExecution{}, err
	}
	if err := decodeNodeRun(&v, a, b); err != nil {
		return model.WorkflowNodeExecution{}, err
	}
	return v, nil
}

func (r *PostgresWorkflowRepository) CreateWorkflowApproval(v model.WorkflowApproval) (model.WorkflowApproval, error) {
	err := r.db.QueryRow(`INSERT INTO workflow_approvals(organization_id,execution_id,node_id,status,requested_at,decided_at,decided_by_user_id,comment,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id,organization_id,execution_id,node_id,status,requested_at,decided_at,decided_by_user_id,comment,created_at,updated_at`,
		v.OrganizationID, v.ExecutionID, v.NodeID, v.Status, v.RequestedAt, v.DecidedAt, v.DecidedByUserID, v.Comment, v.CreatedAt, v.UpdatedAt).
		Scan(&v.ID, &v.OrganizationID, &v.ExecutionID, &v.NodeID, &v.Status, &v.RequestedAt, &v.DecidedAt, &v.DecidedByUserID, &v.Comment, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}
func (r *PostgresWorkflowRepository) UpdateWorkflowApproval(v model.WorkflowApproval) (model.WorkflowApproval, error) {
	err := r.db.QueryRow(`UPDATE workflow_approvals SET status=$3,decided_at=$4,decided_by_user_id=$5,comment=$6,updated_at=$7 WHERE organization_id=$1 AND id=$2
		RETURNING id,organization_id,execution_id,node_id,status,requested_at,decided_at,decided_by_user_id,comment,created_at,updated_at`,
		v.OrganizationID, v.ID, v.Status, v.DecidedAt, v.DecidedByUserID, v.Comment, v.UpdatedAt).
		Scan(&v.ID, &v.OrganizationID, &v.ExecutionID, &v.NodeID, &v.Status, &v.RequestedAt, &v.DecidedAt, &v.DecidedByUserID, &v.Comment, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkflowApproval{}, ErrWorkflowApprovalNotFound
	}
	return v, err
}
func (r *PostgresWorkflowRepository) GetWorkflowApproval(orgID, id int64) (model.WorkflowApproval, error) {
	var v model.WorkflowApproval
	err := r.db.QueryRow(`SELECT id,organization_id,execution_id,node_id,status,requested_at,decided_at,decided_by_user_id,comment,created_at,updated_at FROM workflow_approvals WHERE organization_id=$1 AND id=$2`, orgID, id).
		Scan(&v.ID, &v.OrganizationID, &v.ExecutionID, &v.NodeID, &v.Status, &v.RequestedAt, &v.DecidedAt, &v.DecidedByUserID, &v.Comment, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkflowApproval{}, ErrWorkflowApprovalNotFound
	}
	return v, err
}
func (r *PostgresWorkflowRepository) FindWorkflowApproval(executionID int64, nodeID string) (model.WorkflowApproval, error) {
	var v model.WorkflowApproval
	err := r.db.QueryRow(`SELECT id,organization_id,execution_id,node_id,status,requested_at,decided_at,decided_by_user_id,comment,created_at,updated_at FROM workflow_approvals WHERE execution_id=$1 AND node_id=$2 ORDER BY id DESC LIMIT 1`, executionID, nodeID).
		Scan(&v.ID, &v.OrganizationID, &v.ExecutionID, &v.NodeID, &v.Status, &v.RequestedAt, &v.DecidedAt, &v.DecidedByUserID, &v.Comment, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkflowApproval{}, ErrWorkflowApprovalNotFound
	}
	return v, err
}
func (r *PostgresWorkflowRepository) ListWorkflowApprovals(executionID int64) ([]model.WorkflowApproval, error) {
	rows, err := r.db.Query(`SELECT id,organization_id,execution_id,node_id,status,requested_at,decided_at,decided_by_user_id,comment,created_at,updated_at FROM workflow_approvals WHERE execution_id=$1 ORDER BY id`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.WorkflowApproval{}
	for rows.Next() {
		var v model.WorkflowApproval
		if err := rows.Scan(&v.ID, &v.OrganizationID, &v.ExecutionID, &v.NodeID, &v.Status, &v.RequestedAt, &v.DecidedAt, &v.DecidedByUserID, &v.Comment, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *PostgresWorkflowRepository) CreateWorkflowCheckpoint(v model.WorkflowCheckpoint) (model.WorkflowCheckpoint, error) {
	a, _ := json.Marshal(v.Variables)
	b, _ := json.Marshal(v.NextNodeIDs)
	var ao, bo []byte
	if v.Sequence <= 0 {
		err := r.db.QueryRow(`INSERT INTO workflow_checkpoints(execution_id,sequence,status,node_id,variables,next_node_ids,created_at)
			SELECT $1,COALESCE(MAX(sequence),0)+1,$2,$3,$4::jsonb,$5::jsonb,$6 FROM workflow_checkpoints WHERE execution_id=$1
			RETURNING id,execution_id,sequence,status,node_id,variables,next_node_ids,created_at`,
			v.ExecutionID, v.Status, v.NodeID, string(a), string(b), v.CreatedAt).Scan(&v.ID, &v.ExecutionID, &v.Sequence, &v.Status, &v.NodeID, &ao, &bo, &v.CreatedAt)
		if err != nil {
			return model.WorkflowCheckpoint{}, err
		}
	} else {
		err := r.db.QueryRow(`INSERT INTO workflow_checkpoints(execution_id,sequence,status,node_id,variables,next_node_ids,created_at) VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7)
			RETURNING id,execution_id,sequence,status,node_id,variables,next_node_ids,created_at`,
			v.ExecutionID, v.Sequence, v.Status, v.NodeID, string(a), string(b), v.CreatedAt).Scan(&v.ID, &v.ExecutionID, &v.Sequence, &v.Status, &v.NodeID, &ao, &bo, &v.CreatedAt)
		if err != nil {
			return model.WorkflowCheckpoint{}, err
		}
	}
	v.Variables = map[string]any{}
	v.NextNodeIDs = []string{}
	if err := json.Unmarshal(ao, &v.Variables); err != nil {
		return model.WorkflowCheckpoint{}, err
	}
	if err := json.Unmarshal(bo, &v.NextNodeIDs); err != nil {
		return model.WorkflowCheckpoint{}, err
	}
	return v, nil
}
func (r *PostgresWorkflowRepository) ListWorkflowCheckpoints(executionID int64) ([]model.WorkflowCheckpoint, error) {
	rows, err := r.db.Query(`SELECT id,execution_id,sequence,status,node_id,variables,next_node_ids,created_at FROM workflow_checkpoints WHERE execution_id=$1 ORDER BY sequence`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.WorkflowCheckpoint{}
	for rows.Next() {
		var v model.WorkflowCheckpoint
		var a, b []byte
		if err := rows.Scan(&v.ID, &v.ExecutionID, &v.Sequence, &v.Status, &v.NodeID, &a, &b, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.Variables = map[string]any{}
		v.NextNodeIDs = []string{}
		if err := json.Unmarshal(a, &v.Variables); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &v.NextNodeIDs); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
