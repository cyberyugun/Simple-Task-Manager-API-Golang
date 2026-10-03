package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/events"
	"go-simple-task-api/internal/model"
)

const idempotencyTTL = 24 * time.Hour

type PostgresTaskRepository struct {
	db *sql.DB
}

func NewPostgresTaskRepository(db *sql.DB) *PostgresTaskRepository {
	return &PostgresTaskRepository{db: db}
}

func (r *PostgresTaskRepository) Create(task model.Task) (model.Task, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return model.Task{}, err
	}
	defer tx.Rollback()

	created, err := insertTask(tx, task)
	if err != nil {
		return model.Task{}, err
	}
	if err := enqueueTaskEvent(tx, created, model.EventTaskCreated, created.CreatedAt); err != nil {
		return model.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Task{}, err
	}
	return created, nil
}

func (r *PostgresTaskRepository) CreateIdempotent(task model.Task, key, requestHash string, now time.Time) (model.TaskCreateResult, error) {
	if key == "" {
		created, err := r.Create(task)
		return model.TaskCreateResult{Task: created}, err
	}

	tx, err := r.db.Begin()
	if err != nil {
		return model.TaskCreateResult{}, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		DELETE FROM idempotency_keys
		WHERE workspace_id = $1 AND idempotency_key = $2 AND expires_at <= $3
	`, task.WorkspaceID, key, now); err != nil {
		return model.TaskCreateResult{}, err
	}

	result, err := tx.Exec(`
		INSERT INTO idempotency_keys (
			workspace_id, idempotency_key, request_hash, resource_type,
			resource_id, created_at, expires_at
		)
		VALUES ($1, $2, $3, 'task', '', $4, $5)
		ON CONFLICT (workspace_id, idempotency_key) DO NOTHING
	`, task.WorkspaceID, key, requestHash, now, now.Add(idempotencyTTL))
	if err != nil {
		return model.TaskCreateResult{}, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return model.TaskCreateResult{}, err
	}

	if inserted == 0 {
		var storedHash, resourceID string
		err := tx.QueryRow(`
			SELECT request_hash, resource_id
			FROM idempotency_keys
			WHERE workspace_id = $1 AND idempotency_key = $2
		`, task.WorkspaceID, key).Scan(&storedHash, &resourceID)
		if err != nil {
			return model.TaskCreateResult{}, err
		}
		if storedHash != requestHash {
			return model.TaskCreateResult{}, ErrIdempotencyConflict
		}
		id, err := strconv.ParseInt(resourceID, 10, 64)
		if err != nil || id <= 0 {
			return model.TaskCreateResult{}, fmt.Errorf("idempotency resource is incomplete")
		}
		existing, err := findTaskByID(tx, task.WorkspaceID, id)
		if err != nil {
			return model.TaskCreateResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return model.TaskCreateResult{}, err
		}
		return model.TaskCreateResult{Task: existing, Replayed: true}, nil
	}

	created, err := insertTask(tx, task)
	if err != nil {
		return model.TaskCreateResult{}, err
	}
	if err := enqueueTaskEvent(tx, created, model.EventTaskCreated, created.CreatedAt); err != nil {
		return model.TaskCreateResult{}, err
	}
	if _, err := tx.Exec(`
		UPDATE idempotency_keys
		SET resource_id = $3
		WHERE workspace_id = $1 AND idempotency_key = $2
	`, task.WorkspaceID, key, strconv.FormatInt(created.ID, 10)); err != nil {
		return model.TaskCreateResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.TaskCreateResult{}, err
	}
	return model.TaskCreateResult{Task: created}, nil
}

func insertTask(tx *sql.Tx, task model.Task) (model.Task, error) {
	const query = `
		INSERT INTO tasks (workspace_id, user_id, created_by_user_id, title, description, completed, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, workspace_id, created_by_user_id, title, description, completed, created_at, updated_at
	`

	var legacyUserID any
	if task.PersonalWorkspace {
		legacyUserID = task.UserID
	}
	return scanTask(tx.QueryRow(
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
	return findTaskByID(r.db, workspaceID, id)
}

type queryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

func findTaskByID(q queryRower, workspaceID, id int64) (model.Task, error) {
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

	task, err := scanTask(q.QueryRow(query, id, workspaceID))
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

	updated, err := updateTask(tx, task)
	if err != nil {
		return model.Task{}, err
	}
	if err := enqueueTaskEvent(tx, updated, model.EventTaskUpdated, updated.UpdatedAt); err != nil {
		return model.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Task{}, err
	}
	return updated, nil
}

func updateTask(tx *sql.Tx, task model.Task) (model.Task, error) {
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
	return updated, err
}

func (r *PostgresTaskRepository) Complete(workspaceID, id int64, updatedAt time.Time) (model.Task, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return model.Task{}, err
	}
	defer tx.Rollback()

	task, err := findTaskByID(tx, workspaceID, id)
	if err != nil {
		return model.Task{}, err
	}
	task.Completed = true
	task.UpdatedAt = updatedAt
	updated, err := updateTask(tx, task)
	if err != nil {
		return model.Task{}, err
	}
	if err := enqueueTaskEvent(tx, updated, model.EventTaskCompleted, updatedAt); err != nil {
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

	const query = `
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
		RETURNING id, COALESCE(workspace_id, $2), COALESCE(created_by_user_id, user_id), title, description, completed, created_at, updated_at
	`
	task, err := scanTask(tx.QueryRow(query, id, workspaceID))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrTaskNotFound
	}
	if err != nil {
		return err
	}
	if err := enqueueTaskEvent(tx, task, model.EventTaskDeleted, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

func enqueueTaskEvent(tx *sql.Tx, task model.Task, eventType string, occurredAt time.Time) error {
	eventID, err := events.NewEventID()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"id":                 task.ID,
		"workspace_id":       task.WorkspaceID,
		"created_by_user_id": task.UserID,
		"title":              task.Title,
		"description":        task.Description,
		"completed":          task.Completed,
		"created_at":         task.CreatedAt,
		"updated_at":         task.UpdatedAt,
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO outbox_events (
			event_id, workspace_id, aggregate_type, aggregate_id,
			event_type, schema_version, payload, occurred_at, available_at
		)
		VALUES ($1, $2, 'task', $3, $4, $5, $6::jsonb, $7, $7)
	`,
		eventID,
		task.WorkspaceID,
		strconv.FormatInt(task.ID, 10),
		eventType,
		model.EventSchemaVersion,
		string(payload),
		occurredAt,
	)
	return err
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
