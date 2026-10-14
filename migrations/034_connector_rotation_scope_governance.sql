UPDATE integration_credential_metadata
SET rotated_at = COALESCE(rotated_at, updated_at)
WHERE rotated_at IS NULL;

CREATE TABLE IF NOT EXISTS integration_credential_policies (
    organization_id BIGINT PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    rotation_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    auto_rotate BOOLEAN NOT NULL DEFAULT FALSE,
    rotation_interval_days INTEGER NOT NULL DEFAULT 90 CHECK (rotation_interval_days BETWEEN 1 AND 3650),
    enforce_least_privilege_scopes BOOLEAN NOT NULL DEFAULT FALSE,
    created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (NOT auto_rotate OR rotation_enabled)
);

CREATE INDEX IF NOT EXISTS idx_integration_credential_policies_auto_rotate
    ON integration_credential_policies (organization_id, rotation_interval_days)
    WHERE rotation_enabled = TRUE AND auto_rotate = TRUE;

CREATE INDEX IF NOT EXISTS idx_integration_credentials_rotation_due
    ON integration_credential_metadata (rotated_at, connection_id)
    WHERE status = 'active' AND rotated_at IS NOT NULL;
