CREATE TABLE IF NOT EXISTS attachments (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    task_id BIGINT REFERENCES tasks(id) ON DELETE CASCADE,
    comment_id BIGINT REFERENCES task_comments(id) ON DELETE CASCADE,
    uploaded_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    provider TEXT NOT NULL,
    bucket TEXT NOT NULL DEFAULT '',
    object_key TEXT NOT NULL,
    file_name TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    sha256 TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN (
        'upload_pending','uploaded','scanning','clean','infected','quarantined','deleted'
    )),
    scan_engine TEXT NOT NULL DEFAULT '',
    scan_message TEXT NOT NULL DEFAULT '',
    encryption TEXT NOT NULL DEFAULT '',
    encryption_key_id TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    legal_hold BOOLEAN NOT NULL DEFAULT FALSE,
    retain_until TIMESTAMPTZ,
    uploaded_at TIMESTAMPTZ,
    scanned_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (task_id IS NOT NULL OR comment_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_attachments_workspace_task
    ON attachments (workspace_id, task_id, id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_attachments_workspace_comment
    ON attachments (workspace_id, comment_id, id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_attachments_scan_queue
    ON attachments (status, id)
    WHERE status IN ('uploaded','scanning');

CREATE INDEX IF NOT EXISTS idx_attachments_retention
    ON attachments (retain_until, id)
    WHERE deleted_at IS NULL AND legal_hold = FALSE AND retain_until IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_attachments_dedupe
    ON attachments (workspace_id, sha256, size_bytes, id)
    WHERE status = 'clean' AND deleted_at IS NULL;
