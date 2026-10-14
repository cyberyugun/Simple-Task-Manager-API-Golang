ALTER TABLE attachments
    ADD COLUMN IF NOT EXISTS version_group_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS previous_attachment_id BIGINT REFERENCES attachments(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS quarantine_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS released_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS released_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS release_note TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS preview_status TEXT NOT NULL DEFAULT 'none'
        CHECK (preview_status IN ('none','pending','processing','ready','failed')),
    ADD COLUMN IF NOT EXISTS preview_object_key TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS preview_content_type TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS preview_width INTEGER NOT NULL DEFAULT 0 CHECK (preview_width >= 0),
    ADD COLUMN IF NOT EXISTS preview_height INTEGER NOT NULL DEFAULT 0 CHECK (preview_height >= 0),
    ADD COLUMN IF NOT EXISTS preview_engine TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS preview_message TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS preview_created_at TIMESTAMPTZ;

UPDATE attachments
SET version_group_id = 'legacy-' || id::text
WHERE version_group_id = '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_attachments_version_group_version
    ON attachments (workspace_id, version_group_id, version)
    WHERE version_group_id <> '';

CREATE INDEX IF NOT EXISTS idx_attachments_version_group
    ON attachments (workspace_id, version_group_id, version DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_attachments_preview_queue
    ON attachments (preview_status, id)
    WHERE deleted_at IS NULL AND status = 'clean' AND preview_status IN ('pending','processing');

CREATE INDEX IF NOT EXISTS idx_attachments_quarantine
    ON attachments (workspace_id, status, id)
    WHERE deleted_at IS NULL AND status = 'quarantined';
