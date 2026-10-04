package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrConnectorOAuthInvalid       = errors.New("invalid connector oauth configuration")
	ErrConnectorOAuthState         = errors.New("invalid or expired connector oauth state")
	ErrConnectorOAuthExchange      = errors.New("connector oauth token exchange failed")
	ErrConnectorScopeMismatch      = errors.New("connector oauth scopes do not satisfy required scopes")
	ErrConnectorCredentialRevoked  = errors.New("connector credential is revoked")
	ErrConnectorCredentialExpired  = errors.New("connector credential is expired")
	ErrConnectorCredentialRotation = errors.New("invalid connector credential rotation")
)

type ConnectorSecurityService struct {
	repo          repository.ConnectorSecurityRepository
	integrations  repository.IntegrationRepository
	orgs          repository.OrganizationRepository
	cipher        *IntegrationCredentialCipher
	client        *http.Client
	allowInsecure bool
}

func NewConnectorSecurityService(
	repo repository.ConnectorSecurityRepository,
	integrations repository.IntegrationRepository,
	orgs repository.OrganizationRepository,
	cipher *IntegrationCredentialCipher,
	allowInsecure bool,
) *ConnectorSecurityService {
	return &ConnectorSecurityService{
		repo: repo, integrations: integrations, orgs: orgs, cipher: cipher,
		client: &http.Client{Timeout: 15 * time.Second}, allowInsecure: allowInsecure,
	}
}

func (s *ConnectorSecurityService) SetHTTPClient(client *http.Client) {
	if client != nil {
		s.client = client
	}
}

func (s *ConnectorSecurityService) SecretBackends() []model.ConnectorSecretBackend {
	return []model.ConnectorSecretBackend{
		{Key: model.ConnectorSecretBackendDatabase, DisplayName: "Database Envelope Encryption", EnvelopeEncryption: true, CustomerManagedKey: true},
		{Key: model.ConnectorSecretBackendAWS, DisplayName: "AWS Secrets Manager", ExternalVault: true, EnvelopeEncryption: true, CustomerManagedKey: true},
		{Key: model.ConnectorSecretBackendAzure, DisplayName: "Azure Key Vault", ExternalVault: true, EnvelopeEncryption: true, CustomerManagedKey: true},
		{Key: model.ConnectorSecretBackendGCP, DisplayName: "GCP Secret Manager", ExternalVault: true, EnvelopeEncryption: true, CustomerManagedKey: true},
		{Key: model.ConnectorSecretBackendVault, DisplayName: "HashiCorp Vault", ExternalVault: true, EnvelopeEncryption: true, CustomerManagedKey: true},
	}
}

func (s *ConnectorSecurityService) BeginOAuth(actorUserID, organizationID, connectionID int64) (model.ConnectorOAuthStart, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.ConnectorOAuthStart{}, err
	}
	connection, err := s.integrations.GetIntegrationConnection(organizationID, connectionID)
	if err != nil {
		return model.ConnectorOAuthStart{}, err
	}
	if connection.AuthType != model.IntegrationAuthOAuth2 {
		return model.ConnectorOAuthStart{}, ErrConnectorOAuthInvalid
	}
	authorizeURL := integrationConfigString(connection.Config, "authorize_url")
	tokenURL := integrationConfigString(connection.Config, "token_url")
	clientID := integrationConfigString(connection.Config, "client_id")
	redirectURI := integrationConfigString(connection.Config, "redirect_uri")
	if err := s.validateOAuthURL(authorizeURL); err != nil {
		return model.ConnectorOAuthStart{}, err
	}
	if err := s.validateOAuthURL(tokenURL); err != nil {
		return model.ConnectorOAuthStart{}, err
	}
	if clientID == "" || redirectURI == "" {
		return model.ConnectorOAuthStart{}, ErrConnectorOAuthInvalid
	}
	if parsed, err := url.Parse(redirectURI); err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return model.ConnectorOAuthStart{}, ErrConnectorOAuthInvalid
	}
	scopes := integrationConfigStrings(connection.Config, "scopes")
	state, err := randomOpaque(32)
	if err != nil {
		return model.ConnectorOAuthStart{}, err
	}
	verifier, err := randomOpaque(48)
	if err != nil {
		return model.ConnectorOAuthStart{}, err
	}
	encryptedVerifier, err := s.cipher.Encrypt([]byte(verifier))
	if err != nil {
		return model.ConnectorOAuthStart{}, err
	}
	stateDigest := sha256.Sum256([]byte(state))
	now := time.Now().UTC()
	expires := now.Add(10 * time.Minute)
	_, err = s.repo.CreateOAuthSession(model.ConnectorOAuthSession{
		OrganizationID: organizationID, ConnectionID: connectionID,
		StateHash: hex.EncodeToString(stateDigest[:]), EncryptedVerifier: encryptedVerifier,
		RedirectURI: redirectURI, RequestedScopes: scopes, CreatedByUserID: actorUserID,
		ExpiresAt: expires, CreatedAt: now,
	})
	if err != nil {
		return model.ConnectorOAuthStart{}, err
	}
	challengeDigest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeDigest[:])
	parsed, _ := url.Parse(authorizeURL)
	q := parsed.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	parsed.RawQuery = q.Encode()
	s.audit(organizationID, actorUserID, "integration.oauth.started", "integration_connection", fmt.Sprint(connectionID), map[string]any{"provider": connection.Provider})
	return model.ConnectorOAuthStart{AuthorizationURL: parsed.String(), State: state, ExpiresAt: expires}, nil
}

func (s *ConnectorSecurityService) CompleteOAuth(actorUserID, organizationID, connectionID int64, req model.CompleteConnectorOAuthRequest) (model.ConnectorCredentialMetadata, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	state := strings.TrimSpace(req.State)
	code := strings.TrimSpace(req.Code)
	if state == "" || code == "" {
		return model.ConnectorCredentialMetadata{}, ErrConnectorOAuthState
	}
	digest := sha256.Sum256([]byte(state))
	session, err := s.repo.ConsumeOAuthSession(hex.EncodeToString(digest[:]), time.Now().UTC())
	if errors.Is(err, repository.ErrConnectorOAuthSessionNotFound) {
		return model.ConnectorCredentialMetadata{}, ErrConnectorOAuthState
	}
	if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	if session.OrganizationID != organizationID || session.ConnectionID != connectionID {
		return model.ConnectorCredentialMetadata{}, ErrConnectorOAuthState
	}
	verifierRaw, err := s.cipher.Decrypt(session.EncryptedVerifier)
	if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	connection, err := s.integrations.GetIntegrationConnection(organizationID, connectionID)
	if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	existing, err := s.readCredentials(connectionID)
	if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("client_id", integrationConfigString(connection.Config, "client_id"))
	values.Set("redirect_uri", session.RedirectURI)
	values.Set("code_verifier", string(verifierRaw))
	if secret := credentialString(existing, "client_secret"); secret != "" {
		values.Set("client_secret", secret)
	}
	token, err := s.exchangeToken(connection, values)
	if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	granted := tokenScopes(token, session.RequestedScopes)
	if !containsAllScopes(granted, session.RequestedScopes) {
		return model.ConnectorCredentialMetadata{}, ErrConnectorScopeMismatch
	}
	credentials := mergeCredentials(existing, token)
	now := time.Now().UTC()
	metadata, err := s.persistCredential(connection, actorUserID, credentials, granted, tokenExpiry(token, now), false)
	if err == nil {
		s.audit(organizationID, actorUserID, "integration.oauth.completed", "integration_connection", fmt.Sprint(connectionID), map[string]any{"scopes": granted})
	}
	return metadata, err
}

func (s *ConnectorSecurityService) Credential(actorUserID, organizationID, connectionID int64) (model.ConnectorCredentialMetadata, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	return s.repo.GetCredentialMetadata(organizationID, connectionID)
}

func (s *ConnectorSecurityService) AccessAudit(actorUserID, organizationID, connectionID int64) ([]model.ConnectorCredentialAccess, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListCredentialAccess(organizationID, connectionID, 200)
}

func (s *ConnectorSecurityService) Rotate(actorUserID, organizationID, connectionID int64, req model.RotateConnectorCredentialRequest) (model.ConnectorCredentialMetadata, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	connection, err := s.integrations.GetIntegrationConnection(organizationID, connectionID)
	if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	credentials, err := s.readCredentials(connectionID)
	if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	meta, err := s.repo.GetCredentialMetadata(organizationID, connectionID)
	if errors.Is(err, repository.ErrConnectorCredentialNotFound) {
		meta = defaultCredentialMetadata(connection)
	} else if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	backend := strings.TrimSpace(req.SecretBackend)
	if backend == "" {
		backend = meta.SecretBackend
	}
	if !validSecretBackend(backend) {
		return model.ConnectorCredentialMetadata{}, ErrConnectorCredentialRotation
	}
	keyVersion := req.KeyVersion
	if keyVersion <= 0 {
		keyVersion = meta.KeyVersion + 1
	}
	raw, _ := json.Marshal(credentials)
	encrypted, err := s.cipher.Encrypt(raw)
	if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	connection.UpdatedByUserID = actorUserID
	connection.UpdatedAt = time.Now().UTC()
	if _, err := s.integrations.UpdateIntegrationConnection(connection, &encrypted); err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	now := time.Now().UTC()
	meta.SecretBackend = backend
	meta.KeyVersion = keyVersion
	meta.CredentialVersion++
	meta.RotatedAt = &now
	meta.UpdatedAt = now
	if meta.Status == "" {
		meta.Status = model.ConnectorCredentialActive
	}
	meta, err = s.repo.UpsertCredentialMetadata(meta)
	if err == nil {
		s.recordAccess(meta, &actorUserID, "rotate")
		s.audit(organizationID, actorUserID, "integration.credential.rotated", "integration_connection", fmt.Sprint(connectionID), map[string]any{"backend": backend, "key_version": keyVersion})
	}
	return meta, err
}

func (s *ConnectorSecurityService) Revoke(actorUserID, organizationID, connectionID int64) (model.ConnectorCredentialMetadata, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	connection, err := s.integrations.GetIntegrationConnection(organizationID, connectionID)
	if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	credentials, err := s.readCredentials(connectionID)
	if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	if revokeURL := integrationConfigString(connection.Config, "revoke_url"); revokeURL != "" {
		if err := s.validateOAuthURL(revokeURL); err != nil {
			return model.ConnectorCredentialMetadata{}, err
		}
		values := url.Values{}
		values.Set("token", firstCredential(credentials, "refresh_token", "access_token"))
		values.Set("client_id", integrationConfigString(connection.Config, "client_id"))
		if secret := credentialString(credentials, "client_secret"); secret != "" {
			values.Set("client_secret", secret)
		}
		request, _ := http.NewRequest(http.MethodPost, revokeURL, strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, requestErr := s.client.Do(request)
		if requestErr != nil {
			return model.ConnectorCredentialMetadata{}, requestErr
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return model.ConnectorCredentialMetadata{}, fmt.Errorf("%w: revoke HTTP %d", ErrConnectorOAuthExchange, resp.StatusCode)
		}
	}
	meta, err := s.repo.GetCredentialMetadata(organizationID, connectionID)
	if errors.Is(err, repository.ErrConnectorCredentialNotFound) {
		meta = defaultCredentialMetadata(connection)
	} else if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	now := time.Now().UTC()
	meta.Status = model.ConnectorCredentialRevoked
	meta.RevokedAt = &now
	meta.UpdatedAt = now
	meta, err = s.repo.UpsertCredentialMetadata(meta)
	if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	connection.Status = model.IntegrationConnectionDisabled
	connection.UpdatedByUserID = actorUserID
	connection.UpdatedAt = now
	_, _ = s.integrations.UpdateIntegrationConnection(connection, nil)
	s.recordAccess(meta, &actorUserID, "revoke")
	s.audit(organizationID, actorUserID, "integration.credential.revoked", "integration_connection", fmt.Sprint(connectionID), nil)
	return meta, nil
}

func (s *ConnectorSecurityService) TestConnection(actorUserID, organizationID, connectionID int64) (model.ConnectorConnectionTest, error) {
	if _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.ConnectorConnectionTest{}, err
	}
	connection, err := s.integrations.GetIntegrationConnection(organizationID, connectionID)
	if err != nil {
		return model.ConnectorConnectionTest{}, err
	}
	meta, err := s.repo.GetCredentialMetadata(organizationID, connectionID)
	if errors.Is(err, repository.ErrConnectorCredentialNotFound) {
		meta = defaultCredentialMetadata(connection)
	} else if err != nil {
		return model.ConnectorConnectionTest{}, err
	}
	now := time.Now().UTC()
	if meta.Status == model.ConnectorCredentialRevoked {
		return model.ConnectorConnectionTest{}, ErrConnectorCredentialRevoked
	}
	if meta.ExpiresAt != nil && !meta.ExpiresAt.After(now) {
		return model.ConnectorConnectionTest{}, ErrConnectorCredentialExpired
	}
	required := integrationConfigStrings(connection.Config, "scopes")
	scopesValid := containsAllScopes(meta.GrantedScopes, required)
	result := model.ConnectorConnectionTest{ConnectionID: connectionID, ScopesValid: scopesValid, CheckedAt: now}
	if !scopesValid {
		result.Message = ErrConnectorScopeMismatch.Error()
		return result, nil
	}
	healthURL := integrationConfigString(connection.Config, "health_url")
	if healthURL == "" {
		result.Healthy = true
		result.Message = "credential metadata and scopes are valid"
		meta.LastValidatedAt = &now
		meta.UpdatedAt = now
		_, _ = s.repo.UpsertCredentialMetadata(meta)
		return result, nil
	}
	if err := s.validateOAuthURL(healthURL); err != nil {
		return model.ConnectorConnectionTest{}, err
	}
	credentials, err := s.readCredentials(connectionID)
	if err != nil {
		return model.ConnectorConnectionTest{}, err
	}
	request, _ := http.NewRequest(http.MethodGet, healthURL, nil)
	if token := credentialString(credentials, "access_token"); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.client.Do(request)
	if err != nil {
		result.Message = err.Error()
		return result, nil
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	resp.Body.Close()
	result.HTTPStatus = resp.StatusCode
	result.Healthy = resp.StatusCode >= 200 && resp.StatusCode < 300
	if !result.Healthy {
		result.Message = fmt.Sprintf("connector health endpoint returned HTTP %d", resp.StatusCode)
	}
	meta.LastValidatedAt = &now
	meta.UpdatedAt = now
	_, _ = s.repo.UpsertCredentialMetadata(meta)
	health := model.IntegrationHealthDegraded
	failures := connection.ConsecutiveFailures + 1
	if result.Healthy {
		health, failures = model.IntegrationHealthHealthy, 0
	}
	_ = s.integrations.UpdateIntegrationHealth(connectionID, health, failures, now, connection.RateLimitRemaining, connection.RateLimitResetAt)
	s.recordAccess(meta, &actorUserID, "connection_test")
	return result, nil
}

func (s *ConnectorSecurityService) RefreshDueSystem(limit int) ([]model.ConnectorCredentialMetadata, error) {
	if limit <= 0 {
		limit = 50
	}
	items, err := s.repo.ListCredentialsDue(time.Now().UTC().Add(5*time.Minute), limit)
	if err != nil {
		return nil, err
	}
	out := make([]model.ConnectorCredentialMetadata, 0, len(items))
	for _, item := range items {
		refreshed, refreshErr := s.refreshOne(context.Background(), item)
		if refreshErr != nil {
			now := time.Now().UTC()
			item.Status = model.ConnectorCredentialError
			if item.ExpiresAt != nil && !item.ExpiresAt.After(now) {
				item.Status = model.ConnectorCredentialExpired
			}
			item.UpdatedAt = now
			_, _ = s.repo.UpsertCredentialMetadata(item)
			continue
		}
		out = append(out, refreshed)
	}
	return out, nil
}

func (s *ConnectorSecurityService) refreshOne(ctx context.Context, meta model.ConnectorCredentialMetadata) (model.ConnectorCredentialMetadata, error) {
	connection, err := s.integrations.GetIntegrationConnection(meta.OrganizationID, meta.ConnectionID)
	if err != nil {
		return meta, err
	}
	credentials, err := s.readCredentials(meta.ConnectionID)
	if err != nil {
		return meta, err
	}
	refreshToken := credentialString(credentials, "refresh_token")
	if refreshToken == "" {
		return meta, ErrConnectorOAuthExchange
	}
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)
	values.Set("client_id", integrationConfigString(connection.Config, "client_id"))
	if secret := credentialString(credentials, "client_secret"); secret != "" {
		values.Set("client_secret", secret)
	}
	token, err := s.exchangeToken(connection, values)
	if err != nil {
		return meta, err
	}
	merged := mergeCredentials(credentials, token)
	granted := tokenScopes(token, meta.GrantedScopes)
	required := integrationConfigStrings(connection.Config, "scopes")
	if !containsAllScopes(granted, required) {
		return meta, ErrConnectorScopeMismatch
	}
	now := time.Now().UTC()
	meta, err = s.persistCredential(connection, 0, merged, granted, tokenExpiry(token, now), true)
	if err == nil {
		s.recordAccess(meta, nil, "automatic_refresh")
	}
	return meta, err
}

func (s *ConnectorSecurityService) persistCredential(connection model.IntegrationConnection, actorUserID int64, credentials map[string]any, scopes []string, expiresAt *time.Time, refreshed bool) (model.ConnectorCredentialMetadata, error) {
	raw, err := json.Marshal(credentials)
	if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	encrypted, err := s.cipher.Encrypt(raw)
	if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	now := time.Now().UTC()
	connection.Status = model.IntegrationConnectionActive
	if actorUserID > 0 {
		connection.UpdatedByUserID = actorUserID
	}
	connection.UpdatedAt = now
	if _, err := s.integrations.UpdateIntegrationConnection(connection, &encrypted); err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	meta, err := s.repo.GetCredentialMetadata(connection.OrganizationID, connection.ID)
	if errors.Is(err, repository.ErrConnectorCredentialNotFound) {
		meta = defaultCredentialMetadata(connection)
	} else if err != nil {
		return model.ConnectorCredentialMetadata{}, err
	}
	meta.Status = model.ConnectorCredentialActive
	meta.GrantedScopes = append([]string(nil), scopes...)
	meta.ExpiresAt = expiresAt
	meta.CredentialVersion++
	meta.UpdatedAt = now
	meta.RevokedAt = nil
	if refreshed {
		meta.LastRefreshAt = &now
	}
	meta, err = s.repo.UpsertCredentialMetadata(meta)
	if err == nil {
		var actor *int64
		if actorUserID > 0 {
			actor = &actorUserID
		}
		s.recordAccess(meta, actor, "write")
	}
	return meta, err
}

func (s *ConnectorSecurityService) readCredentials(connectionID int64) (map[string]any, error) {
	secretRow, err := s.integrations.GetIntegrationConnectionSecret(connectionID)
	if err != nil {
		return nil, err
	}
	raw, err := s.cipher.Decrypt(secretRow.EncryptedCredentials)
	if err != nil {
		return nil, err
	}
	var credentials map[string]any
	if err := json.Unmarshal(raw, &credentials); err != nil {
		return nil, err
	}
	return credentials, nil
}

func (s *ConnectorSecurityService) exchangeToken(connection model.IntegrationConnection, values url.Values) (map[string]any, error) {
	tokenURL := integrationConfigString(connection.Config, "token_url")
	if err := s.validateOAuthURL(tokenURL); err != nil {
		return nil, err
	}
	request, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnectorOAuthExchange, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: HTTP %d", ErrConnectorOAuthExchange, resp.StatusCode)
	}
	var token map[string]any
	if err := json.Unmarshal(raw, &token); err != nil {
		values, parseErr := url.ParseQuery(string(raw))
		if parseErr != nil {
			return nil, ErrConnectorOAuthExchange
		}
		token = make(map[string]any)
		for key := range values {
			token[key] = values.Get(key)
		}
	}
	if strings.TrimSpace(fmt.Sprint(token["access_token"])) == "" {
		return nil, ErrConnectorOAuthExchange
	}
	return token, nil
}

func (s *ConnectorSecurityService) validateOAuthURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && !(s.allowInsecure && parsed.Scheme == "http")) {
		return ErrConnectorOAuthInvalid
	}
	return nil
}

func defaultCredentialMetadata(connection model.IntegrationConnection) model.ConnectorCredentialMetadata {
	return model.ConnectorCredentialMetadata{
		ConnectionID: connection.ID, OrganizationID: connection.OrganizationID,
		SecretBackend: model.ConnectorSecretBackendDatabase,
		SecretRef:     fmt.Sprintf("org/%d/integration/%d", connection.OrganizationID, connection.ID),
		KeyVersion:    1, CredentialVersion: 0, Status: model.ConnectorCredentialActive,
		GrantedScopes: []string{}, UpdatedAt: time.Now().UTC(),
	}
}

func (s *ConnectorSecurityService) recordAccess(meta model.ConnectorCredentialMetadata, actor *int64, action string) {
	_ = s.repo.RecordCredentialAccess(model.ConnectorCredentialAccess{
		OrganizationID: meta.OrganizationID, ConnectionID: meta.ConnectionID,
		ActorUserID: actor, Action: action, SecretBackend: meta.SecretBackend,
		SecretRef: meta.SecretRef, CreatedAt: time.Now().UTC(),
	})
}

func (s *ConnectorSecurityService) requireAdmin(userID, organizationID int64) (model.OrganizationMember, error) {
	member, err := s.orgs.GetMember(organizationID, userID)
	if err != nil {
		return model.OrganizationMember{}, ErrIntegrationForbidden
	}
	switch member.Role {
	case model.OrganizationRoleOwner, model.OrganizationRoleAdmin, model.OrganizationRoleDelegatedAdmin:
		return member, nil
	default:
		return model.OrganizationMember{}, ErrIntegrationForbidden
	}
}

func (s *ConnectorSecurityService) audit(organizationID, actorUserID int64, action, resourceType, resourceID string, metadata map[string]any) {
	var actor *int64
	if actorUserID > 0 {
		actor = &actorUserID
	}
	_ = s.orgs.RecordAudit(model.OrganizationAuditEvent{
		OrganizationID: organizationID, ActorUserID: actor, Action: action,
		ResourceType: resourceType, ResourceID: resourceID, Metadata: metadata, CreatedAt: time.Now().UTC(),
	})
}

func randomOpaque(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func integrationConfigStrings(config map[string]any, key string) []string {
	value, ok := config[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if v := strings.TrimSpace(item); v != "" {
				out = append(out, v)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if v := strings.TrimSpace(fmt.Sprint(item)); v != "" {
				out = append(out, v)
			}
		}
		return out
	case string:
		fields := strings.FieldsFunc(typed, func(r rune) bool { return r == ' ' || r == ',' })
		return fields
	default:
		return nil
	}
}

func containsAllScopes(granted, required []string) bool {
	set := make(map[string]struct{}, len(granted))
	for _, scope := range granted {
		set[strings.TrimSpace(scope)] = struct{}{}
	}
	for _, scope := range required {
		if _, ok := set[strings.TrimSpace(scope)]; !ok {
			return false
		}
	}
	return true
}

func tokenScopes(token map[string]any, fallback []string) []string {
	raw := strings.TrimSpace(fmt.Sprint(token["scope"]))
	if raw == "" || raw == "<nil>" {
		return append([]string(nil), fallback...)
	}
	return strings.FieldsFunc(raw, func(r rune) bool { return r == ' ' || r == ',' })
}

func tokenExpiry(token map[string]any, now time.Time) *time.Time {
	value, ok := token["expires_in"]
	if !ok {
		return nil
	}
	var seconds int64
	switch typed := value.(type) {
	case float64:
		seconds = int64(typed)
	case json.Number:
		seconds, _ = typed.Int64()
	case string:
		seconds, _ = strconv.ParseInt(typed, 10, 64)
	}
	if seconds <= 0 {
		return nil
	}
	expires := now.Add(time.Duration(seconds) * time.Second)
	return &expires
}

func mergeCredentials(existing, token map[string]any) map[string]any {
	out := make(map[string]any, len(existing)+len(token))
	for key, value := range existing {
		out[key] = value
	}
	for key, value := range token {
		if value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
			out[key] = value
		}
	}
	return out
}

func validSecretBackend(value string) bool {
	switch value {
	case model.ConnectorSecretBackendDatabase, model.ConnectorSecretBackendAWS, model.ConnectorSecretBackendAzure, model.ConnectorSecretBackendGCP, model.ConnectorSecretBackendVault:
		return true
	default:
		return false
	}
}
