package repository

import (
	"encoding/json"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresLifecycleRepository struct {
	db queryExecutor
}

type queryExecutor interface {
	Exec(query string, args ...any) (sqlResult, error)
	Query(query string, args ...any) (rowsScanner, error)
	QueryRow(query string, args ...any) rowScanner
}

type sqlResult interface {
	RowsAffected() (int64, error)
}

type rowScanner interface {
	Scan(dest ...any) error
}

type rowsScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

func NewPostgresLifecycleRepository(db interface {
	Exec(query string, args ...any) (sqlResult, error)
	Query(query string, args ...any) (rowsScanner, error)
	QueryRow(query string, args ...any) rowScanner
}) *PostgresLifecycleRepository {
	return &PostgresLifecycleRepository{db: db}
}
