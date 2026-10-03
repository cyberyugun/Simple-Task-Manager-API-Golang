CREATE TABLE IF NOT EXISTS workflow_definitions (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    description TEXT NOT NULL DEFAULT '',
    active_version_id BIGINT,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    updated_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_definitions_org
    ON workflow_definitions (organization_id, id);

CREATE TABLE IF NOT EXISTS workflow_versions (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    workflow_id BIGINT NOT NULL REFERENCES workflow_definitions(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version > 0),
    status TEXT NOT NULL CHECK (status IN ('draft','published','archived')),
    graph JSONB NOT NULL DEFAULT '{"nodes":[],"edges":[]}'::jsonb,
    checksum TEXT NOT NULL DEFAULT '',
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    published_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workflow_id, version)
);

CREATE INDEX IF NOT EXISTS idx_workflow_versions_org_workflow
    ON workflow_versions (organization_id, workflow_id, version DESC);

CREATE INDEX IF NOT EXISTS idx_workflow_versions_status
    ON workflow_versions (organization_id, status, id);

ALTER TABLE workflow_definitions
    DROP CONSTRAINT IF EXISTS workflow_definitions_active_version_fk;

ALTER TABLE workflow_definitions
    ADD CONSTRAINT workflow_definitions_active_version_fk
    FOREIGN KEY (active_version_id) REFERENCES workflow_versions(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS workflow_executions (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    workflow_id BIGINT NOT NULL REFERENCES workflow_definitions(id) ON DELETE CASCADE,
    workflow_version_id BIGINT NOT NULL REFERENCES workflow_versions(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN (
        'running','waiting_approval','waiting_delay','succeeded','failed',
        'cancelled','compensating','compensated'
    )),
    trigger_type TEXT NOT NULL CHECK (trigger_type IN ('manual','event','scheduled','connector','internal')),
    trigger_key TEXT NOT NULL DEFAULT '',
    trigger_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    variables JSONB NOT NULL DEFAULT '{}'::jsonb,
    workflow_snapshot JSONB NOT NULL,
    next_node_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    dry_run BOOLEAN NOT NULL DEFAULT FALSE,
    error_message TEXT NOT NULL DEFAULT '',
    requested_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    resume_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_executions_org
    ON workflow_executions (organization_id, started_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_workflow_executions_due
    ON workflow_executions (resume_at, id)
    WHERE status = 'waiting_delay' AND resume_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS workflow_node_executions (
    id BIGSERIAL PRIMARY KEY,
    execution_id BIGINT NOT NULL REFERENCES workflow_executions(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL CHECK (length(trim(node_id)) > 0),
    node_type TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN (
        'running','waiting','retry_wait','succeeded','failed','skipped','compensated'
    )),
    attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt > 0),
    input JSONB NOT NULL DEFAULT '{}'::jsonb,
    output JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT NOT NULL DEFAULT '',
    retry_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_node_executions_execution
    ON workflow_node_executions (execution_id, id);

CREATE INDEX IF NOT EXISTS idx_workflow_node_executions_node
    ON workflow_node_executions (execution_id, node_id, attempt DESC, id DESC);

CREATE TABLE IF NOT EXISTS workflow_approvals (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    execution_id BIGINT NOT NULL REFERENCES workflow_executions(id) ON DELETE CASCADE,
    node_id TEXT NOT NULL CHECK (length(trim(node_id)) > 0),
    status TEXT NOT NULL CHECK (status IN ('pending','approved','rejected')),
    requested_at TIMESTAMPTZ NOT NULL,
    decided_at TIMESTAMPTZ,
    decided_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    comment TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_approvals_execution
    ON workflow_approvals (execution_id, status, id);

CREATE TABLE IF NOT EXISTS workflow_checkpoints (
    id BIGSERIAL PRIMARY KEY,
    execution_id BIGINT NOT NULL REFERENCES workflow_executions(id) ON DELETE CASCADE,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    status TEXT NOT NULL,
    node_id TEXT NOT NULL DEFAULT '',
    variables JSONB NOT NULL DEFAULT '{}'::jsonb,
    next_node_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (execution_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_workflow_checkpoints_execution
    ON workflow_checkpoints (execution_id, sequence);
