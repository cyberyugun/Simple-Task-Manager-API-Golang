CREATE TABLE IF NOT EXISTS data_platform_connections (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    target TEXT NOT NULL,
    bi_contracts JSONB NOT NULL DEFAULT '[]'::jsonb,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    secret_ref TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    masking JSONB NOT NULL DEFAULT '{"mode":"none","fields":[]}'::jsonb,
    freshness_slo_minutes INTEGER NOT NULL DEFAULT 1440 CHECK (freshness_slo_minutes BETWEEN 1 AND 10080),
    max_monthly_cost_usd NUMERIC(12,2) NOT NULL DEFAULT 0 CHECK (max_monthly_cost_usd >= 0),
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_data_platform_connections_org
    ON data_platform_connections (organization_id, status, id);

CREATE TABLE IF NOT EXISTS data_platform_schema_versions (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    connection_id BIGINT NOT NULL REFERENCES data_platform_connections(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    compatibility TEXT NOT NULL DEFAULT 'backward',
    status TEXT NOT NULL DEFAULT 'active',
    fields JSONB NOT NULL,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deprecated_at TIMESTAMPTZ,
    UNIQUE(connection_id, version)
);

CREATE INDEX IF NOT EXISTS idx_data_platform_schema_active
    ON data_platform_schema_versions (connection_id, version DESC)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS data_export_jobs (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    connection_id BIGINT NOT NULL REFERENCES data_platform_connections(id) ON DELETE CASCADE,
    mode TEXT NOT NULL,
    status TEXT NOT NULL,
    schema_version INTEGER NOT NULL,
    requested_by_user_id BIGINT NOT NULL REFERENCES users(id),
    rows INTEGER NOT NULL DEFAULT 0,
    bytes BIGINT NOT NULL DEFAULT 0,
    estimated_cost_usd NUMERIC(12,6) NOT NULL DEFAULT 0,
    delivery_uri TEXT NOT NULL DEFAULT '',
    payload_hash TEXT NOT NULL DEFAULT '',
    checkpoint_before TEXT NOT NULL DEFAULT '',
    checkpoint_after TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_data_export_jobs_org
    ON data_export_jobs (organization_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_data_export_jobs_connection
    ON data_export_jobs (connection_id, id DESC);

CREATE TABLE IF NOT EXISTS data_export_checkpoints (
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    connection_id BIGINT PRIMARY KEY REFERENCES data_platform_connections(id) ON DELETE CASCADE,
    version INTEGER NOT NULL DEFAULT 1,
    last_updated_at TIMESTAMPTZ NOT NULL,
    last_record_key TEXT NOT NULL DEFAULT '',
    last_job_id BIGINT NOT NULL REFERENCES data_export_jobs(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS data_lineage_records (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    connection_id BIGINT NOT NULL REFERENCES data_platform_connections(id) ON DELETE CASCADE,
    export_job_id BIGINT NOT NULL REFERENCES data_export_jobs(id) ON DELETE CASCADE,
    source_datasets JSONB NOT NULL,
    target_dataset TEXT NOT NULL,
    fields JSONB NOT NULL,
    governance_tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_data_lineage_org
    ON data_lineage_records (organization_id, id DESC);

CREATE TABLE IF NOT EXISTS reverse_etl_hooks (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    connection_id BIGINT NOT NULL REFERENCES data_platform_connections(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    source_object TEXT NOT NULL,
    destination TEXT NOT NULL,
    field_mapping JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reverse_etl_hooks_org
    ON reverse_etl_hooks (organization_id, enabled, id);
