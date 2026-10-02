CREATE INDEX IF NOT EXISTS idx_tasks_user_created_at
    ON tasks (user_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_tasks_user_updated_at
    ON tasks (user_id, updated_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_tasks_user_title_lower
    ON tasks (user_id, LOWER(title), id);
