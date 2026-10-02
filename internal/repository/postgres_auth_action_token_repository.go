package repository

import (
	"database/sql"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresAuthActionTokenRepository struct {
	db *sql.DB
}

func NewPostgresAuthActionTokenRepository(db *sql.DB) *PostgresAuthActionTokenRepository {
	return &PostgresAuthActionTokenRepository{db: db}
}

func (r *PostgresAuthActionTokenRepository) Create(token model.AuthActionToken) error {
	_, err := r.db.Exec(`
		INSERT INTO auth_action_tokens (user_id, token_hash, purpose, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, token.UserID, token.TokenHash, token.Purpose, token.ExpiresAt, token.CreatedAt)
	return err
}

func (r *PostgresAuthActionTokenRepository) Consume(tokenHash, purpose string, now time.Time) (model.AuthActionToken, error) {
	const query = `
		UPDATE auth_action_tokens
		SET consumed_at = $3
		WHERE token_hash = $1
		  AND purpose = $2
		  AND consumed_at IS NULL
		  AND expires_at > $3
		RETURNING id, user_id, token_hash, purpose, expires_at, consumed_at, created_at
	`

	var token model.AuthActionToken
	err := r.db.QueryRow(query, tokenHash, purpose, now).Scan(
		&token.ID,
		&token.UserID,
		&token.TokenHash,
		&token.Purpose,
		&token.ExpiresAt,
		&token.ConsumedAt,
		&token.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AuthActionToken{}, ErrInvalidActionToken
	}
	return token, err
}
