package repository

import (
	"errors"
	"sort"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrOAuthClientNotFound = errors.New("oauth client not found")
	ErrAuthorizationCode   = errors.New("invalid or expired authorization code")
	ErrAPIKeyNotFound      = errors.New("api key not found")
	ErrEnterprisePolicy    = errors.New("enterprise policy not found")
	ErrOIDCConnection      = errors.New("oidc connection not found")
	ErrSCIMUserNotFound    = errors.New("scim user not found")
)

type EnterpriseIdentityRepository interface {
	CreateOAuthClient(client model.OAuthClient) error
	FindOAuthClient(clientID string) (model.OAuthClient, error)
	RevokeOAuthClient(clientID string, now time.Time) error

	SaveAuthorizationCode(code model.OAuthAuthorizationCode) error
	ConsumeAuthorizationCode(codeHash string, now time.Time) (model.OAuthAuthorizationCode, error)

	CreateAPIKey(key model.ServiceAPIKey) (model.ServiceAPIKey, error)
	FindAPIKeyByHash(keyHash string, now time.Time) (model.ServiceAPIKey, error)
	ListAPIKeys(workspaceID int64) ([]model.ServiceAPIKey, error)
	RevokeAPIKey(workspaceID, keyID int64, now time.Time) error
	TouchAPIKey(keyID int64, now time.Time) error

	RevokeAccessToken(jti string, expiresAt time.Time, revokedByUserID int64, now time.Time) error
	IsAccessTokenRevoked(jti string, now time.Time) (bool, error)

	UpsertSecurityPolicy(policy model.EnterpriseSecurityPolicy) (model.EnterpriseSecurityPolicy, error)
	GetSecurityPolicy(workspaceID int64) (model.EnterpriseSecurityPolicy, error)

	UpsertOIDCConnection(connection model.OIDCConnection) (model.OIDCConnection, error)
	GetOIDCConnection(workspaceID int64) (model.OIDCConnection, error)

	UpsertSCIMUser(user model.SCIMUser) (model.SCIMUser, error)
	ListSCIMUsers(workspaceID int64) ([]model.SCIMUser, error)
	FindSCIMUser(workspaceID int64, externalID string) (model.SCIMUser, error)
}

type InMemoryEnterpriseIdentityRepository struct {
	mu          sync.Mutex
	clients     map[string]model.OAuthClient
	codes       map[string]model.OAuthAuthorizationCode
	apiKeys     map[int64]model.ServiceAPIKey
	revokedJTIs map[string]time.Time
	policies    map[int64]model.EnterpriseSecurityPolicy
	oidc        map[int64]model.OIDCConnection
	scim        map[int64]map[string]model.SCIMUser
	nextKeyID   int64
	nextSCIMID  int64
}

func NewInMemoryEnterpriseIdentityRepository() *InMemoryEnterpriseIdentityRepository {
	return &InMemoryEnterpriseIdentityRepository{
		clients:     make(map[string]model.OAuthClient),
		codes:       make(map[string]model.OAuthAuthorizationCode),
		apiKeys:     make(map[int64]model.ServiceAPIKey),
		revokedJTIs: make(map[string]time.Time),
		policies:    make(map[int64]model.EnterpriseSecurityPolicy),
		oidc:        make(map[int64]model.OIDCConnection),
		scim:        make(map[int64]map[string]model.SCIMUser),
		nextKeyID:   1,
		nextSCIMID:  1,
	}
}

func (r *InMemoryEnterpriseIdentityRepository) CreateOAuthClient(client model.OAuthClient) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.clients[client.ClientID]; ok {
		return errors.New("oauth client already exists")
	}
	r.clients[client.ClientID] = client
	return nil
}

func (r *InMemoryEnterpriseIdentityRepository) FindOAuthClient(clientID string) (model.OAuthClient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	client, ok := r.clients[clientID]
	if !ok || client.RevokedAt != nil {
		return model.OAuthClient{}, ErrOAuthClientNotFound
	}
	return client, nil
}

func (r *InMemoryEnterpriseIdentityRepository) RevokeOAuthClient(clientID string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	client, ok := r.clients[clientID]
	if !ok {
		return ErrOAuthClientNotFound
	}
	client.RevokedAt = &now
	r.clients[clientID] = client
	return nil
}

func (r *InMemoryEnterpriseIdentityRepository) SaveAuthorizationCode(code model.OAuthAuthorizationCode) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.codes[code.CodeHash]; ok {
		return errors.New("authorization code already exists")
	}
	r.codes[code.CodeHash] = code
	return nil
}

func (r *InMemoryEnterpriseIdentityRepository) ConsumeAuthorizationCode(codeHash string, now time.Time) (model.OAuthAuthorizationCode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	code, ok := r.codes[codeHash]
	if !ok || code.ConsumedAt != nil || !code.ExpiresAt.After(now) {
		return model.OAuthAuthorizationCode{}, ErrAuthorizationCode
	}
	code.ConsumedAt = &now
	r.codes[codeHash] = code
	return code, nil
}

func (r *InMemoryEnterpriseIdentityRepository) CreateAPIKey(key model.ServiceAPIKey) (model.ServiceAPIKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.apiKeys {
		if existing.KeyHash == key.KeyHash {
			return model.ServiceAPIKey{}, errors.New("api key already exists")
		}
	}
	key.ID = r.nextKeyID
	r.nextKeyID++
	r.apiKeys[key.ID] = key
	return key, nil
}

func (r *InMemoryEnterpriseIdentityRepository) FindAPIKeyByHash(keyHash string, now time.Time) (model.ServiceAPIKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, key := range r.apiKeys {
		if key.KeyHash != keyHash || key.RevokedAt != nil {
			continue
		}
		if key.ExpiresAt != nil && !key.ExpiresAt.After(now) {
			continue
		}
		return key, nil
	}
	return model.ServiceAPIKey{}, ErrAPIKeyNotFound
}

func (r *InMemoryEnterpriseIdentityRepository) ListAPIKeys(workspaceID int64) ([]model.ServiceAPIKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.ServiceAPIKey, 0)
	for _, key := range r.apiKeys {
		if key.WorkspaceID == workspaceID {
			key.KeyHash = ""
			items = append(items, key)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryEnterpriseIdentityRepository) RevokeAPIKey(workspaceID, keyID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.apiKeys[keyID]
	if !ok || key.WorkspaceID != workspaceID {
		return ErrAPIKeyNotFound
	}
	key.RevokedAt = &now
	r.apiKeys[keyID] = key
	return nil
}

func (r *InMemoryEnterpriseIdentityRepository) TouchAPIKey(keyID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.apiKeys[keyID]
	if !ok {
		return ErrAPIKeyNotFound
	}
	key.LastUsedAt = &now
	r.apiKeys[keyID] = key
	return nil
}

func (r *InMemoryEnterpriseIdentityRepository) RevokeAccessToken(jti string, expiresAt time.Time, revokedByUserID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revokedJTIs[jti] = expiresAt
	return nil
}

func (r *InMemoryEnterpriseIdentityRepository) IsAccessTokenRevoked(jti string, now time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	expiresAt, ok := r.revokedJTIs[jti]
	if !ok {
		return false, nil
	}
	if !expiresAt.After(now) {
		delete(r.revokedJTIs, jti)
		return false, nil
	}
	return true, nil
}

func (r *InMemoryEnterpriseIdentityRepository) UpsertSecurityPolicy(policy model.EnterpriseSecurityPolicy) (model.EnterpriseSecurityPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policies[policy.WorkspaceID] = policy
	return policy, nil
}

func (r *InMemoryEnterpriseIdentityRepository) GetSecurityPolicy(workspaceID int64) (model.EnterpriseSecurityPolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	policy, ok := r.policies[workspaceID]
	if !ok {
		return model.EnterpriseSecurityPolicy{}, ErrEnterprisePolicy
	}
	return policy, nil
}

func (r *InMemoryEnterpriseIdentityRepository) UpsertOIDCConnection(connection model.OIDCConnection) (model.OIDCConnection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.oidc[connection.WorkspaceID] = connection
	return connection, nil
}

func (r *InMemoryEnterpriseIdentityRepository) GetOIDCConnection(workspaceID int64) (model.OIDCConnection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	connection, ok := r.oidc[workspaceID]
	if !ok {
		return model.OIDCConnection{}, ErrOIDCConnection
	}
	return connection, nil
}

func (r *InMemoryEnterpriseIdentityRepository) UpsertSCIMUser(user model.SCIMUser) (model.SCIMUser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.scim[user.WorkspaceID] == nil {
		r.scim[user.WorkspaceID] = make(map[string]model.SCIMUser)
	}
	if existing, ok := r.scim[user.WorkspaceID][user.ExternalID]; ok {
		user.ID = existing.ID
		user.CreatedAt = existing.CreatedAt
	} else {
		user.ID = r.nextSCIMID
		r.nextSCIMID++
	}
	r.scim[user.WorkspaceID][user.ExternalID] = user
	return user, nil
}

func (r *InMemoryEnterpriseIdentityRepository) ListSCIMUsers(workspaceID int64) ([]model.SCIMUser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.SCIMUser, 0)
	for _, user := range r.scim[workspaceID] {
		items = append(items, user)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryEnterpriseIdentityRepository) FindSCIMUser(workspaceID int64, externalID string) (model.SCIMUser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	user, ok := r.scim[workspaceID][externalID]
	if !ok {
		return model.SCIMUser{}, ErrSCIMUserNotFound
	}
	return user, nil
}
