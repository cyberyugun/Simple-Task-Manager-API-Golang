package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresTaskCollaborationRepository struct {
	db *sql.DB
}

func NewPostgresTaskCollaborationRepository(db *sql.DB) *PostgresTaskCollaborationRepository {
	return &PostgresTaskCollaborationRepository{db: db}
}

func (r *PostgresTaskCollaborationRepository) CreateProject(project model.TaskProject) (model.TaskProject, error) {
	var item model.TaskProject
	err := r.db.QueryRow(`
		INSERT INTO task_projects (
			workspace_id, name, description, created_by_user_id, archived_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, workspace_id, name, description, created_by_user_id, archived_at, created_at, updated_at
	`, project.WorkspaceID, project.Name, project.Description, project.CreatedByUserID,
		project.ArchivedAt, project.CreatedAt, project.UpdatedAt,
	).Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Description, &item.CreatedByUserID,
		&item.ArchivedAt, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PostgresTaskCollaborationRepository) ListProjects(workspaceID int64, includeArchived bool) ([]model.TaskProject, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, name, description, created_by_user_id, archived_at, created_at, updated_at
		FROM task_projects
		WHERE workspace_id = $1 AND ($2 OR archived_at IS NULL)
		ORDER BY id
	`, workspaceID, includeArchived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TaskProject, 0)
	for rows.Next() {
		var item model.TaskProject
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Description,
			&item.CreatedByUserID, &item.ArchivedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresTaskCollaborationRepository) ArchiveProject(workspaceID, projectID int64, archived bool, at time.Time) (model.TaskProject, error) {
	var archivedAt *time.Time
	if archived {
		archivedAt = &at
	}
	var item model.TaskProject
	err := r.db.QueryRow(`
		UPDATE task_projects SET archived_at = $3, updated_at = $4
		WHERE workspace_id = $1 AND id = $2
		RETURNING id, workspace_id, name, description, created_by_user_id, archived_at, created_at, updated_at
	`, workspaceID, projectID, archivedAt, at).Scan(
		&item.ID, &item.WorkspaceID, &item.Name, &item.Description, &item.CreatedByUserID,
		&item.ArchivedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.TaskProject{}, ErrTaskProjectNotFound
	}
	return item, err
}

func (r *PostgresTaskCollaborationRepository) CreateList(list model.TaskList) (model.TaskList, error) {
	var item model.TaskList
	err := r.db.QueryRow(`
		INSERT INTO task_lists (
			workspace_id, project_id, name, position, created_by_user_id, archived_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, workspace_id, project_id, name, position, created_by_user_id, archived_at, created_at, updated_at
	`, list.WorkspaceID, list.ProjectID, list.Name, list.Position, list.CreatedByUserID,
		list.ArchivedAt, list.CreatedAt, list.UpdatedAt,
	).Scan(&item.ID, &item.WorkspaceID, &item.ProjectID, &item.Name, &item.Position,
		&item.CreatedByUserID, &item.ArchivedAt, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PostgresTaskCollaborationRepository) ListLists(workspaceID int64, projectID *int64, includeArchived bool) ([]model.TaskList, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, project_id, name, position, created_by_user_id, archived_at, created_at, updated_at
		FROM task_lists
		WHERE workspace_id = $1
		  AND ($2::bigint IS NULL OR project_id = $2)
		  AND ($3 OR archived_at IS NULL)
		ORDER BY position, id
	`, workspaceID, projectID, includeArchived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TaskList, 0)
	for rows.Next() {
		var item model.TaskList
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.ProjectID, &item.Name, &item.Position,
			&item.CreatedByUserID, &item.ArchivedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresTaskCollaborationRepository) ArchiveList(workspaceID, listID int64, archived bool, at time.Time) (model.TaskList, error) {
	var archivedAt *time.Time
	if archived {
		archivedAt = &at
	}
	var item model.TaskList
	err := r.db.QueryRow(`
		UPDATE task_lists SET archived_at = $3, updated_at = $4
		WHERE workspace_id = $1 AND id = $2
		RETURNING id, workspace_id, project_id, name, position, created_by_user_id, archived_at, created_at, updated_at
	`, workspaceID, listID, archivedAt, at).Scan(
		&item.ID, &item.WorkspaceID, &item.ProjectID, &item.Name, &item.Position,
		&item.CreatedByUserID, &item.ArchivedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.TaskList{}, ErrTaskListNotFound
	}
	return item, err
}

func (r *PostgresTaskCollaborationRepository) CreateLabel(label model.TaskLabel) (model.TaskLabel, error) {
	var item model.TaskLabel
	err := r.db.QueryRow(`
		INSERT INTO task_labels (workspace_id, name, color, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id, workspace_id, name, color, created_at, updated_at
	`, label.WorkspaceID, label.Name, label.Color, label.CreatedAt, label.UpdatedAt).Scan(
		&item.ID, &item.WorkspaceID, &item.Name, &item.Color, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil && isUniqueViolation(err) {
		return model.TaskLabel{}, ErrTaskRelationExists
	}
	return item, err
}

func (r *PostgresTaskCollaborationRepository) ListLabels(workspaceID int64) ([]model.TaskLabel, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, name, color, created_at, updated_at
		FROM task_labels WHERE workspace_id = $1 ORDER BY lower(name), id
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TaskLabel, 0)
	for rows.Next() {
		var item model.TaskLabel
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Color, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresTaskCollaborationRepository) AddTaskLabel(workspaceID, taskID, labelID int64, at time.Time) error {
	result, err := r.db.Exec(`
		INSERT INTO task_label_links (task_id, label_id, created_at)
		SELECT t.id, l.id, $4
		FROM tasks t
		JOIN task_labels l ON l.id = $3 AND l.workspace_id = $1
		WHERE t.id = $2 AND t.workspace_id = $1
		ON CONFLICT (task_id, label_id) DO NOTHING
	`, workspaceID, taskID, labelID, at)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrTaskRelationExists
	}
	return nil
}

func (r *PostgresTaskCollaborationRepository) RemoveTaskLabel(workspaceID, taskID, labelID int64) error {
	result, err := r.db.Exec(`
		DELETE FROM task_label_links tl
		USING tasks t, task_labels l
		WHERE tl.task_id = t.id AND tl.label_id = l.id
		  AND t.workspace_id = $1 AND l.workspace_id = $1
		  AND t.id = $2 AND l.id = $3
	`, workspaceID, taskID, labelID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrTaskLabelNotFound
	}
	return nil
}

func (r *PostgresTaskCollaborationRepository) ListTaskLabels(workspaceID, taskID int64) ([]model.TaskLabel, error) {
	rows, err := r.db.Query(`
		SELECT l.id, l.workspace_id, l.name, l.color, l.created_at, l.updated_at
		FROM task_label_links tl
		JOIN task_labels l ON l.id = tl.label_id
		JOIN tasks t ON t.id = tl.task_id
		WHERE t.workspace_id = $1 AND t.id = $2 AND l.workspace_id = $1
		ORDER BY l.id
	`, workspaceID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TaskLabel, 0)
	for rows.Next() {
		var item model.TaskLabel
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Color, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresTaskCollaborationRepository) AddAssignee(workspaceID, taskID, userID, assignedBy int64, at time.Time) error {
	result, err := r.db.Exec(`
		INSERT INTO task_assignees (task_id, user_id, assigned_by_user_id, created_at)
		SELECT t.id, $3, $4, $5
		FROM tasks t
		JOIN workspace_members m ON m.workspace_id = t.workspace_id AND m.user_id = $3
		WHERE t.id = $2 AND t.workspace_id = $1
		ON CONFLICT (task_id, user_id) DO NOTHING
	`, workspaceID, taskID, userID, assignedBy, at)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrTaskRelationExists
	}
	return nil
}

func (r *PostgresTaskCollaborationRepository) RemoveAssignee(workspaceID, taskID, userID int64) error {
	result, err := r.db.Exec(`
		DELETE FROM task_assignees ta
		USING tasks t
		WHERE ta.task_id = t.id AND t.workspace_id = $1 AND t.id = $2 AND ta.user_id = $3
	`, workspaceID, taskID, userID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrWorkspaceMemberNotFound
	}
	return nil
}

func (r *PostgresTaskCollaborationRepository) ListAssignees(workspaceID, taskID int64) ([]int64, error) {
	return r.listTaskUsers("task_assignees", workspaceID, taskID)
}

func (r *PostgresTaskCollaborationRepository) AddWatcher(workspaceID, taskID, userID int64, at time.Time) error {
	result, err := r.db.Exec(`
		INSERT INTO task_watchers (task_id, user_id, created_at)
		SELECT t.id, $3, $4
		FROM tasks t
		JOIN workspace_members m ON m.workspace_id = t.workspace_id AND m.user_id = $3
		WHERE t.id = $2 AND t.workspace_id = $1
		ON CONFLICT (task_id, user_id) DO NOTHING
	`, workspaceID, taskID, userID, at)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrTaskRelationExists
	}
	return nil
}

func (r *PostgresTaskCollaborationRepository) RemoveWatcher(workspaceID, taskID, userID int64) error {
	result, err := r.db.Exec(`
		DELETE FROM task_watchers tw
		USING tasks t
		WHERE tw.task_id = t.id AND t.workspace_id = $1 AND t.id = $2 AND tw.user_id = $3
	`, workspaceID, taskID, userID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrWorkspaceMemberNotFound
	}
	return nil
}

func (r *PostgresTaskCollaborationRepository) ListWatchers(workspaceID, taskID int64) ([]int64, error) {
	return r.listTaskUsers("task_watchers", workspaceID, taskID)
}

func (r *PostgresTaskCollaborationRepository) listTaskUsers(table string, workspaceID, taskID int64) ([]int64, error) {
	query := fmt.Sprintf(`
		SELECT rel.user_id
		FROM %s rel
		JOIN tasks t ON t.id = rel.task_id
		WHERE t.workspace_id = $1 AND t.id = $2
		ORDER BY rel.user_id
	`, table)
	rows, err := r.db.Query(query, workspaceID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]int64, 0)
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		items = append(items, userID)
	}
	return items, rows.Err()
}

func (r *PostgresTaskCollaborationRepository) CreateComment(comment model.TaskComment) (model.TaskComment, error) {
	var item model.TaskComment
	err := r.db.QueryRow(`
		INSERT INTO task_comments (
			task_id, workspace_id, user_id, body, edited_at, deleted_at, created_at, updated_at
		)
		SELECT t.id, $2, $3, $4, $5, $6, $7, $8
		FROM tasks t
		WHERE t.id = $1 AND t.workspace_id = $2
		RETURNING id, task_id, workspace_id, user_id, body, edited_at, deleted_at, created_at, updated_at
	`, comment.TaskID, comment.WorkspaceID, comment.UserID, comment.Body, comment.EditedAt,
		comment.DeletedAt, comment.CreatedAt, comment.UpdatedAt,
	).Scan(&item.ID, &item.TaskID, &item.WorkspaceID, &item.UserID, &item.Body,
		&item.EditedAt, &item.DeletedAt, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.TaskComment{}, ErrTaskNotFound
	}
	return item, err
}

func (r *PostgresTaskCollaborationRepository) UpdateComment(workspaceID, taskID, commentID, userID int64, body string, at time.Time) (model.TaskComment, error) {
	var item model.TaskComment
	err := r.db.QueryRow(`
		UPDATE task_comments
		SET body = $5, edited_at = $6, updated_at = $6
		WHERE workspace_id = $1 AND task_id = $2 AND id = $3 AND user_id = $4 AND deleted_at IS NULL
		RETURNING id, task_id, workspace_id, user_id, body, edited_at, deleted_at, created_at, updated_at
	`, workspaceID, taskID, commentID, userID, body, at).Scan(
		&item.ID, &item.TaskID, &item.WorkspaceID, &item.UserID, &item.Body,
		&item.EditedAt, &item.DeletedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.TaskComment{}, ErrTaskCommentNotFound
	}
	return item, err
}

func (r *PostgresTaskCollaborationRepository) DeleteComment(workspaceID, taskID, commentID, userID int64, at time.Time) (model.TaskComment, error) {
	var item model.TaskComment
	err := r.db.QueryRow(`
		UPDATE task_comments
		SET deleted_at = $5, updated_at = $5
		WHERE workspace_id = $1 AND task_id = $2 AND id = $3 AND user_id = $4 AND deleted_at IS NULL
		RETURNING id, task_id, workspace_id, user_id, body, edited_at, deleted_at, created_at, updated_at
	`, workspaceID, taskID, commentID, userID, at).Scan(
		&item.ID, &item.TaskID, &item.WorkspaceID, &item.UserID, &item.Body,
		&item.EditedAt, &item.DeletedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.TaskComment{}, ErrTaskCommentNotFound
	}
	return item, err
}

func (r *PostgresTaskCollaborationRepository) ListComments(workspaceID, taskID int64) ([]model.TaskComment, error) {
	rows, err := r.db.Query(`
		SELECT id, task_id, workspace_id, user_id, body, edited_at, deleted_at, created_at, updated_at
		FROM task_comments
		WHERE workspace_id = $1 AND task_id = $2 AND deleted_at IS NULL
		ORDER BY created_at, id
	`, workspaceID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TaskComment, 0)
	for rows.Next() {
		var item model.TaskComment
		if err := rows.Scan(&item.ID, &item.TaskID, &item.WorkspaceID, &item.UserID,
			&item.Body, &item.EditedAt, &item.DeletedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresTaskCollaborationRepository) CreateDependency(workspaceID int64, dependency model.TaskDependency) (model.TaskDependency, error) {
	var item model.TaskDependency
	err := r.db.QueryRow(`
		INSERT INTO task_dependencies (task_id, depends_on_task_id, created_by_user_id, created_at)
		SELECT t.id, d.id, $4, $5
		FROM tasks t
		JOIN tasks d ON d.id = $3 AND d.workspace_id = $1
		WHERE t.id = $2 AND t.workspace_id = $1
		RETURNING task_id, depends_on_task_id, created_by_user_id, created_at
	`, workspaceID, dependency.TaskID, dependency.DependsOnTaskID,
		dependency.CreatedByUserID, dependency.CreatedAt,
	).Scan(&item.TaskID, &item.DependsOnTaskID, &item.CreatedByUserID, &item.CreatedAt)
	if err != nil && isUniqueViolation(err) {
		return model.TaskDependency{}, ErrTaskRelationExists
	}
	if errors.Is(err, sql.ErrNoRows) {
		return model.TaskDependency{}, ErrTaskNotFound
	}
	return item, err
}

func (r *PostgresTaskCollaborationRepository) DeleteDependency(workspaceID, taskID, dependsOnTaskID int64) error {
	result, err := r.db.Exec(`
		DELETE FROM task_dependencies dep
		USING tasks t
		WHERE dep.task_id = t.id AND t.workspace_id = $1
		  AND dep.task_id = $2 AND dep.depends_on_task_id = $3
	`, workspaceID, taskID, dependsOnTaskID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrTaskDependencyNotFound
	}
	return nil
}

func (r *PostgresTaskCollaborationRepository) ListDependencies(workspaceID, taskID int64) ([]model.TaskDependency, error) {
	rows, err := r.db.Query(`
		SELECT dep.task_id, dep.depends_on_task_id, dep.created_by_user_id, dep.created_at
		FROM task_dependencies dep
		JOIN tasks t ON t.id = dep.task_id
		WHERE t.workspace_id = $1 AND dep.task_id = $2
		ORDER BY dep.depends_on_task_id
	`, workspaceID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TaskDependency, 0)
	for rows.Next() {
		var item model.TaskDependency
		if err := rows.Scan(&item.TaskID, &item.DependsOnTaskID, &item.CreatedByUserID, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresTaskCollaborationRepository) SetRecurrence(rule model.TaskRecurrenceRule) (model.TaskRecurrenceRule, error) {
	var item model.TaskRecurrenceRule
	err := r.db.QueryRow(`
		INSERT INTO task_recurrence_rules (
			task_id, frequency, interval_count, timezone, next_run_at, active, created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (task_id) DO UPDATE SET
			frequency = EXCLUDED.frequency,
			interval_count = EXCLUDED.interval_count,
			timezone = EXCLUDED.timezone,
			next_run_at = EXCLUDED.next_run_at,
			active = EXCLUDED.active,
			updated_at = EXCLUDED.updated_at
		RETURNING task_id, frequency, interval_count, timezone, next_run_at, active, created_at, updated_at
	`, rule.TaskID, rule.Frequency, rule.IntervalCount, rule.Timezone, rule.NextRunAt,
		rule.Active, rule.CreatedAt, rule.UpdatedAt,
	).Scan(&item.TaskID, &item.Frequency, &item.IntervalCount, &item.Timezone,
		&item.NextRunAt, &item.Active, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PostgresTaskCollaborationRepository) GetRecurrence(workspaceID, taskID int64) (model.TaskRecurrenceRule, error) {
	var item model.TaskRecurrenceRule
	err := r.db.QueryRow(`
		SELECT r.task_id, r.frequency, r.interval_count, r.timezone, r.next_run_at,
			r.active, r.created_at, r.updated_at
		FROM task_recurrence_rules r
		JOIN tasks t ON t.id = r.task_id
		WHERE t.workspace_id = $1 AND r.task_id = $2
	`, workspaceID, taskID).Scan(
		&item.TaskID, &item.Frequency, &item.IntervalCount, &item.Timezone,
		&item.NextRunAt, &item.Active, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.TaskRecurrenceRule{}, ErrTaskRecurrenceNotFound
	}
	return item, err
}

func (r *PostgresTaskCollaborationRepository) DeleteRecurrence(workspaceID, taskID int64) error {
	result, err := r.db.Exec(`
		DELETE FROM task_recurrence_rules r
		USING tasks t
		WHERE r.task_id = t.id AND t.workspace_id = $1 AND r.task_id = $2
	`, workspaceID, taskID)
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return ErrTaskRecurrenceNotFound
	}
	return nil
}

func (r *PostgresTaskCollaborationRepository) CreateCustomField(field model.TaskCustomFieldDefinition) (model.TaskCustomFieldDefinition, error) {
	var item model.TaskCustomFieldDefinition
	err := r.db.QueryRow(`
		INSERT INTO task_custom_field_definitions (
			workspace_id, name, field_type, required, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, workspace_id, name, field_type, required, created_at, updated_at
	`, field.WorkspaceID, field.Name, field.FieldType, field.Required, field.CreatedAt, field.UpdatedAt).Scan(
		&item.ID, &item.WorkspaceID, &item.Name, &item.FieldType, &item.Required, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil && isUniqueViolation(err) {
		return model.TaskCustomFieldDefinition{}, ErrTaskRelationExists
	}
	return item, err
}

func (r *PostgresTaskCollaborationRepository) ListCustomFields(workspaceID int64) ([]model.TaskCustomFieldDefinition, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, name, field_type, required, created_at, updated_at
		FROM task_custom_field_definitions
		WHERE workspace_id = $1
		ORDER BY id
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TaskCustomFieldDefinition, 0)
	for rows.Next() {
		var item model.TaskCustomFieldDefinition
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.FieldType,
			&item.Required, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresTaskCollaborationRepository) SetCustomFieldValue(workspaceID int64, value model.TaskCustomFieldValue) (model.TaskCustomFieldValue, error) {
	raw, err := json.Marshal(value.Value)
	if err != nil {
		return model.TaskCustomFieldValue{}, err
	}
	var item model.TaskCustomFieldValue
	var out []byte
	err = r.db.QueryRow(`
		INSERT INTO task_custom_field_values (task_id, field_id, value, updated_at)
		SELECT t.id, f.id, $4::jsonb, $5
		FROM tasks t
		JOIN task_custom_field_definitions f ON f.id = $3 AND f.workspace_id = $1
		WHERE t.id = $2 AND t.workspace_id = $1
		ON CONFLICT (task_id, field_id) DO UPDATE SET
			value = EXCLUDED.value,
			updated_at = EXCLUDED.updated_at
		RETURNING task_id, field_id, value, updated_at
	`, workspaceID, value.TaskID, value.FieldID, string(raw), value.UpdatedAt).Scan(
		&item.TaskID, &item.FieldID, &out, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.TaskCustomFieldValue{}, ErrTaskCustomFieldNotFound
	}
	if err != nil {
		return model.TaskCustomFieldValue{}, err
	}
	if err := json.Unmarshal(out, &item.Value); err != nil {
		return model.TaskCustomFieldValue{}, err
	}
	return item, nil
}

func (r *PostgresTaskCollaborationRepository) ListTaskCustomFieldValues(workspaceID, taskID int64) ([]model.TaskCustomFieldValue, error) {
	rows, err := r.db.Query(`
		SELECT v.task_id, v.field_id, v.value, v.updated_at
		FROM task_custom_field_values v
		JOIN tasks t ON t.id = v.task_id
		JOIN task_custom_field_definitions f ON f.id = v.field_id
		WHERE t.workspace_id = $1 AND t.id = $2 AND f.workspace_id = $1
		ORDER BY v.field_id
	`, workspaceID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TaskCustomFieldValue, 0)
	for rows.Next() {
		var item model.TaskCustomFieldValue
		var raw []byte
		if err := rows.Scan(&item.TaskID, &item.FieldID, &raw, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Value); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresTaskCollaborationRepository) RecordActivityAndEvent(activity model.TaskActivity, eventType string, data any) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	raw, err := json.Marshal(activity.Metadata)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO task_activities (workspace_id, task_id, actor_user_id, action, metadata, created_at)
		SELECT $1, t.id, $3, $4, $5::jsonb, $6
		FROM tasks t
		WHERE t.id = $2 AND t.workspace_id = $1
	`, activity.WorkspaceID, activity.TaskID, activity.ActorUserID,
		activity.Action, string(raw), activity.CreatedAt); err != nil {
		return err
	}
	if eventType != "" {
		if err := insertOutbox(
			tx, activity.WorkspaceID, eventType, "task", fmt.Sprint(activity.TaskID),
			data, activity.CreatedAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *PostgresTaskCollaborationRepository) ListActivity(workspaceID, taskID int64, limit int) ([]model.TaskActivity, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, task_id, actor_user_id, action, metadata, created_at
		FROM task_activities
		WHERE workspace_id = $1 AND task_id = $2
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`, workspaceID, taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.TaskActivity, 0)
	for rows.Next() {
		var item model.TaskActivity
		var raw []byte
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.TaskID, &item.ActorUserID,
			&item.Action, &raw, &item.CreatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &item.Metadata); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
