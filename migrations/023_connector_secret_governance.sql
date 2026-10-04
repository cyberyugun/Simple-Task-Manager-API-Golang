CREATE TABLE IF NOT EXISTS integration_oauth_sessions (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    connection_id BIGINT NOT NULL REFERENCES integration_connections(id) ON DELETE CASCADE,
    state_hash TEXT NOT NULL UNIQUE,
    encrypted_verifier TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    requested_scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_integration_oauth_sessions_active
    ON integration_oauth_sessions (expires_at, id)
    WHERE consumed_at IS NULL;

CREATE TABLE IF NOT EXISTS integration_credential_metadata (
    connection_id BIGINT PRIMARY KEY REFERENCES integration_connections(id) ON DELETE CASCADE,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    secret_backend TEXT NOT NULL DEFAULT 'database_envelope'
        CHECK (secret_backend IN ('database_envelope','aws_secrets_manager','azure_key_vault','gcp_secret_manager','hashicorp_vault')),
    secret_ref TEXT NOT NULL,
    key_version INTEGER NOT NULL DEFAULT 1 CHECK (key_version > 0),
    credential_version INTEGER NOT NULL DEFAULT 1 CHECK (credential_version > 0),
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active','expired','revoked','error')),
    granted_scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    expires_at TIMESTAMPTZ,
    last_refresh_at TIMESTAMPTZ,
    last_validated_at TIMESTAMPTZ,
    rotated_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_integration_credentials_refresh_due
    ON integration_credential_metadata (expires_at, connection_id)
    WHERE status = 'active' AND expires_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_integration_credentials_org
    ON integration_credential_metadata (organization_id, status, connection_id);

CREATE TABLE IF NOT EXISTS integration_credential_access_audit (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    connection_id BIGINT NOT NULL REFERENCES integration_connections(id) ON DELETE CASCADE,
    actor_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    secret_backend TEXT NOT NULL,
    secret_ref TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_integration_credential_access_audit
    ON integration_credential_access_audit (organization_id, connection_id, created_at DESC, id DESC);
