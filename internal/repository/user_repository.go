package repository

import (
	"errors"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrUserNotFound = errors.New("user not found")
	ErrEmailExists  = errors.New("email already registered")
)

type UserRepository interface {
	Create(user model.User) (model.User, error)
	FindByID(id int64) (model.User, error)
	FindByEmail(email string) (model.User, error)
	UpdatePassword(id int64, passwordHash string, now time.Time) error
	MarkEmailVerified(id int64, now time.Time) error
}

type InMemoryUserRepository struct {
	mu      sync.RWMutex
	users   map[int64]model.User
	byEmail map[string]int64
	nextID  int64
}

func NewInMemoryUserRepository() *InMemoryUserRepository {
	return &InMemoryUserRepository{
		users:   make(map[int64]model.User),
		byEmail: make(map[string]int64),
		nextID:  1,
	}
}

func (r *InMemoryUserRepository) Create(user model.User) (model.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	email := strings.ToLower(strings.TrimSpace(user.Email))
	if _, exists := r.byEmail[email]; exists {
		return model.User{}, ErrEmailExists
	}

	user.ID = r.nextID
	user.Email = email
	r.nextID++
	r.users[user.ID] = user
	r.byEmail[email] = user.ID
	return user, nil
}

func (r *InMemoryUserRepository) FindByID(id int64) (model.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	user, ok := r.users[id]
	if !ok {
		return model.User{}, ErrUserNotFound
	}
	return user, nil
}

func (r *InMemoryUserRepository) FindByEmail(email string) (model.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	id, ok := r.byEmail[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return model.User{}, ErrUserNotFound
	}
	return r.users[id], nil
}

func (r *InMemoryUserRepository) UpdatePassword(id int64, passwordHash string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	user, ok := r.users[id]
	if !ok {
		return ErrUserNotFound
	}
	user.PasswordHash = passwordHash
	user.UpdatedAt = now
	r.users[id] = user
	return nil
}

func (r *InMemoryUserRepository) MarkEmailVerified(id int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	user, ok := r.users[id]
	if !ok {
		return ErrUserNotFound
	}
	user.EmailVerifiedAt = &now
	user.UpdatedAt = now
	r.users[id] = user
	return nil
}
