package repository

import (
	"errors"
	"sort"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var ErrInvalidRefreshToken = errors.New("invalid or expired refresh token")

type RefreshTokenRepository interface {
	Create(session model.RefreshSession) error
	Rotate(oldHash, newHash string, newExpiresAt, now time.Time) (model.RefreshSession, error)
	Revoke(tokenHash string, now time.Time) error
	ListActive(userID int64, now time.Time) ([]model.RefreshSession, error)
	RevokeByID(userID, sessionID int64, now time.Time) error
	RevokeAll(userID int64, now time.Time) error
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
	if session.LastUsedAt.IsZero() {
		session.LastUsedAt = session.CreatedAt
	}
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
	old.LastUsedAt = now
	r.sessions[oldHash] = old

	r.sessions[newHash] = model.RefreshSession{
		ID:               r.nextID,
		UserID:           old.UserID,
		TokenHash:        newHash,
		UserAgent:        old.UserAgent,
		IPAddress:        old.IPAddress,
		MFAAuthenticated: old.MFAAuthenticated,
		ExpiresAt:        newExpiresAt,
		LastUsedAt:       now,
		CreatedAt:        now,
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
	session.LastUsedAt = now
	r.sessions[tokenHash] = session
	return nil
}

func (r *InMemoryRefreshTokenRepository) ListActive(userID int64, now time.Time) ([]model.RefreshSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sessions := make([]model.RefreshSession, 0)
	for _, session := range r.sessions {
		if session.UserID == userID && session.RevokedAt == nil && session.ExpiresAt.After(now) {
			session.TokenHash = ""
			sessions = append(sessions, session)
		}
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUsedAt.After(sessions[j].LastUsedAt)
	})
	return sessions, nil
}

func (r *InMemoryRefreshTokenRepository) RevokeByID(userID, sessionID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for hash, session := range r.sessions {
		if session.UserID != userID || session.ID != sessionID {
			continue
		}
		if session.RevokedAt == nil {
			revokedAt := now
			session.RevokedAt = &revokedAt
			session.LastUsedAt = now
			r.sessions[hash] = session
		}
		return nil
	}
	return ErrInvalidRefreshToken
}

func (r *InMemoryRefreshTokenRepository) RevokeAll(userID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for hash, session := range r.sessions {
		if session.UserID == userID && session.RevokedAt == nil {
			revokedAt := now
			session.RevokedAt = &revokedAt
			session.LastUsedAt = now
			r.sessions[hash] = session
		}
	}
	return nil
}
