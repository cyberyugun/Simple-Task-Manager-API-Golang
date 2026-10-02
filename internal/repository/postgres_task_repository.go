package repository

import (
	"database/sql"
	"errors"

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
		INSERT INTO tasks (title, description, completed, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, title, description, completed, created_at, updated_at
	`

	return scanTask(r.db.QueryRow(
		query,
		task.Title,
		task.Description,
		task.Completed,
		task.CreatedAt,
		task.UpdatedAt,
	))
}

func (r *PostgresTaskRepository) FindAll() ([]model.Task, error) {
	const query = `
		SELECT id, title, description, completed, created_at, updated_at
		FROM tasks
		ORDER BY id ASC
	`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]model.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tasks, nil
}

func (r *PostgresTaskRepository) FindByID(id int64) (model.Task, error) {
	const query = `
		SELECT id, title, description, completed, created_at, updated_at
		FROM tasks
		WHERE id = $1
	`

	task, err := scanTask(r.db.QueryRow(query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Task{}, ErrTaskNotFound
	}
	return task, err
}

func (r *PostgresTaskRepository) Update(task model.Task) (model.Task, error) {
	const query = `
		UPDATE tasks
		SET title = $2,
			description = $3,
			completed = $4,
			updated_at = $5
		WHERE id = $1
		RETURNING id, title, description, completed, created_at, updated_at
	`

	updated, err := scanTask(r.db.QueryRow(
		query,
		task.ID,
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

func (r *PostgresTaskRepository) Delete(id int64) error {
	result, err := r.db.Exec(`DELETE FROM tasks WHERE id = $1`, id)
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
		&task.Title,
		&task.Description,
		&task.Completed,
		&task.CreatedAt,
		&task.UpdatedAt,
	)
	return task, err
}
