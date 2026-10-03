CREATE TABLE IF NOT EXISTS organization_operations_policies (
    organization_id BIGINT PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    monthly_budget_cents BIGINT NOT NULL DEFAULT 10000 CHECK (monthly_budget_cents > 0),
    budget_alert_threshold_percent INTEGER NOT NULL DEFAULT 80 CHECK (budget_alert_threshold_percent BETWEEN 1 AND 100),
    slo_target_basis_points INTEGER NOT NULL DEFAULT 9990 CHECK (slo_target_basis_points BETWEEN 9000 AND 10000),
    incident_escalation_minutes INTEGER NOT NULL DEFAULT 30 CHECK (incident_escalation_minutes BETWEEN 1 AND 10080),
    support_tier TEXT NOT NULL DEFAULT 'standard' CHECK (support_tier IN ('standard','priority','enterprise')),
    support_contact TEXT NOT NULL DEFAULT '',
    updated_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS organization_cost_allocations (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    category TEXT NOT NULL,
    source TEXT NOT NULL,
    amount_cents BIGINT NOT NULL CHECK (amount_cents >= 0),
    currency TEXT NOT NULL DEFAULT 'USD',
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (period_end > period_start)
);

CREATE INDEX IF NOT EXISTS idx_organization_cost_allocations_period
    ON organization_cost_allocations (organization_id, period_start DESC, period_end DESC, id DESC);

CREATE TABLE IF NOT EXISTS organization_operational_alerts (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL,
    type TEXT NOT NULL,
    metric TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('open','acknowledged')),
    message TEXT NOT NULL,
    current_value BIGINT NOT NULL,
    threshold_value BIGINT NOT NULL,
    detected_at TIMESTAMPTZ NOT NULL,
    acknowledged_at TIMESTAMPTZ,
    acknowledged_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (organization_id, fingerprint)
);

CREATE INDEX IF NOT EXISTS idx_organization_operational_alerts_status
    ON organization_operational_alerts (organization_id, status, detected_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS organization_maintenance_windows (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('scheduled','canceled','completed')),
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (ends_at > starts_at)
);

CREATE INDEX IF NOT EXISTS idx_organization_maintenance_windows_time
    ON organization_maintenance_windows (organization_id, starts_at, ends_at);

CREATE TABLE IF NOT EXISTS organization_operational_incidents (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('sev1','sev2','sev3','sev4')),
    status TEXT NOT NULL CHECK (status IN ('open','monitoring','resolved')),
    summary TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_organization_operational_incidents_status
    ON organization_operational_incidents (organization_id, status, started_at DESC, id DESC);
