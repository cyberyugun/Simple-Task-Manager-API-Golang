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
		INSERT INTO refresh_tokens (
			user_id, token_hash, user_agent, ip_address, expires_at, last_used_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := r.db.Exec(
		query,
		session.UserID,
		session.TokenHash,
		session.UserAgent,
		session.IPAddress,
		session.ExpiresAt,
		session.LastUsedAt,
		session.CreatedAt,
	)
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
		SET revoked_at = $2, last_used_at = $2
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND expires_at > $2
		RETURNING id, user_id, token_hash, user_agent, ip_address, expires_at, last_used_at, revoked_at, created_at
	`

	old, err := scanRefreshSession(tx.QueryRow(consumeQuery, oldHash, now))
	if errors.Is(err, sql.ErrNoRows) {
		return model.RefreshSession{}, ErrInvalidRefreshToken
	}
	if err != nil {
		return model.RefreshSession{}, err
	}

	const createQuery = `
		INSERT INTO refresh_tokens (
			user_id, token_hash, user_agent, ip_address, expires_at, last_used_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
	`
	if _, err := tx.Exec(
		createQuery,
		old.UserID,
		newHash,
		old.UserAgent,
		old.IPAddress,
		newExpiresAt,
		now,
	); err != nil {
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
		SET revoked_at = $2, last_used_at = $2
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, tokenHash, now)
	return err
}

func (r *PostgresRefreshTokenRepository) ListActive(userID int64, now time.Time) ([]model.RefreshSession, error) {
	rows, err := r.db.Query(`
		SELECT id, user_id, token_hash, user_agent, ip_address, expires_at, last_used_at, revoked_at, created_at
		FROM refresh_tokens
		WHERE user_id = $1
		  AND revoked_at IS NULL
		  AND expires_at > $2
		ORDER BY last_used_at DESC, id DESC
	`, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := make([]model.RefreshSession, 0)
	for rows.Next() {
		session, err := scanRefreshSession(rows)
		if err != nil {
			return nil, err
		}
		session.TokenHash = ""
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (r *PostgresRefreshTokenRepository) RevokeByID(userID, sessionID int64, now time.Time) error {
	result, err := r.db.Exec(`
		UPDATE refresh_tokens
		SET revoked_at = $3, last_used_at = $3
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL
	`, sessionID, userID, now)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrInvalidRefreshToken
	}
	return nil
}

func (r *PostgresRefreshTokenRepository) RevokeAll(userID int64, now time.Time) error {
	_, err := r.db.Exec(`
		UPDATE refresh_tokens
		SET revoked_at = $2, last_used_at = $2
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID, now)
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
		&session.UserAgent,
		&session.IPAddress,
		&session.ExpiresAt,
		&session.LastUsedAt,
		&session.RevokedAt,
		&session.CreatedAt,
	)
	return session, err
}
