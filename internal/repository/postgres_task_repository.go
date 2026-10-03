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

func (r *PostgresTaskRepository) Create(task model.Task) (model.Task, error) {
	const query = `
		INSERT INTO tasks (workspace_id, user_id, created_by_user_id, title, description, completed, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, workspace_id, created_by_user_id, title, description, completed, created_at, updated_at
	`

	tx, err := r.db.Begin()
	if err != nil {
		return model.Task{}, err
	}
	defer tx.Rollback()

	var legacyUserID any
	if task.PersonalWorkspace {
		legacyUserID = task.UserID
	}
	created, err := scanTask(tx.QueryRow(
		query,
		task.WorkspaceID,
		legacyUserID,
		task.UserID,
		task.Title,
		task.Description,
		task.Completed,
		task.CreatedAt,
		task.UpdatedAt,
	))
	if err != nil {
		return model.Task{}, err
	}
	if err := insertOutbox(
		tx,
		created.WorkspaceID,
		model.EventTaskCreated,
		"task",
		fmt.Sprintf("%d", created.ID),
		created,
		created.CreatedAt,
	); err != nil {
		return model.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Task{}, err
	}
	return created, nil
}

func (r *PostgresTaskRepository) FindAll(workspaceID int64, query model.TaskQuery) (model.TaskPage, error) {
	conditions := []string{`
		(
			workspace_id = $1
			OR (
				workspace_id IS NULL
				AND user_id IN (
					SELECT created_by_user_id
					FROM workspaces
					WHERE id = $1 AND is_personal = TRUE
				)
			)
		)
	`}
	args := []any{workspaceID}

	if query.Search != "" {
		args = append(args, "%"+query.Search+"%")
		conditions = append(conditions, fmt.Sprintf("(title ILIKE $%d OR description ILIKE $%d)", len(args), len(args)))
	}
	if query.Completed != nil {
		args = append(args, *query.Completed)
		conditions = append(conditions, fmt.Sprintf("completed = $%d", len(args)))
	}

	where := strings.Join(conditions, " AND ")
	var total int64
	if err := r.db.QueryRow("SELECT COUNT(*) FROM tasks WHERE "+where, args...).Scan(&total); err != nil {
		return model.TaskPage{}, err
	}

	sortColumns := map[string]string{
		"id":         "id",
		"title":      "LOWER(title)",
		"created_at": "created_at",
		"updated_at": "updated_at",
		"completed":  "completed",
	}
	sortColumn := sortColumns[query.Sort]
	order := "ASC"
	if query.Order == "desc" {
		order = "DESC"
	}

	args = append(args, query.Limit)
	limitPos := len(args)
	args = append(args, (query.Page-1)*query.Limit)
	offsetPos := len(args)

	statement := fmt.Sprintf(`
		SELECT id, COALESCE(workspace_id, $1), COALESCE(created_by_user_id, user_id), title, description, completed, created_at, updated_at
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
		Items: tasks,
		Pagination: model.Pagination{
			Page:       query.Page,
			Limit:      query.Limit,
			Total:      total,
			TotalPages: totalPages(total, query.Limit),
		},
	}, nil
}

func (r *PostgresTaskRepository) FindByID(workspaceID, id int64) (model.Task, error) {
	const query = `
		SELECT id, COALESCE(workspace_id, $2), COALESCE(created_by_user_id, user_id), title, description, completed, created_at, updated_at
		FROM tasks
		WHERE id = $1
		  AND (
		    workspace_id = $2
		    OR (
		      workspace_id IS NULL
		      AND user_id IN (
		        SELECT created_by_user_id
		        FROM workspaces
		        WHERE id = $2 AND is_personal = TRUE
		      )
		    )
		  )
	`

	task, err := scanTask(r.db.QueryRow(query, id, workspaceID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Task{}, ErrTaskNotFound
	}
	return task, err
}

func (r *PostgresTaskRepository) Update(task model.Task) (model.Task, error) {
	const query = `
		UPDATE tasks
		SET workspace_id = COALESCE(workspace_id, $2),
			title = $3,
			description = $4,
			completed = $5,
			updated_at = $6
		WHERE id = $1
		  AND (
		    workspace_id = $2
		    OR (
		      workspace_id IS NULL
		      AND user_id IN (
		        SELECT created_by_user_id
		        FROM workspaces
		        WHERE id = $2 AND is_personal = TRUE
		      )
		    )
		  )
		RETURNING id, workspace_id, COALESCE(created_by_user_id, user_id), title, description, completed, created_at, updated_at
	`

	tx, err := r.db.Begin()
	if err != nil {
		return model.Task{}, err
	}
	defer tx.Rollback()

	updated, err := scanTask(tx.QueryRow(
		query,
		task.ID,
		task.WorkspaceID,
		task.Title,
		task.Description,
		task.Completed,
		task.UpdatedAt,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Task{}, ErrTaskNotFound
	}
	if err != nil {
		return model.Task{}, err
	}
	if err := insertOutbox(
		tx,
		updated.WorkspaceID,
		model.EventTaskUpdated,
		"task",
		fmt.Sprintf("%d", updated.ID),
		updated,
		updated.UpdatedAt,
	); err != nil {
		return model.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Task{}, err
	}
	return updated, nil
}

func (r *PostgresTaskRepository) Delete(workspaceID, id int64) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	task, err := scanTask(tx.QueryRow(`
		SELECT id, COALESCE(workspace_id, $2), COALESCE(created_by_user_id, user_id),
		       title, description, completed, created_at, updated_at
		FROM tasks
		WHERE id = $1
		  AND (
		    workspace_id = $2
		    OR (
		      workspace_id IS NULL
		      AND user_id IN (
		        SELECT created_by_user_id
		        FROM workspaces
		        WHERE id = $2 AND is_personal = TRUE
		      )
		    )
		  )
		FOR UPDATE
	`, id, workspaceID))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTaskNotFound
	}
	if err != nil {
		return err
	}

	result, err := tx.Exec(`
		DELETE FROM tasks
		WHERE id = $1
		  AND (
		    workspace_id = $2
		    OR (
		      workspace_id IS NULL
		      AND user_id IN (
		        SELECT created_by_user_id
		        FROM workspaces
		        WHERE id = $2 AND is_personal = TRUE
		      )
		    )
		  )
	`, id, workspaceID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrTaskNotFound
	}

	if err := insertOutbox(
		tx,
		task.WorkspaceID,
		model.EventTaskDeleted,
		"task",
		fmt.Sprintf("%d", task.ID),
		task,
		time.Now(),
	); err != nil {
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
		&task.ID,
		&task.WorkspaceID,
		&task.UserID,
		&task.Title,
		&task.Description,
		&task.Completed,
		&task.CreatedAt,
		&task.UpdatedAt,
	)
	return task, err
}
