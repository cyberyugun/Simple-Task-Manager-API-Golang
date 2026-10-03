package repository

import (
	"database/sql"
	"errors"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var ErrTOTPCredentialNotFound = errors.New("totp credential not found")

type MFARepository interface {
	UpsertTOTP(credential model.TOTPCredential) error
	GetTOTP(userID int64) (model.TOTPCredential, error)
	ConfirmTOTP(userID int64, now time.Time) error
	DeleteTOTP(userID int64) error
}

type InMemoryMFARepository struct {
	mu          sync.Mutex
	credentials map[int64]model.TOTPCredential
}

func NewInMemoryMFARepository() *InMemoryMFARepository {
	return &InMemoryMFARepository{credentials: make(map[int64]model.TOTPCredential)}
}

func (r *InMemoryMFARepository) UpsertTOTP(credential model.TOTPCredential) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.credentials[credential.UserID]; ok && credential.CreatedAt.IsZero() {
		credential.CreatedAt = existing.CreatedAt
	}
	r.credentials[credential.UserID] = credential
	return nil
}

func (r *InMemoryMFARepository) GetTOTP(userID int64) (model.TOTPCredential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	credential, ok := r.credentials[userID]
	if !ok {
		return model.TOTPCredential{}, ErrTOTPCredentialNotFound
	}
	return credential, nil
}

func (r *InMemoryMFARepository) ConfirmTOTP(userID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	credential, ok := r.credentials[userID]
	if !ok {
		return ErrTOTPCredentialNotFound
	}
	credential.ConfirmedAt = &now
	credential.UpdatedAt = now
	r.credentials[userID] = credential
	return nil
}

func (r *InMemoryMFARepository) DeleteTOTP(userID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.credentials, userID)
	return nil
}

type PostgresMFARepository struct {
	db *sql.DB
}

func NewPostgresMFARepository(db *sql.DB) *PostgresMFARepository {
	return &PostgresMFARepository{db: db}
}

func (r *PostgresMFARepository) UpsertTOTP(credential model.TOTPCredential) error {
	_, err := r.db.Exec(`
		INSERT INTO user_totp_credentials
			(user_id, secret_ciphertext, confirmed_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) DO UPDATE SET
			secret_ciphertext = EXCLUDED.secret_ciphertext,
			confirmed_at = EXCLUDED.confirmed_at,
			updated_at = EXCLUDED.updated_at
	`, credential.UserID, credential.SecretCiphertext, credential.ConfirmedAt, credential.CreatedAt, credential.UpdatedAt)
	return err
}

func (r *PostgresMFARepository) GetTOTP(userID int64) (model.TOTPCredential, error) {
	var credential model.TOTPCredential
	err := r.db.QueryRow(`
		SELECT user_id, secret_ciphertext, confirmed_at, created_at, updated_at
		FROM user_totp_credentials
		WHERE user_id = $1
	`, userID).Scan(
		&credential.UserID,
		&credential.SecretCiphertext,
		&credential.ConfirmedAt,
		&credential.CreatedAt,
		&credential.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.TOTPCredential{}, ErrTOTPCredentialNotFound
	}
	return credential, err
}

func (r *PostgresMFARepository) ConfirmTOTP(userID int64, now time.Time) error {
	result, err := r.db.Exec(`
		UPDATE user_totp_credentials
		SET confirmed_at = $2, updated_at = $2
		WHERE user_id = $1
	`, userID, now)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrTOTPCredentialNotFound
	}
	return nil
}

func (r *PostgresMFARepository) DeleteTOTP(userID int64) error {
	_, err := r.db.Exec(`DELETE FROM user_totp_credentials WHERE user_id = $1`, userID)
	return err
}
