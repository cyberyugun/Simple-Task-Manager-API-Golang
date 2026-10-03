CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_tasks_user_title_trgm
    ON tasks USING GIN (title gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_tasks_user_description_trgm
    ON tasks USING GIN (description gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_tasks_user_completed_created_at
    ON tasks (user_id, completed, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_active_last_used
    ON refresh_tokens (user_id, last_used_at DESC, id DESC)
    WHERE revoked_at IS NULL;
