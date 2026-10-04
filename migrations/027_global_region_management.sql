CREATE TABLE IF NOT EXISTS organization_region_policies (
    organization_id BIGINT PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    home_region TEXT NOT NULL,
    allowed_regions JSONB NOT NULL DEFAULT '[]'::jsonb,
    failover_regions JSONB NOT NULL DEFAULT '[]'::jsonb,
    data_residency_enforced BOOLEAN NOT NULL DEFAULT TRUE,
    cross_region_approval_required BOOLEAN NOT NULL DEFAULT TRUE,
    rpo_seconds INTEGER NOT NULL DEFAULT 300 CHECK (rpo_seconds > 0),
    rto_seconds INTEGER NOT NULL DEFAULT 1800 CHECK (rto_seconds > 0),
    updated_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS regional_placements (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    primary_region TEXT NOT NULL,
    replica_regions JSONB NOT NULL DEFAULT '[]'::jsonb,
    status TEXT NOT NULL DEFAULT 'active',
    last_replicated_at TIMESTAMPTZ,
    last_verified_at TIMESTAMPTZ,
    updated_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, resource_type, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_regional_placements_org_region
    ON regional_placements (organization_id, primary_region);

CREATE TABLE IF NOT EXISTS region_migrations (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    scope TEXT NOT NULL,
    resource_type TEXT NOT NULL DEFAULT '',
    resource_id TEXT NOT NULL DEFAULT '',
    source_region TEXT NOT NULL,
    target_region TEXT NOT NULL,
    status TEXT NOT NULL,
    reason TEXT NOT NULL,
    checkpoint JSONB NOT NULL DEFAULT '{}'::jsonb,
    requested_by_user_id BIGINT NOT NULL REFERENCES users(id),
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_by_user_id BIGINT REFERENCES users(id),
    decision_comment TEXT NOT NULL DEFAULT '',
    decided_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_region_migrations_org_status
    ON region_migrations (organization_id, status, id DESC);

CREATE TABLE IF NOT EXISTS cross_region_transfers (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    classification TEXT NOT NULL,
    source_region TEXT NOT NULL,
    target_region TEXT NOT NULL,
    status TEXT NOT NULL,
    reason TEXT NOT NULL,
    requested_by_user_id BIGINT NOT NULL REFERENCES users(id),
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_by_user_id BIGINT REFERENCES users(id),
    decision_comment TEXT NOT NULL DEFAULT '',
    decided_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cross_region_transfers_org_status
    ON cross_region_transfers (organization_id, status, id DESC);

CREATE TABLE IF NOT EXISTS region_failover_exercises (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    source_region TEXT NOT NULL,
    target_region TEXT NOT NULL,
    status TEXT NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    achieved_rpo_seconds INTEGER NOT NULL DEFAULT 0,
    achieved_rto_seconds INTEGER NOT NULL DEFAULT 0,
    meets_rpo BOOLEAN NOT NULL DEFAULT FALSE,
    meets_rto BOOLEAN NOT NULL DEFAULT FALSE,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_region_failover_exercises_org
    ON region_failover_exercises (organization_id, id DESC);
