ALTER TABLE event_schema_versions
    ADD COLUMN IF NOT EXISTS owner_service TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS owner_contact TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS deprecation_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS replacement_schema_id BIGINT,
    ADD COLUMN IF NOT EXISTS deprecated_by_user_id BIGINT;

CREATE INDEX IF NOT EXISTS idx_event_schema_versions_replacement
    ON event_schema_versions (workspace_id, event_type, replacement_schema_id)
    WHERE replacement_schema_id IS NOT NULL;
