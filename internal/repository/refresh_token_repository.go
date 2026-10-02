package repository

import (
	"errors"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var ErrInvalidRefreshToken = errors.New("invalid or expired refresh token")

type RefreshTokenRepository interface {
	Create(session model.RefreshSession) error
	Rotate(oldHash, newHash string, newExpiresAt, now time.Time) (model.RefreshSession, error)
	Revoke(tokenHash string, now time.Time) error
}

type InMemoryRefreshTokenRepository struct {
	mu       sync.Mutex
	sessions map[string]model.RefreshSession
	nextID   int64
}

func NewInMemoryRefreshTokenRepository() *InMemoryRefreshTokenRepository {
	return &InMemoryRefreshTokenRepository{
		sessions: make(map[string]model.RefreshSession),
		nextID:   1,
	}
}

func (r *InMemoryRefreshTokenRepository) Create(session model.RefreshSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sessions[session.TokenHash]; exists {
		return errors.New("refresh token already exists")
	}

	session.ID = r.nextID
	r.nextID++
	r.sessions[session.TokenHash] = session
	return nil
}

func (r *InMemoryRefreshTokenRepository) Rotate(oldHash, newHash string, newExpiresAt, now time.Time) (model.RefreshSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	old, ok := r.sessions[oldHash]
	if !ok || old.RevokedAt != nil || !old.ExpiresAt.After(now) {
		return model.RefreshSession{}, ErrInvalidRefreshToken
	}
	if _, exists := r.sessions[newHash]; exists {
		return model.RefreshSession{}, errors.New("refresh token already exists")
	}

	revokedAt := now
	old.RevokedAt = &revokedAt
	r.sessions[oldHash] = old

	r.sessions[newHash] = model.RefreshSession{
		ID:        r.nextID,
		UserID:    old.UserID,
		TokenHash: newHash,
		ExpiresAt: newExpiresAt,
		CreatedAt: now,
	}
	r.nextID++

	return old, nil
}

func (r *InMemoryRefreshTokenRepository) Revoke(tokenHash string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	session, ok := r.sessions[tokenHash]
	if !ok || session.RevokedAt != nil {
		return nil
	}

	revokedAt := now
	session.RevokedAt = &revokedAt
	r.sessions[tokenHash] = session
	return nil
}
