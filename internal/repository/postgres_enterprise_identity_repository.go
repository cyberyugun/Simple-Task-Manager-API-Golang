package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresEnterpriseIdentityRepository struct {
	db *sql.DB
}

func NewPostgresEnterpriseIdentityRepository(db *sql.DB) *PostgresEnterpriseIdentityRepository {
	return &PostgresEnterpriseIdentityRepository{db: db}
}

func marshalStrings(values []string) ([]byte, error) {
	if values == nil {
		values = []string{}
	}
	return json.Marshal(values)
}

func unmarshalStrings(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	if values == nil {
		values = []string{}
	}
	return values, nil
}

func (r *PostgresEnterpriseIdentityRepository) CreateOAuthClient(client model.OAuthClient) error {
	redirects, err := marshalStrings(client.RedirectURIs)
	if err != nil {
		return err
	}
	scopes, err := marshalStrings(client.AllowedScopes)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO oauth_clients
			(client_id, workspace_id, name, secret_hash, redirect_uris, allowed_scopes, created_by_user_id, created_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8)
	`, client.ClientID, client.WorkspaceID, client.Name, client.SecretHash, string(redirects), string(scopes), client.CreatedByUserID, client.CreatedAt)
	return err
}

func (r *PostgresEnterpriseIdentityRepository) FindOAuthClient(clientID string) (model.OAuthClient, error) {
	var client model.OAuthClient
	var redirects, scopes []byte
	var revoked sql.NullTime
	err := r.db.QueryRow(`
		SELECT client_id, workspace_id, name, secret_hash, redirect_uris, allowed_scopes,
		       created_by_user_id, created_at, revoked_at
		FROM oauth_clients
		WHERE client_id = $1 AND revoked_at IS NULL
	`, clientID).Scan(
		&client.ClientID, &client.WorkspaceID, &client.Name, &client.SecretHash, &redirects, &scopes,
		&client.CreatedByUserID, &client.CreatedAt, &revoked,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.OAuthClient{}, ErrOAuthClientNotFound
	}
	if err != nil {
		return model.OAuthClient{}, err
	}
	if client.RedirectURIs, err = unmarshalStrings(redirects); err != nil {
		return model.OAuthClient{}, err
	}
	if client.AllowedScopes, err = unmarshalStrings(scopes); err != nil {
		return model.OAuthClient{}, err
	}
	if revoked.Valid {
		client.RevokedAt = &revoked.Time
	}
	return client, nil
}

func (r *PostgresEnterpriseIdentityRepository) RevokeOAuthClient(clientID string, now time.Time) error {
	result, err := r.db.Exec(`UPDATE oauth_clients SET revoked_at = COALESCE(revoked_at, $2) WHERE client_id = $1`, clientID, now)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrOAuthClientNotFound
	}
	return nil
}

func (r *PostgresEnterpriseIdentityRepository) SaveAuthorizationCode(code model.OAuthAuthorizationCode) error {
	scopes, err := marshalStrings(code.Scopes)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO oauth_authorization_codes
			(code_hash, client_id, user_id, workspace_id, redirect_uri, scopes, code_challenge, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9)
	`, code.CodeHash, code.ClientID, code.UserID, code.WorkspaceID, code.RedirectURI, string(scopes), code.CodeChallenge, code.ExpiresAt, code.CreatedAt)
	return err
}

func (r *PostgresEnterpriseIdentityRepository) ConsumeAuthorizationCode(codeHash string, now time.Time) (model.OAuthAuthorizationCode, error) {
	var code model.OAuthAuthorizationCode
	var scopes []byte
	var consumed sql.NullTime
	err := r.db.QueryRow(`
		UPDATE oauth_authorization_codes
		SET consumed_at = $2
		WHERE code_hash = $1 AND consumed_at IS NULL AND expires_at > $2
		RETURNING code_hash, client_id, user_id, workspace_id, redirect_uri, scopes,
		          code_challenge, expires_at, consumed_at, created_at
	`, codeHash, now).Scan(
		&code.CodeHash, &code.ClientID, &code.UserID, &code.WorkspaceID, &code.RedirectURI, &scopes,
		&code.CodeChallenge, &code.ExpiresAt, &consumed, &code.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.OAuthAuthorizationCode{}, ErrAuthorizationCode
	}
	if err != nil {
		return model.OAuthAuthorizationCode{}, err
	}
	if code.Scopes, err = unmarshalStrings(scopes); err != nil {
		return model.OAuthAuthorizationCode{}, err
	}
	if consumed.Valid {
		code.ConsumedAt = &consumed.Time
	}
	return code, nil
}

func (r *PostgresEnterpriseIdentityRepository) CreateAPIKey(key model.ServiceAPIKey) (model.ServiceAPIKey, error) {
	scopes, err := marshalStrings(key.Scopes)
	if err != nil {
		return model.ServiceAPIKey{}, err
	}
	var expires sql.NullTime
	if key.ExpiresAt != nil {
		expires = sql.NullTime{Time: *key.ExpiresAt, Valid: true}
	}
	err = r.db.QueryRow(`
		INSERT INTO service_api_keys
			(workspace_id, name, key_prefix, key_hash, scopes, created_by_user_id, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)
		RETURNING id
	`, key.WorkspaceID, key.Name, key.KeyPrefix, key.KeyHash, string(scopes), key.CreatedByUserID, expires, key.CreatedAt).Scan(&key.ID)
	return key, err
}

func (r *PostgresEnterpriseIdentityRepository) FindAPIKeyByHash(keyHash string, now time.Time) (model.ServiceAPIKey, error) {
	var key model.ServiceAPIKey
	var scopes []byte
	var lastUsed, expires, revoked sql.NullTime
	err := r.db.QueryRow(`
		SELECT id, workspace_id, name, key_prefix, key_hash, scopes, created_by_user_id,
		       last_used_at, expires_at, revoked_at, created_at
		FROM service_api_keys
		WHERE key_hash = $1
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > $2)
	`, keyHash, now).Scan(
		&key.ID, &key.WorkspaceID, &key.Name, &key.KeyPrefix, &key.KeyHash, &scopes, &key.CreatedByUserID,
		&lastUsed, &expires, &revoked, &key.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ServiceAPIKey{}, ErrAPIKeyNotFound
	}
	if err != nil {
		return model.ServiceAPIKey{}, err
	}
	if key.Scopes, err = unmarshalStrings(scopes); err != nil {
		return model.ServiceAPIKey{}, err
	}
	if lastUsed.Valid {
		key.LastUsedAt = &lastUsed.Time
	}
	if expires.Valid {
		key.ExpiresAt = &expires.Time
	}
	if revoked.Valid {
		key.RevokedAt = &revoked.Time
	}
	return key, nil
}

func (r *PostgresEnterpriseIdentityRepository) ListAPIKeys(workspaceID int64) ([]model.ServiceAPIKey, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, name, key_prefix, scopes, created_by_user_id,
		       last_used_at, expires_at, revoked_at, created_at
		FROM service_api_keys
		WHERE workspace_id = $1
		ORDER BY id
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.ServiceAPIKey, 0)
	for rows.Next() {
		var key model.ServiceAPIKey
		var scopes []byte
		var lastUsed, expires, revoked sql.NullTime
		if err := rows.Scan(
			&key.ID, &key.WorkspaceID, &key.Name, &key.KeyPrefix, &scopes, &key.CreatedByUserID,
			&lastUsed, &expires, &revoked, &key.CreatedAt,
		); err != nil {
			return nil, err
		}
		if key.Scopes, err = unmarshalStrings(scopes); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			key.LastUsedAt = &lastUsed.Time
		}
		if expires.Valid {
			key.ExpiresAt = &expires.Time
		}
		if revoked.Valid {
			key.RevokedAt = &revoked.Time
		}
		items = append(items, key)
	}
	return items, rows.Err()
}

func (r *PostgresEnterpriseIdentityRepository) RevokeAPIKey(workspaceID, keyID int64, now time.Time) error {
	result, err := r.db.Exec(`
		UPDATE service_api_keys SET revoked_at = COALESCE(revoked_at, $3)
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, keyID, now)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

func (r *PostgresEnterpriseIdentityRepository) TouchAPIKey(keyID int64, now time.Time) error {
	_, err := r.db.Exec(`UPDATE service_api_keys SET last_used_at = $2 WHERE id = $1`, keyID, now)
	return err
}

func (r *PostgresEnterpriseIdentityRepository) RevokeAccessToken(jti string, expiresAt time.Time, revokedByUserID int64, now time.Time) error {
	var actor any
	if revokedByUserID > 0 {
		actor = revokedByUserID
	}
	_, err := r.db.Exec(`
		INSERT INTO revoked_access_tokens (jti, expires_at, revoked_by_user_id, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (jti) DO NOTHING
	`, jti, expiresAt, actor, now)
	return err
}

func (r *PostgresEnterpriseIdentityRepository) IsAccessTokenRevoked(jti string, now time.Time) (bool, error) {
	var exists bool
	err := r.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM revoked_access_tokens
			WHERE jti = $1 AND expires_at > $2
		)
	`, jti, now).Scan(&exists)
	return exists, err
}

func (r *PostgresEnterpriseIdentityRepository) UpsertSecurityPolicy(policy model.EnterpriseSecurityPolicy) (model.EnterpriseSecurityPolicy, error) {
	domains, err := marshalStrings(policy.AllowedEmailDomains)
	if err != nil {
		return model.EnterpriseSecurityPolicy{}, err
	}
	err = r.db.QueryRow(`
		INSERT INTO enterprise_security_policies
			(workspace_id, require_mfa, allow_service_accounts, allowed_email_domains,
			 max_session_age_minutes, updated_by_user_id, updated_at)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7)
		ON CONFLICT (workspace_id) DO UPDATE SET
			require_mfa = EXCLUDED.require_mfa,
			allow_service_accounts = EXCLUDED.allow_service_accounts,
			allowed_email_domains = EXCLUDED.allowed_email_domains,
			max_session_age_minutes = EXCLUDED.max_session_age_minutes,
			updated_by_user_id = EXCLUDED.updated_by_user_id,
			updated_at = EXCLUDED.updated_at
		RETURNING workspace_id, require_mfa, allow_service_accounts, allowed_email_domains,
		          max_session_age_minutes, updated_by_user_id, updated_at
	`, policy.WorkspaceID, policy.RequireMFA, policy.AllowServiceAccounts, string(domains),
		policy.MaxSessionAgeMinutes, policy.UpdatedByUserID, policy.UpdatedAt,
	).Scan(
		&policy.WorkspaceID, &policy.RequireMFA, &policy.AllowServiceAccounts, &domains,
		&policy.MaxSessionAgeMinutes, &policy.UpdatedByUserID, &policy.UpdatedAt,
	)
	if err != nil {
		return model.EnterpriseSecurityPolicy{}, err
	}
	policy.AllowedEmailDomains, err = unmarshalStrings(domains)
	return policy, err
}

func (r *PostgresEnterpriseIdentityRepository) GetSecurityPolicy(workspaceID int64) (model.EnterpriseSecurityPolicy, error) {
	var policy model.EnterpriseSecurityPolicy
	var domains []byte
	err := r.db.QueryRow(`
		SELECT workspace_id, require_mfa, allow_service_accounts, allowed_email_domains,
		       max_session_age_minutes, updated_by_user_id, updated_at
		FROM enterprise_security_policies
		WHERE workspace_id = $1
	`, workspaceID).Scan(
		&policy.WorkspaceID, &policy.RequireMFA, &policy.AllowServiceAccounts, &domains,
		&policy.MaxSessionAgeMinutes, &policy.UpdatedByUserID, &policy.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.EnterpriseSecurityPolicy{}, ErrEnterprisePolicy
	}
	if err != nil {
		return model.EnterpriseSecurityPolicy{}, err
	}
	policy.AllowedEmailDomains, err = unmarshalStrings(domains)
	return policy, err
}

func (r *PostgresEnterpriseIdentityRepository) UpsertOIDCConnection(connection model.OIDCConnection) (model.OIDCConnection, error) {
	scopes, err := marshalStrings(connection.Scopes)
	if err != nil {
		return model.OIDCConnection{}, err
	}
	err = r.db.QueryRow(`
		INSERT INTO oidc_connections
			(workspace_id, issuer_url, client_id, client_secret_ref, scopes, enabled, updated_by_user_id, updated_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)
		ON CONFLICT (workspace_id) DO UPDATE SET
			issuer_url = EXCLUDED.issuer_url,
			client_id = EXCLUDED.client_id,
			client_secret_ref = EXCLUDED.client_secret_ref,
			scopes = EXCLUDED.scopes,
			enabled = EXCLUDED.enabled,
			updated_by_user_id = EXCLUDED.updated_by_user_id,
			updated_at = EXCLUDED.updated_at
		RETURNING workspace_id, issuer_url, client_id, client_secret_ref, scopes,
		          enabled, updated_by_user_id, updated_at
	`, connection.WorkspaceID, connection.IssuerURL, connection.ClientID, connection.ClientSecretRef,
		string(scopes), connection.Enabled, connection.UpdatedByUserID, connection.UpdatedAt,
	).Scan(
		&connection.WorkspaceID, &connection.IssuerURL, &connection.ClientID, &connection.ClientSecretRef,
		&scopes, &connection.Enabled, &connection.UpdatedByUserID, &connection.UpdatedAt,
	)
	if err != nil {
		return model.OIDCConnection{}, err
	}
	connection.Scopes, err = unmarshalStrings(scopes)
	return connection, err
}

func (r *PostgresEnterpriseIdentityRepository) GetOIDCConnection(workspaceID int64) (model.OIDCConnection, error) {
	var connection model.OIDCConnection
	var scopes []byte
	err := r.db.QueryRow(`
		SELECT workspace_id, issuer_url, client_id, client_secret_ref, scopes,
		       enabled, updated_by_user_id, updated_at
		FROM oidc_connections
		WHERE workspace_id = $1
	`, workspaceID).Scan(
		&connection.WorkspaceID, &connection.IssuerURL, &connection.ClientID, &connection.ClientSecretRef,
		&scopes, &connection.Enabled, &connection.UpdatedByUserID, &connection.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.OIDCConnection{}, ErrOIDCConnection
	}
	if err != nil {
		return model.OIDCConnection{}, err
	}
	connection.Scopes, err = unmarshalStrings(scopes)
	return connection, err
}

func (r *PostgresEnterpriseIdentityRepository) UpsertSCIMUser(user model.SCIMUser) (model.SCIMUser, error) {
	err := r.db.QueryRow(`
		INSERT INTO scim_users
			(workspace_id, external_id, user_id, email, display_name, active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (workspace_id, external_id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			email = EXCLUDED.email,
			display_name = EXCLUDED.display_name,
			active = EXCLUDED.active,
			updated_at = EXCLUDED.updated_at
		RETURNING id, workspace_id, external_id, user_id, email, display_name, active, created_at, updated_at
	`, user.WorkspaceID, user.ExternalID, user.UserID, user.Email, user.DisplayName, user.Active, user.CreatedAt, user.UpdatedAt).Scan(
		&user.ID, &user.WorkspaceID, &user.ExternalID, &user.UserID, &user.Email, &user.DisplayName,
		&user.Active, &user.CreatedAt, &user.UpdatedAt,
	)
	return user, err
}

func (r *PostgresEnterpriseIdentityRepository) ListSCIMUsers(workspaceID int64) ([]model.SCIMUser, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, external_id, user_id, email, display_name, active, created_at, updated_at
		FROM scim_users
		WHERE workspace_id = $1
		ORDER BY id
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.SCIMUser, 0)
	for rows.Next() {
		var user model.SCIMUser
		if err := rows.Scan(&user.ID, &user.WorkspaceID, &user.ExternalID, &user.UserID, &user.Email,
			&user.DisplayName, &user.Active, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, user)
	}
	return items, rows.Err()
}

func (r *PostgresEnterpriseIdentityRepository) FindSCIMUser(workspaceID int64, externalID string) (model.SCIMUser, error) {
	var user model.SCIMUser
	err := r.db.QueryRow(`
		SELECT id, workspace_id, external_id, user_id, email, display_name, active, created_at, updated_at
		FROM scim_users
		WHERE workspace_id = $1 AND external_id = $2
	`, workspaceID, externalID).Scan(
		&user.ID, &user.WorkspaceID, &user.ExternalID, &user.UserID, &user.Email,
		&user.DisplayName, &user.Active, &user.CreatedAt, &user.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.SCIMUser{}, ErrSCIMUserNotFound
	}
	return user, err
}
