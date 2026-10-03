CREATE TABLE IF NOT EXISTS workspace_governance_policies (
    workspace_id BIGINT PRIMARY KEY REFERENCES workspaces(id) ON DELETE CASCADE,
    default_classification TEXT NOT NULL DEFAULT 'internal'
        CHECK (default_classification IN ('public','internal','confidential','restricted')),
    audit_retention_days INTEGER NOT NULL DEFAULT 365 CHECK (audit_retention_days BETWEEN 30 AND 3650),
    operational_retention_days INTEGER NOT NULL DEFAULT 365 CHECK (operational_retention_days BETWEEN 1 AND 3650),
    privacy_request_sla_hours INTEGER NOT NULL DEFAULT 720 CHECK (privacy_request_sla_hours BETWEEN 1 AND 2160),
    require_dpa BOOLEAN NOT NULL DEFAULT FALSE,
    restrict_cross_region_transfer BOOLEAN NOT NULL DEFAULT FALSE,
    allowed_data_regions JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS data_inventory_entries (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL,
    field_name TEXT NOT NULL,
    classification TEXT NOT NULL CHECK (classification IN ('public','internal','confidential','restricted')),
    contains_personal_data BOOLEAN NOT NULL DEFAULT FALSE,
    data_region TEXT NOT NULL DEFAULT '',
    retention_days INTEGER NOT NULL CHECK (retention_days BETWEEN 1 AND 3650),
    updated_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, resource_type, field_name)
);

CREATE INDEX IF NOT EXISTS idx_data_inventory_workspace_classification
    ON data_inventory_entries (workspace_id, classification, contains_personal_data);

CREATE TABLE IF NOT EXISTS legal_holds (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    reason TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL DEFAULT '',
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    released_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_legal_holds_workspace_active
    ON legal_holds (workspace_id, created_at DESC)
    WHERE released_at IS NULL;

CREATE TABLE IF NOT EXISTS privacy_requests (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    subject_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request_type TEXT NOT NULL CHECK (request_type IN ('access','export','delete','correct')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','completed','rejected')),
    reason TEXT NOT NULL DEFAULT '',
    requested_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    due_at TIMESTAMPTZ NOT NULL,
    completed_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_privacy_requests_workspace_due
    ON privacy_requests (workspace_id, status, due_at);

CREATE TABLE IF NOT EXISTS compliance_evidence (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    framework TEXT NOT NULL,
    control_id TEXT NOT NULL,
    evidence_type TEXT NOT NULL,
    description TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_compliance_evidence_workspace_control
    ON compliance_evidence (workspace_id, framework, control_id, created_at DESC);
