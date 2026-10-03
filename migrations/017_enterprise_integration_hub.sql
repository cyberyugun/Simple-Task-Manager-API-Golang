CREATE TABLE IF NOT EXISTS integration_connections (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('slack','microsoft_teams','jira','github','generic_webhook')),
    name TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active','disabled','error')),
    auth_type TEXT NOT NULL CHECK (auth_type IN ('oauth2','bearer_token','webhook_secret')),
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    encrypted_credentials TEXT NOT NULL,
    health_status TEXT NOT NULL DEFAULT 'unknown' CHECK (health_status IN ('unknown','healthy','degraded')),
    last_health_checked_at TIMESTAMPTZ,
    consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    rate_limit_remaining BIGINT,
    rate_limit_reset_at TIMESTAMPTZ,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    updated_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_integration_connections_org_provider
    ON integration_connections (organization_id, provider, status, id);

CREATE TABLE IF NOT EXISTS integration_deliveries (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    connection_id BIGINT NOT NULL REFERENCES integration_connections(id) ON DELETE CASCADE,
    event_key TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL CHECK (status IN ('pending','retry','delivered','dead_letter')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 5 CHECK (max_attempts BETWEEN 1 AND 20),
    available_at TIMESTAMPTZ NOT NULL,
    locked_at TIMESTAMPTZ,
    locked_by TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    last_http_status INTEGER NOT NULL DEFAULT 0,
    delivered_at TIMESTAMPTZ,
    dead_lettered_at TIMESTAMPTZ,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, event_key)
);

CREATE INDEX IF NOT EXISTS idx_integration_deliveries_ready
    ON integration_deliveries (available_at, id)
    WHERE status IN ('pending','retry') AND dead_lettered_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_integration_deliveries_org
    ON integration_deliveries (organization_id, status, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS integration_inbound_events (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    connection_id BIGINT NOT NULL REFERENCES integration_connections(id) ON DELETE CASCADE,
    provider_event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL CHECK (status IN ('accepted','duplicate')),
    received_at TIMESTAMPTZ NOT NULL,
    UNIQUE (connection_id, provider_event_id)
);

CREATE INDEX IF NOT EXISTS idx_integration_inbound_events_org
    ON integration_inbound_events (organization_id, received_at DESC, id DESC);
