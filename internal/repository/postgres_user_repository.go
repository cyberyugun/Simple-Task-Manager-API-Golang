package repository

import (
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"go-simple-task-api/internal/model"
)

type PostgresUserRepository struct {
	db *sql.DB
}

func NewPostgresUserRepository(db *sql.DB) *PostgresUserRepository {
	return &PostgresUserRepository{db: db}
}

func (r *PostgresUserRepository) Create(user model.User) (model.User, error) {
	const query = `
		INSERT INTO users (name, email, password_hash, email_verified_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, name, email, password_hash, email_verified_at, created_at, updated_at
	`

	created, err := scanUser(r.db.QueryRow(
		query,
		user.Name,
		user.Email,
		user.PasswordHash,
		user.EmailVerifiedAt,
		user.CreatedAt,
		user.UpdatedAt,
	))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.User{}, ErrEmailExists
		}
	}
	return created, err
}

func (r *PostgresUserRepository) FindByID(id int64) (model.User, error) {
	const query = `
		SELECT id, name, email, password_hash, email_verified_at, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	user, err := scanUser(r.db.QueryRow(query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, ErrUserNotFound
	}
	return user, err
}

func (r *PostgresUserRepository) FindByEmail(email string) (model.User, error) {
	const query = `
		SELECT id, name, email, password_hash, email_verified_at, created_at, updated_at
		FROM users
		WHERE email = $1
	`

	user, err := scanUser(r.db.QueryRow(query, email))
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, ErrUserNotFound
	}
	return user, err
}

func (r *PostgresUserRepository) UpdatePassword(id int64, passwordHash string, now time.Time) error {
	result, err := r.db.Exec(`
		UPDATE users
		SET password_hash = $2, updated_at = $3
		WHERE id = $1
	`, id, passwordHash, now)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (r *PostgresUserRepository) MarkEmailVerified(id int64, now time.Time) error {
	result, err := r.db.Exec(`
		UPDATE users
		SET email_verified_at = COALESCE(email_verified_at, $2),
		    updated_at = $2
		WHERE id = $1
	`, id, now)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrUserNotFound
	}
	return nil
}

type userScanner interface {
	Scan(dest ...any) error
}

func scanUser(scanner userScanner) (model.User, error) {
	var user model.User
	err := scanner.Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.PasswordHash,
		&user.EmailVerifiedAt,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	return user, err
}
