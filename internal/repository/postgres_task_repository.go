package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresTaskRepository struct {
	db *sql.DB
}

func NewPostgresTaskRepository(db *sql.DB) *PostgresTaskRepository {
	return &PostgresTaskRepository{db: db}
}

const taskColumns = `
	id, COALESCE(workspace_id, $WORKSPACE), COALESCE(created_by_user_id, user_id),
	title, description, completed, status, priority, start_at, due_at, completed_at,
	project_id, list_id, parent_task_id, position, estimated_minutes, actual_minutes,
	archived_at, deleted_at, version, created_at, updated_at
`

func (r *PostgresTaskRepository) Create(task model.Task) (model.Task, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return model.Task{}, err
	}
	defer tx.Rollback()

	var legacyUserID any
	if task.PersonalWorkspace {
		legacyUserID = task.UserID
	}
	created, err := scanTask(tx.QueryRow(`
		INSERT INTO tasks (
			workspace_id, user_id, created_by_user_id, title, description, completed,
			status, priority, start_at, due_at, completed_at, project_id, list_id,
			parent_task_id, position, estimated_minutes, actual_minutes, archived_at,
			deleted_at, version, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22
		)
		RETURNING id, workspace_id, created_by_user_id, title, description, completed,
			status, priority, start_at, due_at, completed_at, project_id, list_id,
			parent_task_id, position, estimated_minutes, actual_minutes, archived_at,
			deleted_at, version, created_at, updated_at
	`,
		task.WorkspaceID, legacyUserID, task.UserID, task.Title, task.Description, task.Completed,
		task.Status, task.Priority, task.StartAt, task.DueAt, task.CompletedAt, task.ProjectID,
		task.ListID, task.ParentTaskID, task.Position, task.EstimatedMinutes, task.ActualMinutes,
		task.ArchivedAt, task.DeletedAt, 1, task.CreatedAt, task.UpdatedAt,
	))
	if err != nil {
		return model.Task{}, err
	}
	if err := insertOutbox(tx, created.WorkspaceID, model.EventTaskCreated, "task", fmt.Sprint(created.ID), created, created.CreatedAt); err != nil {
		return model.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Task{}, err
	}
	return created, nil
}

func (r *PostgresTaskRepository) FindAll(workspaceID int64, query model.TaskQuery) (model.TaskPage, error) {
	conditions := []string{`(
		workspace_id = $1
		OR (
			workspace_id IS NULL
			AND user_id IN (
				SELECT created_by_user_id FROM workspaces
				WHERE id = $1 AND is_personal = TRUE
			)
		)
	)`}
	args := []any{workspaceID}

	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if query.Search != "" {
		args = append(args, "%"+query.Search+"%")
		pos := len(args)
		conditions = append(conditions, fmt.Sprintf("(title ILIKE $%d OR description ILIKE $%d)", pos, pos))
	}
	if query.Completed != nil {
		add("completed = $%d", *query.Completed)
	}
	if query.Status != "" {
		add("status = $%d", query.Status)
	}
	if query.Priority != "" {
		add("priority = $%d", query.Priority)
	}
	if query.ProjectID != nil {
		add("project_id = $%d", *query.ProjectID)
	}
	if query.ListID != nil {
		add("list_id = $%d", *query.ListID)
	}
	if query.AssigneeID != nil {
		add("EXISTS (SELECT 1 FROM task_assignees ta WHERE ta.task_id = tasks.id AND ta.user_id = $%d)", *query.AssigneeID)
	}
	if query.LabelID != nil {
		add("EXISTS (SELECT 1 FROM task_label_links tl WHERE tl.task_id = tasks.id AND tl.label_id = $%d)", *query.LabelID)
	}
	if query.Archived != nil && *query.Archived {
		conditions = append(conditions, "archived_at IS NOT NULL")
	} else {
		conditions = append(conditions, "archived_at IS NULL")
	}
	if query.Deleted != nil && *query.Deleted {
		conditions = append(conditions, "deleted_at IS NOT NULL")
	} else {
		conditions = append(conditions, "deleted_at IS NULL")
	}

	where := strings.Join(conditions, " AND ")
	var total int64
	if err := r.db.QueryRow("SELECT COUNT(*) FROM tasks WHERE "+where, args...).Scan(&total); err != nil {
		return model.TaskPage{}, err
	}

	sortColumns := map[string]string{
		"id": "id", "title": "LOWER(title)", "created_at": "created_at",
		"updated_at": "updated_at", "completed": "completed", "status": "status",
		"priority": `CASE priority WHEN 'LOW' THEN 1 WHEN 'MEDIUM' THEN 2 WHEN 'HIGH' THEN 3 WHEN 'URGENT' THEN 4 ELSE 0 END`,
		"due_at":   "due_at", "position": "position",
	}
	sortColumn := sortColumns[query.Sort]
	if sortColumn == "" {
		sortColumn = "created_at"
	}
	order := "ASC"
	if query.Order == "desc" {
		order = "DESC"
	}

	args = append(args, query.Limit)
	limitPos := len(args)
	args = append(args, (query.Page-1)*query.Limit)
	offsetPos := len(args)
	statement := fmt.Sprintf(`
		SELECT id, COALESCE(workspace_id, $1), COALESCE(created_by_user_id, user_id),
			title, description, completed, status, priority, start_at, due_at, completed_at,
			project_id, list_id, parent_task_id, position, estimated_minutes, actual_minutes,
			archived_at, deleted_at, version, created_at, updated_at
		FROM tasks
		WHERE %s
		ORDER BY %s %s, id %s
		LIMIT $%d OFFSET $%d
	`, where, sortColumn, order, order, limitPos, offsetPos)

	rows, err := r.db.Query(statement, args...)
	if err != nil {
		return model.TaskPage{}, err
	}
	defer rows.Close()
	tasks := make([]model.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return model.TaskPage{}, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return model.TaskPage{}, err
	}
	return model.TaskPage{
		Items:      tasks,
		Pagination: model.Pagination{Page: query.Page, Limit: query.Limit, Total: total, TotalPages: totalPages(total, query.Limit)},
	}, nil
}

func (r *PostgresTaskRepository) FindByID(workspaceID, id int64) (model.Task, error) {
	task, err := scanTask(r.db.QueryRow(`
		SELECT id, COALESCE(workspace_id, $2), COALESCE(created_by_user_id, user_id),
			title, description, completed, status, priority, start_at, due_at, completed_at,
			project_id, list_id, parent_task_id, position, estimated_minutes, actual_minutes,
			archived_at, deleted_at, version, created_at, updated_at
		FROM tasks
		WHERE id = $1
		  AND (
			workspace_id = $2
			OR (
				workspace_id IS NULL
				AND user_id IN (
					SELECT created_by_user_id FROM workspaces
					WHERE id = $2 AND is_personal = TRUE
				)
			)
		  )
	`, id, workspaceID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Task{}, ErrTaskNotFound
	}
	return task, err
}

func (r *PostgresTaskRepository) Update(task model.Task) (model.Task, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return model.Task{}, err
	}
	defer tx.Rollback()

	updated, err := scanTask(tx.QueryRow(`
		UPDATE tasks
		SET workspace_id = COALESCE(workspace_id, $2),
			title = $3, description = $4, completed = $5, status = $6, priority = $7,
			start_at = $8, due_at = $9, completed_at = $10, project_id = $11,
			list_id = $12, parent_task_id = $13, position = $14,
			estimated_minutes = $15, actual_minutes = $16, archived_at = $17,
			deleted_at = $18, version = version + 1, updated_at = $19
		WHERE id = $1 AND version = $20
		  AND (
			workspace_id = $2
			OR (
				workspace_id IS NULL
				AND user_id IN (
					SELECT created_by_user_id FROM workspaces
					WHERE id = $2 AND is_personal = TRUE
				)
			)
		  )
		RETURNING id, workspace_id, COALESCE(created_by_user_id, user_id),
			title, description, completed, status, priority, start_at, due_at, completed_at,
			project_id, list_id, parent_task_id, position, estimated_minutes, actual_minutes,
			archived_at, deleted_at, version, created_at, updated_at
	`,
		task.ID, task.WorkspaceID, task.Title, task.Description, task.Completed,
		task.Status, task.Priority, task.StartAt, task.DueAt, task.CompletedAt,
		task.ProjectID, task.ListID, task.ParentTaskID, task.Position,
		task.EstimatedMinutes, task.ActualMinutes, task.ArchivedAt, task.DeletedAt,
		task.UpdatedAt, task.Version,
	))
	if errors.Is(err, sql.ErrNoRows) {
		var exists bool
		if checkErr := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM tasks WHERE id = $1 AND workspace_id = $2)", task.ID, task.WorkspaceID).Scan(&exists); checkErr != nil {
			return model.Task{}, checkErr
		}
		if exists {
			return model.Task{}, ErrTaskVersionConflict
		}
		return model.Task{}, ErrTaskNotFound
	}
	if err != nil {
		return model.Task{}, err
	}
	if err := insertOutbox(tx, updated.WorkspaceID, model.EventTaskUpdated, "task", fmt.Sprint(updated.ID), updated, updated.UpdatedAt); err != nil {
		return model.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Task{}, err
	}
	return updated, nil
}

func (r *PostgresTaskRepository) SoftDelete(workspaceID, id int64, at time.Time) (model.Task, error) {
	return r.updateLifecycleState(workspaceID, id, "deleted_at = $3", at)
}

func (r *PostgresTaskRepository) Restore(workspaceID, id int64, at time.Time) (model.Task, error) {
	return r.updateLifecycleState(workspaceID, id, `deleted_at = NULL, archived_at = NULL,
		status = CASE WHEN status = 'ARCHIVED' THEN 'TODO' ELSE status END,
		completed = CASE WHEN status = 'ARCHIVED' THEN FALSE ELSE completed END,
		completed_at = CASE WHEN status = 'ARCHIVED' THEN NULL ELSE completed_at END`, at)
}

func (r *PostgresTaskRepository) Archive(workspaceID, id int64, archived bool, at time.Time) (model.Task, error) {
	if archived {
		return r.updateLifecycleState(workspaceID, id, "archived_at = $3, status = 'ARCHIVED'", at)
	}
	return r.updateLifecycleState(workspaceID, id, `archived_at = NULL,
		status = CASE WHEN status = 'ARCHIVED' THEN 'TODO' ELSE status END,
		completed = CASE WHEN status = 'ARCHIVED' THEN FALSE ELSE completed END,
		completed_at = CASE WHEN status = 'ARCHIVED' THEN NULL ELSE completed_at END`, at)
}

func (r *PostgresTaskRepository) updateLifecycleState(workspaceID, id int64, mutation string, at time.Time) (model.Task, error) {
	query := fmt.Sprintf(`
		UPDATE tasks
		SET %s, updated_at = $3, version = version + 1
		WHERE id = $1 AND workspace_id = $2
		RETURNING id, workspace_id, COALESCE(created_by_user_id, user_id),
			title, description, completed, status, priority, start_at, due_at, completed_at,
			project_id, list_id, parent_task_id, position, estimated_minutes, actual_minutes,
			archived_at, deleted_at, version, created_at, updated_at
	`, mutation)
	task, err := scanTask(r.db.QueryRow(query, id, workspaceID, at))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Task{}, ErrTaskNotFound
	}
	return task, err
}

func (r *PostgresTaskRepository) Delete(workspaceID, id int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	task, err := scanTask(tx.QueryRow(`
		SELECT id, COALESCE(workspace_id, $2), COALESCE(created_by_user_id, user_id),
			title, description, completed, status, priority, start_at, due_at, completed_at,
			project_id, list_id, parent_task_id, position, estimated_minutes, actual_minutes,
			archived_at, deleted_at, version, created_at, updated_at
		FROM tasks
		WHERE id = $1 AND workspace_id = $2
		FOR UPDATE
	`, id, workspaceID))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTaskNotFound
	}
	if err != nil {
		return err
	}
	result, err := tx.Exec("DELETE FROM tasks WHERE id = $1 AND workspace_id = $2", id, workspaceID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrTaskNotFound
	}
	if err := insertOutbox(tx, task.WorkspaceID, model.EventTaskDeleted, "task", fmt.Sprint(task.ID), task, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(scanner taskScanner) (model.Task, error) {
	var task model.Task
	err := scanner.Scan(
		&task.ID, &task.WorkspaceID, &task.UserID, &task.Title, &task.Description,
		&task.Completed, &task.Status, &task.Priority, &task.StartAt, &task.DueAt,
		&task.CompletedAt, &task.ProjectID, &task.ListID, &task.ParentTaskID,
		&task.Position, &task.EstimatedMinutes, &task.ActualMinutes, &task.ArchivedAt,
		&task.DeletedAt, &task.Version, &task.CreatedAt, &task.UpdatedAt,
	)
	return task, err
}
