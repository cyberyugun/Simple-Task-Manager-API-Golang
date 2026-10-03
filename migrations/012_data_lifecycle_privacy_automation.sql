CREATE TABLE IF NOT EXISTS governance_lifecycle_runs (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('running','completed','skipped','failed')),
    archived_count INTEGER NOT NULL DEFAULT 0,
    purged_count INTEGER NOT NULL DEFAULT 0,
    skipped_reason TEXT NOT NULL DEFAULT '',
    triggered_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_governance_lifecycle_runs_workspace
    ON governance_lifecycle_runs (workspace_id, started_at DESC);

CREATE TABLE IF NOT EXISTS archived_tasks (
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    task_id BIGINT NOT NULL,
    snapshot JSONB NOT NULL,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_archived_tasks_retention
    ON archived_tasks (workspace_id, archived_at);

CREATE TABLE IF NOT EXISTS privacy_export_packages (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    privacy_request_id BIGINT NOT NULL REFERENCES privacy_requests(id) ON DELETE CASCADE,
    subject_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    checksum_sha256 CHAR(64) NOT NULL,
    payload JSONB NOT NULL,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_privacy_export_packages_request
    ON privacy_export_packages (workspace_id, privacy_request_id, created_at DESC);

CREATE TABLE IF NOT EXISTS consent_ledger (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    subject_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('granted','withdrawn')),
    policy_version TEXT NOT NULL,
    source TEXT NOT NULL,
    recorded_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_consent_ledger_subject
    ON consent_ledger (workspace_id, subject_user_id, purpose, recorded_at DESC);
