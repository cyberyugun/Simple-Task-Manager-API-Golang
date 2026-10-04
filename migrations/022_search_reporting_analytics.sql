ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS search_vector tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('simple', COALESCE(title, '')), 'A') ||
        setweight(to_tsvector('simple', COALESCE(description, '')), 'B')
    ) STORED;

CREATE INDEX IF NOT EXISTS idx_tasks_search_vector
    ON tasks USING GIN (search_vector);

CREATE INDEX IF NOT EXISTS idx_tasks_workspace_analytics
    ON tasks (workspace_id, status, priority, created_at, completed_at, due_at)
    WHERE deleted_at IS NULL AND archived_at IS NULL;

CREATE TABLE IF NOT EXISTS saved_search_views (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    query JSONB NOT NULL DEFAULT '{}'::jsonb,
    shared BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_saved_search_views_workspace_user
    ON saved_search_views (workspace_id, user_id, shared, id);

CREATE TABLE IF NOT EXISTS scheduled_reports (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    format TEXT NOT NULL CHECK (format IN ('json', 'csv')),
    frequency TEXT NOT NULL CHECK (frequency IN ('daily', 'weekly')),
    timezone TEXT NOT NULL DEFAULT 'UTC',
    hour INTEGER NOT NULL DEFAULT 8 CHECK (hour BETWEEN 0 AND 23),
    filters JSONB NOT NULL DEFAULT '{}'::jsonb,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    next_run_at TIMESTAMPTZ NOT NULL,
    last_run_at TIMESTAMPTZ,
    last_status TEXT NOT NULL DEFAULT 'pending'
        CHECK (last_status IN ('pending', 'running', 'succeeded', 'failed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_scheduled_reports_due
    ON scheduled_reports (next_run_at, id)
    WHERE active = TRUE;

CREATE INDEX IF NOT EXISTS idx_scheduled_reports_workspace_user
    ON scheduled_reports (workspace_id, user_id, id);

CREATE TABLE IF NOT EXISTS report_runs (
    id BIGSERIAL PRIMARY KEY,
    schedule_id BIGINT NOT NULL REFERENCES scheduled_reports(id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
    format TEXT NOT NULL CHECK (format IN ('json', 'csv')),
    row_count INTEGER NOT NULL DEFAULT 0 CHECK (row_count >= 0),
    payload BYTEA,
    error TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_report_runs_workspace_created
    ON report_runs (workspace_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_report_runs_schedule
    ON report_runs (schedule_id, created_at DESC, id DESC);
