package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

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
		INSERT INTO tasks (user_id, title, description, completed, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, user_id, title, description, completed, created_at, updated_at
	`

	return scanTask(r.db.QueryRow(
		query,
		task.UserID,
		task.Title,
		task.Description,
		task.Completed,
		task.CreatedAt,
		task.UpdatedAt,
	))
}

func (r *PostgresTaskRepository) FindAll(userID int64, query model.TaskQuery) (model.TaskPage, error) {
	conditions := []string{"user_id = $1"}
	args := []any{userID}

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
		SELECT id, user_id, title, description, completed, created_at, updated_at
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

func (r *PostgresTaskRepository) FindByID(userID, id int64) (model.Task, error) {
	const query = `
		SELECT id, user_id, title, description, completed, created_at, updated_at
		FROM tasks
		WHERE id = $1 AND user_id = $2
	`

	task, err := scanTask(r.db.QueryRow(query, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Task{}, ErrTaskNotFound
	}
	return task, err
}

func (r *PostgresTaskRepository) Update(task model.Task) (model.Task, error) {
	const query = `
		UPDATE tasks
		SET title = $3,
			description = $4,
			completed = $5,
			updated_at = $6
		WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, title, description, completed, created_at, updated_at
	`

	updated, err := scanTask(r.db.QueryRow(
		query,
		task.ID,
		task.UserID,
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

func (r *PostgresTaskRepository) Delete(userID, id int64) error {
	result, err := r.db.Exec(`DELETE FROM tasks WHERE id = $1 AND user_id = $2`, id, userID)
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

	return nil
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(scanner taskScanner) (model.Task, error) {
	var task model.Task
	err := scanner.Scan(
		&task.ID,
		&task.UserID,
		&task.Title,
		&task.Description,
		&task.Completed,
		&task.CreatedAt,
		&task.UpdatedAt,
	)
	return task, err
}
