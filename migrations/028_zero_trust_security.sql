CREATE TABLE IF NOT EXISTS zero_trust_policies (
    organization_id BIGINT PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    require_workload_mtls BOOLEAN NOT NULL DEFAULT TRUE,
    require_bound_tokens BOOLEAN NOT NULL DEFAULT TRUE,
    step_up_risk_score INTEGER NOT NULL DEFAULT 40 CHECK (step_up_risk_score BETWEEN 1 AND 100),
    revoke_risk_score INTEGER NOT NULL DEFAULT 75 CHECK (revoke_risk_score BETWEEN 1 AND 100),
    impossible_travel_kph INTEGER NOT NULL DEFAULT 900 CHECK (impossible_travel_kph BETWEEN 100 AND 5000),
    waf_block_score INTEGER NOT NULL DEFAULT 80 CHECK (waf_block_score BETWEEN 1 AND 100),
    allowed_cidrs JSONB NOT NULL DEFAULT '[]'::jsonb,
    denied_cidrs JSONB NOT NULL DEFAULT '[]'::jsonb,
    auto_revoke_high_risk BOOLEAN NOT NULL DEFAULT TRUE,
    require_trusted_device BOOLEAN NOT NULL DEFAULT FALSE,
    siem_federation_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    audit_checkpoint_interval INTEGER NOT NULL DEFAULT 100 CHECK (audit_checkpoint_interval BETWEEN 1 AND 10000),
    updated_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS workload_identities (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    spiffe_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    allowed_scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    mtls_required BOOLEAN NOT NULL DEFAULT TRUE,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,
    last_authenticated_at TIMESTAMPTZ,
    UNIQUE (organization_id, spiffe_id)
);

CREATE INDEX IF NOT EXISTS idx_workload_identities_org_status
    ON workload_identities (organization_id, status, id DESC);

CREATE TABLE IF NOT EXISTS workload_certificates (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    workload_id BIGINT NOT NULL REFERENCES workload_identities(id) ON DELETE CASCADE,
    serial_number TEXT NOT NULL,
    sha256_fingerprint CHAR(64) NOT NULL UNIQUE,
    subject TEXT NOT NULL,
    not_before TIMESTAMPTZ NOT NULL,
    not_after TIMESTAMPTZ NOT NULL,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,
    replaced_by_id BIGINT REFERENCES workload_certificates(id)
);

CREATE INDEX IF NOT EXISTS idx_workload_certificates_active
    ON workload_certificates (workload_id, not_after DESC)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS zero_trust_device_trust (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_hash CHAR(64) NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    trust_level TEXT NOT NULL DEFAULT 'trusted',
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,
    UNIQUE (organization_id, user_id, device_hash)
);

CREATE INDEX IF NOT EXISTS idx_zero_trust_device_active
    ON zero_trust_device_trust (organization_id, user_id, expires_at DESC)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS zero_trust_security_events (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id BIGINT REFERENCES users(id),
    session_id BIGINT,
    workload_id BIGINT REFERENCES workload_identities(id),
    type TEXT NOT NULL,
    severity TEXT NOT NULL,
    risk_score INTEGER NOT NULL DEFAULT 0 CHECK (risk_score BETWEEN 0 AND 100),
    action TEXT NOT NULL,
    source_ip TEXT NOT NULL DEFAULT '',
    indicators JSONB NOT NULL DEFAULT '[]'::jsonb,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_zero_trust_events_org
    ON zero_trust_security_events (organization_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_zero_trust_events_risk
    ON zero_trust_security_events (organization_id, risk_score DESC, occurred_at DESC);

CREATE TABLE IF NOT EXISTS zero_trust_audit_checkpoints (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    sequence BIGINT NOT NULL,
    first_event_id BIGINT NOT NULL,
    last_event_id BIGINT NOT NULL,
    event_count INTEGER NOT NULL,
    previous_hash CHAR(64) NOT NULL,
    payload_hash CHAR(64) NOT NULL,
    checkpoint_hash CHAR(64) NOT NULL,
    signature CHAR(64) NOT NULL,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, sequence),
    UNIQUE (organization_id, checkpoint_hash)
);

CREATE TABLE IF NOT EXISTS zero_trust_worm_exports (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    from_event_id BIGINT NOT NULL,
    to_event_id BIGINT NOT NULL,
    event_count INTEGER NOT NULL,
    root_hash CHAR(64) NOT NULL,
    checkpoint_hash CHAR(64) NOT NULL,
    object_uri TEXT NOT NULL,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS zero_trust_siem_destinations (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    endpoint_url TEXT NOT NULL,
    event_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    secret_ref TEXT NOT NULL DEFAULT '',
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    disabled_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_zero_trust_siem_org_enabled
    ON zero_trust_siem_destinations (organization_id, enabled, id DESC);
