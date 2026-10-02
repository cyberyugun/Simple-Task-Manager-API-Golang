package repository

import (
	"errors"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var ErrInvalidActionToken = errors.New("invalid or expired action token")

type AuthActionTokenRepository interface {
	Create(token model.AuthActionToken) error
	Consume(tokenHash, purpose string, now time.Time) (model.AuthActionToken, error)
}

type InMemoryAuthActionTokenRepository struct {
	mu     sync.Mutex
	tokens map[string]model.AuthActionToken
	nextID int64
}

func NewInMemoryAuthActionTokenRepository() *InMemoryAuthActionTokenRepository {
	return &InMemoryAuthActionTokenRepository{
		tokens: make(map[string]model.AuthActionToken),
		nextID: 1,
	}
}

func (r *InMemoryAuthActionTokenRepository) Create(token model.AuthActionToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tokens[token.TokenHash]; exists {
		return errors.New("action token already exists")
	}
	token.ID = r.nextID
	r.nextID++
	r.tokens[token.TokenHash] = token
	return nil
}

func (r *InMemoryAuthActionTokenRepository) Consume(tokenHash, purpose string, now time.Time) (model.AuthActionToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	token, ok := r.tokens[tokenHash]
	if !ok || token.Purpose != purpose || token.ConsumedAt != nil || !token.ExpiresAt.After(now) {
		return model.AuthActionToken{}, ErrInvalidActionToken
	}

	consumedAt := now
	token.ConsumedAt = &consumedAt
	r.tokens[tokenHash] = token
	return token, nil
}
