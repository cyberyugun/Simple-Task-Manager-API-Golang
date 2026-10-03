CREATE TABLE IF NOT EXISTS automation_policies (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    trigger_type TEXT NOT NULL CHECK (trigger_type IN ('operations_alert','billing_usage_percent')),
    trigger_key TEXT NOT NULL,
    comparator TEXT NOT NULL CHECK (comparator IN ('eq','gte','lte')),
    threshold BIGINT NOT NULL,
    action_type TEXT NOT NULL CHECK (action_type IN ('operations.open_incident')),
    action_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    approval_mode TEXT NOT NULL CHECK (approval_mode IN ('automatic','required')),
    cooldown_minutes INTEGER NOT NULL DEFAULT 60 CHECK (cooldown_minutes BETWEEN 1 AND 10080),
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    updated_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_automation_policies_enabled
    ON automation_policies (organization_id, enabled, id);

CREATE TABLE IF NOT EXISTS automation_executions (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    policy_id BIGINT NOT NULL REFERENCES automation_policies(id) ON DELETE CASCADE,
    dedupe_key TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN (
        'pending_approval','approved','rejected','running','succeeded','failed','skipped'
    )),
    trigger_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    action_result JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT NOT NULL DEFAULT '',
    requested_at TIMESTAMPTZ NOT NULL,
    approved_at TIMESTAMPTZ,
    approved_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    rejected_at TIMESTAMPTZ,
    rejected_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    executed_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, dedupe_key)
);

CREATE INDEX IF NOT EXISTS idx_automation_executions_policy
    ON automation_executions (organization_id, policy_id, requested_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_automation_executions_status
    ON automation_executions (organization_id, status, requested_at DESC, id DESC);
