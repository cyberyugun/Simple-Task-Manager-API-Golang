package repository

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

var (
	ErrWebAuthnCredentialNotFound = errors.New("webauthn credential not found")
	ErrWebAuthnSessionNotFound    = errors.New("webauthn ceremony not found")
)

type WebAuthnRepository interface {
	GetOrCreateHandle(userID int64, now time.Time) ([]byte, error)
	ListCredentials(userID int64) ([]webauthn.Credential, error)
	SaveCredential(userID int64, credential webauthn.Credential, now time.Time) error
	HasCredentials(userID int64) (bool, error)
	SaveSession(sessionHash string, userID int64, purpose string, session webauthn.SessionData, expiresAt, now time.Time) error
	ConsumeSession(sessionHash, purpose string, now time.Time) (int64, webauthn.SessionData, error)
}

type inMemoryWebAuthnSession struct {
	userID    int64
	purpose   string
	session   webauthn.SessionData
	expiresAt time.Time
	consumed  bool
}

type InMemoryWebAuthnRepository struct {
	mu          sync.Mutex
	handles     map[int64][]byte
	credentials map[int64]map[string]webauthn.Credential
	sessions    map[string]inMemoryWebAuthnSession
}

func NewInMemoryWebAuthnRepository() *InMemoryWebAuthnRepository {
	return &InMemoryWebAuthnRepository{
		handles:     make(map[int64][]byte),
		credentials: make(map[int64]map[string]webauthn.Credential),
		sessions:    make(map[string]inMemoryWebAuthnSession),
	}
}

func randomWebAuthnHandle() ([]byte, error) {
	handle := make([]byte, 32)
	if _, err := rand.Read(handle); err != nil {
		return nil, err
	}
	return handle, nil
}

func (r *InMemoryWebAuthnRepository) GetOrCreateHandle(userID int64, now time.Time) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing := r.handles[userID]; len(existing) != 0 {
		return append([]byte(nil), existing...), nil
	}
	handle, err := randomWebAuthnHandle()
	if err != nil {
		return nil, err
	}
	r.handles[userID] = handle
	return append([]byte(nil), handle...), nil
}

func (r *InMemoryWebAuthnRepository) ListCredentials(userID int64) ([]webauthn.Credential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]webauthn.Credential, 0, len(r.credentials[userID]))
	for _, credential := range r.credentials[userID] {
		items = append(items, credential)
	}
	return items, nil
}

func (r *InMemoryWebAuthnRepository) SaveCredential(userID int64, credential webauthn.Credential, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.credentials[userID] == nil {
		r.credentials[userID] = make(map[string]webauthn.Credential)
	}
	key := base64.RawURLEncoding.EncodeToString(credential.ID)
	r.credentials[userID][key] = credential
	return nil
}

func (r *InMemoryWebAuthnRepository) HasCredentials(userID int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.credentials[userID]) > 0, nil
}

func (r *InMemoryWebAuthnRepository) SaveSession(sessionHash string, userID int64, purpose string, session webauthn.SessionData, expiresAt, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[sessionHash] = inMemoryWebAuthnSession{
		userID: userID, purpose: purpose, session: session, expiresAt: expiresAt,
	}
	return nil
}

func (r *InMemoryWebAuthnRepository) ConsumeSession(sessionHash, purpose string, now time.Time) (int64, webauthn.SessionData, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.sessions[sessionHash]
	if !ok || item.consumed || item.purpose != purpose || !item.expiresAt.After(now) {
		return 0, webauthn.SessionData{}, ErrWebAuthnSessionNotFound
	}
	item.consumed = true
	r.sessions[sessionHash] = item
	return item.userID, item.session, nil
}

type PostgresWebAuthnRepository struct {
	db *sql.DB
}

func NewPostgresWebAuthnRepository(db *sql.DB) *PostgresWebAuthnRepository {
	return &PostgresWebAuthnRepository{db: db}
}

func (r *PostgresWebAuthnRepository) GetOrCreateHandle(userID int64, now time.Time) ([]byte, error) {
	handle, err := randomWebAuthnHandle()
	if err != nil {
		return nil, err
	}
	var stored []byte
	err = r.db.QueryRow(`
		INSERT INTO webauthn_user_handles (user_id, handle, created_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET user_id = EXCLUDED.user_id
		RETURNING handle
	`, userID, handle, now).Scan(&stored)
	return stored, err
}

func (r *PostgresWebAuthnRepository) ListCredentials(userID int64) ([]webauthn.Credential, error) {
	rows, err := r.db.Query(`
		SELECT credential_json
		FROM webauthn_credentials
		WHERE user_id = $1
		ORDER BY created_at
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]webauthn.Credential, 0)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var credential webauthn.Credential
		if err := json.Unmarshal(raw, &credential); err != nil {
			return nil, err
		}
		items = append(items, credential)
	}
	return items, rows.Err()
}

func (r *PostgresWebAuthnRepository) SaveCredential(userID int64, credential webauthn.Credential, now time.Time) error {
	raw, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	credentialID := base64.RawURLEncoding.EncodeToString(credential.ID)
	_, err = r.db.Exec(`
		INSERT INTO webauthn_credentials
			(credential_id, user_id, credential_json, created_at, last_used_at)
		VALUES ($1, $2, $3::jsonb, $4, $4)
		ON CONFLICT (credential_id) DO UPDATE SET
			credential_json = EXCLUDED.credential_json,
			last_used_at = EXCLUDED.last_used_at
		WHERE webauthn_credentials.user_id = EXCLUDED.user_id
	`, credentialID, userID, string(raw), now)
	return err
}

func (r *PostgresWebAuthnRepository) HasCredentials(userID int64) (bool, error) {
	var exists bool
	err := r.db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM webauthn_credentials WHERE user_id = $1)
	`, userID).Scan(&exists)
	return exists, err
}

func (r *PostgresWebAuthnRepository) SaveSession(sessionHash string, userID int64, purpose string, session webauthn.SessionData, expiresAt, now time.Time) error {
	raw, err := json.Marshal(session)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO webauthn_sessions
			(session_id, user_id, purpose, session_json, expires_at, created_at)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6)
	`, sessionHash, userID, purpose, string(raw), expiresAt, now)
	return err
}

func (r *PostgresWebAuthnRepository) ConsumeSession(sessionHash, purpose string, now time.Time) (int64, webauthn.SessionData, error) {
	var userID int64
	var raw []byte
	err := r.db.QueryRow(`
		UPDATE webauthn_sessions
		SET consumed_at = $3
		WHERE session_id = $1
		  AND purpose = $2
		  AND consumed_at IS NULL
		  AND expires_at > $3
		RETURNING user_id, session_json
	`, sessionHash, purpose, now).Scan(&userID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, webauthn.SessionData{}, ErrWebAuthnSessionNotFound
	}
	if err != nil {
		return 0, webauthn.SessionData{}, err
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(raw, &session); err != nil {
		return 0, webauthn.SessionData{}, err
	}
	return userID, session, nil
}
