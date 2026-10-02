package repository

import (
	"database/sql"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresRefreshTokenRepository struct {
	db *sql.DB
}

func NewPostgresRefreshTokenRepository(db *sql.DB) *PostgresRefreshTokenRepository {
	return &PostgresRefreshTokenRepository{db: db}
}

func (r *PostgresRefreshTokenRepository) Create(session model.RefreshSession) error {
	const query = `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4)
	`
	_, err := r.db.Exec(query, session.UserID, session.TokenHash, session.ExpiresAt, session.CreatedAt)
	return err
}

func (r *PostgresRefreshTokenRepository) Rotate(oldHash, newHash string, newExpiresAt, now time.Time) (model.RefreshSession, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return model.RefreshSession{}, err
	}
	defer tx.Rollback()

	const consumeQuery = `
		UPDATE refresh_tokens
		SET revoked_at = $2
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND expires_at > $2
		RETURNING id, user_id, token_hash, expires_at, revoked_at, created_at
	`

	old, err := scanRefreshSession(tx.QueryRow(consumeQuery, oldHash, now))
	if errors.Is(err, sql.ErrNoRows) {
		return model.RefreshSession{}, ErrInvalidRefreshToken
	}
	if err != nil {
		return model.RefreshSession{}, err
	}

	const createQuery = `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4)
	`
	if _, err := tx.Exec(createQuery, old.UserID, newHash, newExpiresAt, now); err != nil {
		return model.RefreshSession{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.RefreshSession{}, err
	}
	return old, nil
}

func (r *PostgresRefreshTokenRepository) Revoke(tokenHash string, now time.Time) error {
	_, err := r.db.Exec(`
		UPDATE refresh_tokens
		SET revoked_at = $2
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, tokenHash, now)
	return err
}

type refreshSessionScanner interface {
	Scan(dest ...any) error
}

func scanRefreshSession(scanner refreshSessionScanner) (model.RefreshSession, error) {
	var session model.RefreshSession
	err := scanner.Scan(
		&session.ID,
		&session.UserID,
		&session.TokenHash,
		&session.ExpiresAt,
		&session.RevokedAt,
		&session.CreatedAt,
	)
	return session, err
}
