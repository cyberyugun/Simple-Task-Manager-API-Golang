package repository

import (
	"errors"
	"sort"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrConnectorOAuthSessionNotFound = errors.New("connector oauth session not found")
	ErrConnectorCredentialNotFound   = errors.New("connector credential metadata not found")
)

type ConnectorSecurityRepository interface {
	CreateOAuthSession(session model.ConnectorOAuthSession) (model.ConnectorOAuthSession, error)
	ConsumeOAuthSession(stateHash string, now time.Time) (model.ConnectorOAuthSession, error)

	UpsertCredentialMetadata(item model.ConnectorCredentialMetadata) (model.ConnectorCredentialMetadata, error)
	GetCredentialMetadata(organizationID, connectionID int64) (model.ConnectorCredentialMetadata, error)
	ListCredentialsDue(before time.Time, limit int) ([]model.ConnectorCredentialMetadata, error)

	RecordCredentialAccess(item model.ConnectorCredentialAccess) error
	ListCredentialAccess(organizationID, connectionID int64, limit int) ([]model.ConnectorCredentialAccess, error)
}

type InMemoryConnectorSecurityRepository struct {
	mu          sync.Mutex
	sessions    map[int64]model.ConnectorOAuthSession
	stateIndex  map[string]int64
	credentials map[int64]model.ConnectorCredentialMetadata
	access      []model.ConnectorCredentialAccess
	nextSession int64
	nextAccess  int64
}

func NewInMemoryConnectorSecurityRepository() *InMemoryConnectorSecurityRepository {
	return &InMemoryConnectorSecurityRepository{
		sessions: make(map[int64]model.ConnectorOAuthSession), stateIndex: make(map[string]int64),
		credentials: make(map[int64]model.ConnectorCredentialMetadata), nextSession: 1, nextAccess: 1,
	}
}

func (r *InMemoryConnectorSecurityRepository) CreateOAuthSession(session model.ConnectorOAuthSession) (model.ConnectorOAuthSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session.ID = r.nextSession
	r.nextSession++
	session.RequestedScopes = append([]string(nil), session.RequestedScopes...)
	r.sessions[session.ID] = session
	r.stateIndex[session.StateHash] = session.ID
	return session, nil
}

func (r *InMemoryConnectorSecurityRepository) ConsumeOAuthSession(stateHash string, now time.Time) (model.ConnectorOAuthSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.stateIndex[stateHash]
	if !ok {
		return model.ConnectorOAuthSession{}, ErrConnectorOAuthSessionNotFound
	}
	item := r.sessions[id]
	if item.ConsumedAt != nil || !item.ExpiresAt.After(now) {
		return model.ConnectorOAuthSession{}, ErrConnectorOAuthSessionNotFound
	}
	t := now
	item.ConsumedAt = &t
	r.sessions[id] = item
	delete(r.stateIndex, stateHash)
	item.RequestedScopes = append([]string(nil), item.RequestedScopes...)
	return item, nil
}

func (r *InMemoryConnectorSecurityRepository) UpsertCredentialMetadata(item model.ConnectorCredentialMetadata) (model.ConnectorCredentialMetadata, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if previous, ok := r.credentials[item.ConnectionID]; ok {
		if item.CredentialVersion == 0 {
			item.CredentialVersion = previous.CredentialVersion
		}
		if item.KeyVersion == 0 {
			item.KeyVersion = previous.KeyVersion
		}
	}
	item.GrantedScopes = append([]string(nil), item.GrantedScopes...)
	r.credentials[item.ConnectionID] = item
	return item, nil
}

func (r *InMemoryConnectorSecurityRepository) GetCredentialMetadata(organizationID, connectionID int64) (model.ConnectorCredentialMetadata, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.credentials[connectionID]
	if !ok || item.OrganizationID != organizationID {
		return model.ConnectorCredentialMetadata{}, ErrConnectorCredentialNotFound
	}
	item.GrantedScopes = append([]string(nil), item.GrantedScopes...)
	return item, nil
}

func (r *InMemoryConnectorSecurityRepository) ListCredentialsDue(before time.Time, limit int) ([]model.ConnectorCredentialMetadata, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.ConnectorCredentialMetadata, 0)
	for _, item := range r.credentials {
		if item.Status != model.ConnectorCredentialActive || item.ExpiresAt == nil || item.ExpiresAt.After(before) {
			continue
		}
		copyItem := item
		copyItem.GrantedScopes = append([]string(nil), item.GrantedScopes...)
		items = append(items, copyItem)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ExpiresAt.Equal(*items[j].ExpiresAt) {
			return items[i].ConnectionID < items[j].ConnectionID
		}
		return items[i].ExpiresAt.Before(*items[j].ExpiresAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryConnectorSecurityRepository) RecordCredentialAccess(item model.ConnectorCredentialAccess) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextAccess
	r.nextAccess++
	r.access = append(r.access, item)
	return nil
}

func (r *InMemoryConnectorSecurityRepository) ListCredentialAccess(organizationID, connectionID int64, limit int) ([]model.ConnectorCredentialAccess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.ConnectorCredentialAccess, 0)
	for i := len(r.access) - 1; i >= 0; i-- {
		item := r.access[i]
		if item.OrganizationID == organizationID && (connectionID <= 0 || item.ConnectionID == connectionID) {
			items = append(items, item)
			if limit > 0 && len(items) >= limit {
				break
			}
		}
	}
	return items, nil
}
