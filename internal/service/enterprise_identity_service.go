package service

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrEnterpriseAccessDenied  = errors.New("enterprise identity administration requires workspace owner or admin")
	ErrInvalidOAuthClient      = errors.New("invalid oauth client configuration")
	ErrInvalidOAuthRequest     = errors.New("invalid oauth request")
	ErrInvalidOAuthSecret      = errors.New("invalid oauth client credentials")
	ErrInvalidScope            = errors.New("invalid or unauthorized scope")
	ErrServiceAccountsBlocked  = errors.New("service accounts are disabled by workspace policy")
	ErrInvalidEnterprisePolicy = errors.New("invalid enterprise security policy")
	ErrInvalidOIDCConnection   = errors.New("invalid oidc connection")
	ErrInvalidSCIMUser         = errors.New("invalid scim user")
)

const authorizationCodeTTL = 5 * time.Minute

var supportedScopes = map[string]bool{
	model.ScopeTasksRead:      true,
	model.ScopeTasksWrite:     true,
	model.ScopeWorkspaceRead:  true,
	model.ScopeWorkspaceAdmin: true,
	model.ScopeAuditRead:      true,
	model.ScopeIdentityAdmin:  true,
}

type EnterpriseIdentityService struct {
	repo       repository.EnterpriseIdentityRepository
	workspaces repository.WorkspaceRepository
	users      repository.UserRepository
	tokens     *auth.TokenManager
}

func NewEnterpriseIdentityService(
	repo repository.EnterpriseIdentityRepository,
	workspaces repository.WorkspaceRepository,
	users repository.UserRepository,
	tokens *auth.TokenManager,
) *EnterpriseIdentityService {
	return &EnterpriseIdentityService{repo: repo, workspaces: workspaces, users: users, tokens: tokens}
}

func (s *EnterpriseIdentityService) CreateOAuthClient(actorUserID, workspaceID int64, req model.CreateOAuthClientRequest) (model.OAuthClientSecretResult, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.OAuthClientSecretResult{}, err
	}
	name := strings.TrimSpace(req.Name)
	redirects := normalizeStrings(req.RedirectURIs)
	scopes, err := normalizeScopes(req.AllowedScopes)
	if name == "" || err != nil || len(redirects) == 0 {
		return model.OAuthClientSecretResult{}, ErrInvalidOAuthClient
	}
	for _, raw := range redirects {
		u, parseErr := url.Parse(raw)
		if parseErr != nil || u.Scheme == "" || u.Host == "" || u.Fragment != "" {
			return model.OAuthClientSecretResult{}, ErrInvalidOAuthClient
		}
	}

	clientRaw, _, err := auth.GenerateOpaqueToken()
	if err != nil {
		return model.OAuthClientSecretResult{}, err
	}
	secretRaw, secretHash, err := auth.GenerateOpaqueToken()
	if err != nil {
		return model.OAuthClientSecretResult{}, err
	}
	now := time.Now()
	client := model.OAuthClient{
		ClientID:        "stm_" + clientRaw,
		WorkspaceID:     workspaceID,
		Name:            name,
		SecretHash:      secretHash,
		RedirectURIs:    redirects,
		AllowedScopes:   scopes,
		CreatedByUserID: actorUserID,
		CreatedAt:       now,
	}
	if err := s.repo.CreateOAuthClient(client); err != nil {
		return model.OAuthClientSecretResult{}, err
	}
	s.audit(workspaceID, actorUserID, "oauth.client.created", "oauth_client", client.ClientID, map[string]any{"name": name, "scopes": scopes})
	return model.OAuthClientSecretResult{Client: client, ClientSecret: secretRaw}, nil
}

func (s *EnterpriseIdentityService) AuthorizeCode(userID, workspaceID int64, req model.OAuthAuthorizeRequest) (model.OAuthAuthorizeResult, error) {
	if _, err := s.workspaces.ResolveAccess(userID, workspaceID, time.Now()); err != nil {
		return model.OAuthAuthorizeResult{}, ErrEnterpriseAccessDenied
	}
	client, err := s.repo.FindOAuthClient(strings.TrimSpace(req.ClientID))
	if err != nil || client.WorkspaceID != workspaceID {
		return model.OAuthAuthorizeResult{}, ErrInvalidOAuthRequest
	}
	if !contains(client.RedirectURIs, strings.TrimSpace(req.RedirectURI)) {
		return model.OAuthAuthorizeResult{}, ErrInvalidOAuthRequest
	}
	scopes, err := requestedScopes(req.Scopes, client.AllowedScopes)
	if err != nil {
		return model.OAuthAuthorizeResult{}, err
	}
	challenge := strings.TrimSpace(req.CodeChallenge)
	if len(challenge) < 43 || len(challenge) > 128 {
		return model.OAuthAuthorizeResult{}, ErrInvalidOAuthRequest
	}

	raw, hash, err := auth.GenerateOpaqueToken()
	if err != nil {
		return model.OAuthAuthorizeResult{}, err
	}
	now := time.Now()
	if err := s.repo.SaveAuthorizationCode(model.OAuthAuthorizationCode{
		CodeHash:      hash,
		ClientID:      client.ClientID,
		UserID:        userID,
		WorkspaceID:   workspaceID,
		RedirectURI:   strings.TrimSpace(req.RedirectURI),
		Scopes:        scopes,
		CodeChallenge: challenge,
		ExpiresAt:     now.Add(authorizationCodeTTL),
		CreatedAt:     now,
	}); err != nil {
		return model.OAuthAuthorizeResult{}, err
	}
	return model.OAuthAuthorizeResult{Code: raw, ExpiresIn: int64(authorizationCodeTTL.Seconds())}, nil
}

func (s *EnterpriseIdentityService) ExchangeToken(req model.OAuthTokenRequest) (model.OAuthTokenResult, error) {
	switch strings.TrimSpace(req.GrantType) {
	case "authorization_code":
		return s.exchangeAuthorizationCode(req)
	case "client_credentials":
		return s.exchangeClientCredentials(req)
	default:
		return model.OAuthTokenResult{}, ErrInvalidOAuthRequest
	}
}

func (s *EnterpriseIdentityService) exchangeAuthorizationCode(req model.OAuthTokenRequest) (model.OAuthTokenResult, error) {
	client, err := s.authenticateClient(req.ClientID, req.ClientSecret)
	if err != nil {
		return model.OAuthTokenResult{}, err
	}
	code, err := s.repo.ConsumeAuthorizationCode(auth.HashOpaqueToken(strings.TrimSpace(req.Code)), time.Now())
	if err != nil {
		return model.OAuthTokenResult{}, ErrInvalidOAuthRequest
	}
	if code.ClientID != client.ClientID || code.RedirectURI != strings.TrimSpace(req.RedirectURI) {
		return model.OAuthTokenResult{}, ErrInvalidOAuthRequest
	}
	if !verifyPKCES256(strings.TrimSpace(req.CodeVerifier), code.CodeChallenge) {
		return model.OAuthTokenResult{}, ErrInvalidOAuthRequest
	}
	user, err := s.users.FindByID(code.UserID)
	if err != nil {
		return model.OAuthTokenResult{}, err
	}
	token, err := s.tokens.GenerateUser(user.ID, user.Email, code.Scopes, code.WorkspaceID)
	if err != nil {
		return model.OAuthTokenResult{}, err
	}
	return model.OAuthTokenResult{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int64(s.tokens.TTL().Seconds()),
		Scopes:      code.Scopes,
	}, nil
}

func (s *EnterpriseIdentityService) exchangeClientCredentials(req model.OAuthTokenRequest) (model.OAuthTokenResult, error) {
	client, err := s.authenticateClient(req.ClientID, req.ClientSecret)
	if err != nil {
		return model.OAuthTokenResult{}, err
	}
	policy, err := s.policy(client.WorkspaceID)
	if err != nil {
		return model.OAuthTokenResult{}, err
	}
	if !policy.AllowServiceAccounts {
		return model.OAuthTokenResult{}, ErrServiceAccountsBlocked
	}
	scopes, err := requestedScopes(req.Scopes, client.AllowedScopes)
	if err != nil {
		return model.OAuthTokenResult{}, err
	}
	token, err := s.tokens.GenerateService(client.ClientID, client.WorkspaceID, scopes, client.CreatedByUserID)
	if err != nil {
		return model.OAuthTokenResult{}, err
	}
	return model.OAuthTokenResult{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int64(s.tokens.TTL().Seconds()),
		Scopes:      scopes,
	}, nil
}

func (s *EnterpriseIdentityService) CreateAPIKey(actorUserID, workspaceID int64, req model.CreateAPIKeyRequest) (model.APIKeySecretResult, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.APIKeySecretResult{}, err
	}
	policy, err := s.policy(workspaceID)
	if err != nil {
		return model.APIKeySecretResult{}, err
	}
	if !policy.AllowServiceAccounts {
		return model.APIKeySecretResult{}, ErrServiceAccountsBlocked
	}
	name := strings.TrimSpace(req.Name)
	scopes, err := normalizeScopes(req.Scopes)
	if name == "" || err != nil || len(scopes) == 0 {
		return model.APIKeySecretResult{}, ErrInvalidOAuthRequest
	}

	raw, _, err := auth.GenerateOpaqueToken()
	if err != nil {
		return model.APIKeySecretResult{}, err
	}
	secret := "stm_key_" + raw
	now := time.Now()
	key := model.ServiceAPIKey{
		WorkspaceID:     workspaceID,
		Name:            name,
		KeyPrefix:       prefix(secret),
		KeyHash:         auth.HashOpaqueToken(secret),
		Scopes:          scopes,
		CreatedByUserID: actorUserID,
		CreatedAt:       now,
	}
	if req.ExpiresIn > 0 {
		expires := now.Add(time.Duration(req.ExpiresIn) * time.Second)
		key.ExpiresAt = &expires
	}
	key, err = s.repo.CreateAPIKey(key)
	if err != nil {
		return model.APIKeySecretResult{}, err
	}
	s.audit(workspaceID, actorUserID, "service_api_key.created", "service_api_key", strconv.FormatInt(key.ID, 10), map[string]any{"name": name, "scopes": scopes})
	return model.APIKeySecretResult{APIKey: key, Secret: secret}, nil
}

func (s *EnterpriseIdentityService) ExchangeAPIKey(raw string) (model.OAuthTokenResult, error) {
	key, err := s.repo.FindAPIKeyByHash(auth.HashOpaqueToken(strings.TrimSpace(raw)), time.Now())
	if err != nil {
		return model.OAuthTokenResult{}, ErrInvalidOAuthSecret
	}
	policy, err := s.policy(key.WorkspaceID)
	if err != nil {
		return model.OAuthTokenResult{}, err
	}
	if !policy.AllowServiceAccounts {
		return model.OAuthTokenResult{}, ErrServiceAccountsBlocked
	}
	clientID := fmt.Sprintf("apikey:%d", key.ID)
	token, err := s.tokens.GenerateService(clientID, key.WorkspaceID, key.Scopes, key.CreatedByUserID)
	if err != nil {
		return model.OAuthTokenResult{}, err
	}
	_ = s.repo.TouchAPIKey(key.ID, time.Now())
	return model.OAuthTokenResult{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresIn:   int64(s.tokens.TTL().Seconds()),
		Scopes:      key.Scopes,
	}, nil
}

func (s *EnterpriseIdentityService) ListAPIKeys(actorUserID, workspaceID int64) ([]model.ServiceAPIKey, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListAPIKeys(workspaceID)
}

func (s *EnterpriseIdentityService) RevokeAPIKey(actorUserID, workspaceID, keyID int64) error {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return err
	}
	if err := s.repo.RevokeAPIKey(workspaceID, keyID, time.Now()); err != nil {
		return err
	}
	s.audit(workspaceID, actorUserID, "service_api_key.revoked", "service_api_key", strconv.FormatInt(keyID, 10), nil)
	return nil
}

func (s *EnterpriseIdentityService) Introspect(raw string) (model.IntrospectionResult, error) {
	claims, err := s.tokens.ParseClaims(strings.TrimSpace(raw))
	if err != nil {
		return model.IntrospectionResult{Active: false}, nil
	}
	revoked, err := s.repo.IsAccessTokenRevoked(claims.JTI, time.Now())
	if err != nil {
		return model.IntrospectionResult{}, err
	}
	if revoked {
		return model.IntrospectionResult{Active: false}, nil
	}
	return model.IntrospectionResult{
		Active:      true,
		Subject:     claims.Subject,
		ClientID:    claims.ClientID,
		WorkspaceID: claims.WorkspaceID,
		Scopes:      claims.Scopes,
		TokenUse:    claims.TokenUse,
		ExpiresAt:   claims.Expires,
	}, nil
}

func (s *EnterpriseIdentityService) RevokeAccessToken(actorUserID int64, raw string) error {
	claims, err := s.tokens.ParseClaims(strings.TrimSpace(raw))
	if err != nil {
		return nil
	}
	if claims.TokenUse == "user" && claims.Subject != strconv.FormatInt(actorUserID, 10) {
		return ErrEnterpriseAccessDenied
	}
	return s.repo.RevokeAccessToken(claims.JTI, time.Unix(claims.Expires, 0), actorUserID, time.Now())
}

func (s *EnterpriseIdentityService) UpdatePolicy(actorUserID, workspaceID int64, req model.UpdateEnterprisePolicyRequest) (model.EnterpriseSecurityPolicy, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.EnterpriseSecurityPolicy{}, err
	}
	if req.MaxSessionAgeMinutes < 5 || req.MaxSessionAgeMinutes > 43200 {
		return model.EnterpriseSecurityPolicy{}, ErrInvalidEnterprisePolicy
	}
	domains := normalizeDomains(req.AllowedEmailDomains)
	policy, err := s.repo.UpsertSecurityPolicy(model.EnterpriseSecurityPolicy{
		WorkspaceID:          workspaceID,
		RequireMFA:           req.RequireMFA,
		AllowServiceAccounts: req.AllowServiceAccounts,
		AllowedEmailDomains:  domains,
		MaxSessionAgeMinutes: req.MaxSessionAgeMinutes,
		UpdatedByUserID:      actorUserID,
		UpdatedAt:            time.Now(),
	})
	if err == nil {
		s.audit(workspaceID, actorUserID, "enterprise.policy.updated", "enterprise_policy", strconv.FormatInt(workspaceID, 10), map[string]any{"require_mfa": req.RequireMFA, "allow_service_accounts": req.AllowServiceAccounts})
	}
	return policy, err
}

func (s *EnterpriseIdentityService) GetPolicy(actorUserID, workspaceID int64) (model.EnterpriseSecurityPolicy, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.EnterpriseSecurityPolicy{}, err
	}
	return s.policy(workspaceID)
}

func (s *EnterpriseIdentityService) ConfigureOIDC(actorUserID, workspaceID int64, req model.ConfigureOIDCRequest) (model.OIDCConnection, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.OIDCConnection{}, err
	}
	issuer := strings.TrimRight(strings.TrimSpace(req.IssuerURL), "/")
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme != "https" || u.Host == "" || strings.TrimSpace(req.ClientID) == "" {
		return model.OIDCConnection{}, ErrInvalidOIDCConnection
	}
	scopes := normalizeStrings(req.Scopes)
	if len(scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}
	connection, err := s.repo.UpsertOIDCConnection(model.OIDCConnection{
		WorkspaceID:     workspaceID,
		IssuerURL:       issuer,
		ClientID:        strings.TrimSpace(req.ClientID),
		ClientSecretRef: strings.TrimSpace(req.ClientSecretRef),
		Scopes:          scopes,
		Enabled:         req.Enabled,
		UpdatedByUserID: actorUserID,
		UpdatedAt:       time.Now(),
	})
	if err == nil {
		s.audit(workspaceID, actorUserID, "oidc.connection.updated", "oidc_connection", strconv.FormatInt(workspaceID, 10), map[string]any{"issuer": issuer, "enabled": req.Enabled})
	}
	return connection, err
}

func (s *EnterpriseIdentityService) GetOIDC(actorUserID, workspaceID int64) (model.OIDCConnection, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.OIDCConnection{}, err
	}
	return s.repo.GetOIDCConnection(workspaceID)
}

func (s *EnterpriseIdentityService) UpsertSCIMUser(actorUserID, workspaceID int64, req model.SCIMUpsertUserRequest) (model.SCIMUser, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return model.SCIMUser{}, err
	}
	externalID := strings.TrimSpace(req.ExternalID)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	name := strings.TrimSpace(req.DisplayName)
	if externalID == "" || name == "" || !isEmail(email) {
		return model.SCIMUser{}, ErrInvalidSCIMUser
	}
	policy, err := s.policy(workspaceID)
	if err != nil {
		return model.SCIMUser{}, err
	}
	if len(policy.AllowedEmailDomains) > 0 && !emailAllowed(email, policy.AllowedEmailDomains) {
		return model.SCIMUser{}, ErrInvalidSCIMUser
	}

	user, err := s.users.FindByEmail(email)
	if errors.Is(err, repository.ErrUserNotFound) {
		randomPassword, _, genErr := auth.GenerateOpaqueToken()
		if genErr != nil {
			return model.SCIMUser{}, genErr
		}
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
		if hashErr != nil {
			return model.SCIMUser{}, hashErr
		}
		now := time.Now()
		user, err = s.users.Create(model.User{Name: name, Email: email, PasswordHash: string(hash), CreatedAt: now, UpdatedAt: now})
	}
	if err != nil {
		return model.SCIMUser{}, err
	}

	_, accessErr := s.workspaces.ResolveAccess(user.ID, workspaceID, time.Now())
	if req.Active && errors.Is(accessErr, repository.ErrWorkspaceNotFound) {
		if _, err := s.workspaces.AddMember(workspaceID, user.ID, model.WorkspaceRoleMember, time.Now()); err != nil && !errors.Is(err, repository.ErrWorkspaceMemberExists) {
			return model.SCIMUser{}, err
		}
	}
	if !req.Active && accessErr == nil {
		if err := s.workspaces.RemoveMember(workspaceID, user.ID); err != nil && !errors.Is(err, repository.ErrWorkspaceMemberNotFound) {
			return model.SCIMUser{}, err
		}
	}

	now := time.Now()
	record, err := s.repo.UpsertSCIMUser(model.SCIMUser{
		WorkspaceID: workspaceID,
		ExternalID:  externalID,
		UserID:      user.ID,
		Email:       email,
		DisplayName: name,
		Active:      req.Active,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err == nil {
		s.audit(workspaceID, actorUserID, "scim.user.upserted", "scim_user", externalID, map[string]any{"email": email, "active": req.Active})
	}
	return record, err
}

func (s *EnterpriseIdentityService) ListSCIMUsers(actorUserID, workspaceID int64) ([]model.SCIMUser, error) {
	if err := s.requireAdmin(actorUserID, workspaceID); err != nil {
		return nil, err
	}
	return s.repo.ListSCIMUsers(workspaceID)
}

func (s *EnterpriseIdentityService) authenticateClient(clientID, secret string) (model.OAuthClient, error) {
	client, err := s.repo.FindOAuthClient(strings.TrimSpace(clientID))
	if err != nil {
		return model.OAuthClient{}, ErrInvalidOAuthSecret
	}
	if auth.HashOpaqueToken(strings.TrimSpace(secret)) != client.SecretHash {
		return model.OAuthClient{}, ErrInvalidOAuthSecret
	}
	return client, nil
}

func (s *EnterpriseIdentityService) policy(workspaceID int64) (model.EnterpriseSecurityPolicy, error) {
	policy, err := s.repo.GetSecurityPolicy(workspaceID)
	if errors.Is(err, repository.ErrEnterprisePolicy) {
		return model.EnterpriseSecurityPolicy{
			WorkspaceID: workspaceID, AllowServiceAccounts: true, MaxSessionAgeMinutes: 43200,
		}, nil
	}
	return policy, err
}

func (s *EnterpriseIdentityService) requireAdmin(userID, workspaceID int64) error {
	access, err := s.workspaces.ResolveAccess(userID, workspaceID, time.Now())
	if err != nil || (access.Role != model.WorkspaceRoleOwner && access.Role != model.WorkspaceRoleAdmin) {
		return ErrEnterpriseAccessDenied
	}
	return nil
}

func (s *EnterpriseIdentityService) audit(workspaceID, actorUserID int64, action, resourceType, resourceID string, metadata map[string]any) {
	wid, uid := workspaceID, actorUserID
	_ = s.workspaces.RecordAudit(model.AuditEvent{
		WorkspaceID: &wid, ActorUserID: &uid, Action: action, ResourceType: resourceType,
		ResourceID: resourceID, Metadata: metadata, CreatedAt: time.Now(),
	})
}

func normalizeScopes(values []string) ([]string, error) {
	values = normalizeStrings(values)
	for _, value := range values {
		if !supportedScopes[value] {
			return nil, ErrInvalidScope
		}
	}
	return values, nil
}

func requestedScopes(requested, allowed []string) ([]string, error) {
	if len(requested) == 0 {
		return append([]string(nil), allowed...), nil
	}
	scopes, err := normalizeScopes(requested)
	if err != nil {
		return nil, err
	}
	for _, scope := range scopes {
		if !contains(allowed, scope) {
			return nil, ErrInvalidScope
		}
	}
	return scopes, nil
}

func normalizeStrings(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func verifyPKCES256(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == challenge
}

func normalizeDomains(values []string) []string {
	result := normalizeStrings(values)
	for i := range result {
		result[i] = strings.TrimPrefix(strings.ToLower(result[i]), "@")
	}
	return result
}

func emailAllowed(email string, domains []string) bool {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	domain := strings.ToLower(parts[1])
	for _, allowed := range domains {
		if domain == strings.ToLower(allowed) {
			return true
		}
	}
	return false
}

func isEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	return err == nil && strings.EqualFold(address.Address, email)
}

func prefix(secret string) string {
	if len(secret) <= 12 {
		return secret
	}
	return secret[:12]
}
