package repository

import (
	"database/sql"
	"errors"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var ErrOIDCLoginSessionNotFound = errors.New("oidc login session not found")

type OIDCLoginRepository interface {
	Save(stateHash string, session model.OIDCLoginSession) error
	Consume(stateHash string, now time.Time) (model.OIDCLoginSession, error)
}

type InMemoryOIDCLoginRepository struct {
	mu       sync.Mutex
	sessions map[string]model.OIDCLoginSession
	consumed map[string]bool
}

func NewInMemoryOIDCLoginRepository() *InMemoryOIDCLoginRepository {
	return &InMemoryOIDCLoginRepository{
		sessions: make(map[string]model.OIDCLoginSession),
		consumed: make(map[string]bool),
	}
}

func (r *InMemoryOIDCLoginRepository) Save(stateHash string, session model.OIDCLoginSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sessions[stateHash]; exists {
		return errors.New("oidc login session already exists")
	}
	r.sessions[stateHash] = session
	return nil
}

func (r *InMemoryOIDCLoginRepository) Consume(stateHash string, now time.Time) (model.OIDCLoginSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[stateHash]
	if !ok || r.consumed[stateHash] || !session.ExpiresAt.After(now) {
		return model.OIDCLoginSession{}, ErrOIDCLoginSessionNotFound
	}
	r.consumed[stateHash] = true
	return session, nil
}

type PostgresOIDCLoginRepository struct {
	db *sql.DB
}

func NewPostgresOIDCLoginRepository(db *sql.DB) *PostgresOIDCLoginRepository {
	return &PostgresOIDCLoginRepository{db: db}
}

func (r *PostgresOIDCLoginRepository) Save(stateHash string, session model.OIDCLoginSession) error {
	_, err := r.db.Exec(`
		INSERT INTO oidc_login_sessions
			(state_hash, workspace_id, nonce, code_verifier_ciphertext, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, stateHash, session.WorkspaceID, session.Nonce, session.CodeVerifierCiphertext, session.ExpiresAt, session.CreatedAt)
	return err
}

func (r *PostgresOIDCLoginRepository) Consume(stateHash string, now time.Time) (model.OIDCLoginSession, error) {
	var session model.OIDCLoginSession
	err := r.db.QueryRow(`
		UPDATE oidc_login_sessions
		SET consumed_at = $2
		WHERE state_hash = $1
		  AND consumed_at IS NULL
		  AND expires_at > $2
		RETURNING workspace_id, nonce, code_verifier_ciphertext, expires_at, created_at
	`, stateHash, now).Scan(
		&session.WorkspaceID,
		&session.Nonce,
		&session.CodeVerifierCiphertext,
		&session.ExpiresAt,
		&session.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.OIDCLoginSession{}, ErrOIDCLoginSessionNotFound
	}
	return session, err
}
