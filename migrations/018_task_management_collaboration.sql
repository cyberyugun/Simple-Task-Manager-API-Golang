ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS status TEXT,
    ADD COLUMN IF NOT EXISTS priority TEXT,
    ADD COLUMN IF NOT EXISTS start_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS due_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS project_id BIGINT,
    ADD COLUMN IF NOT EXISTS list_id BIGINT,
    ADD COLUMN IF NOT EXISTS parent_task_id BIGINT,
    ADD COLUMN IF NOT EXISTS position BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS estimated_minutes INTEGER,
    ADD COLUMN IF NOT EXISTS actual_minutes INTEGER,
    ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1;

UPDATE tasks
SET status = CASE WHEN completed THEN 'DONE' ELSE 'TODO' END
WHERE status IS NULL;

UPDATE tasks
SET priority = 'MEDIUM'
WHERE priority IS NULL;

ALTER TABLE tasks
    ALTER COLUMN status SET DEFAULT 'TODO',
    ALTER COLUMN status SET NOT NULL,
    ALTER COLUMN priority SET DEFAULT 'MEDIUM',
    ALTER COLUMN priority SET NOT NULL;

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_status_check
    CHECK (status IN ('BACKLOG','TODO','IN_PROGRESS','BLOCKED','IN_REVIEW','DONE','ARCHIVED'));

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_priority_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_priority_check
    CHECK (priority IN ('LOW','MEDIUM','HIGH','URGENT'));

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_estimated_minutes_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_estimated_minutes_check
    CHECK (estimated_minutes IS NULL OR estimated_minutes >= 0);

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_actual_minutes_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_actual_minutes_check
    CHECK (actual_minutes IS NULL OR actual_minutes >= 0);

CREATE TABLE IF NOT EXISTS task_projects (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    description TEXT NOT NULL DEFAULT '',
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_projects_workspace
    ON task_projects (workspace_id, archived_at, id);

CREATE TABLE IF NOT EXISTS task_lists (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id BIGINT REFERENCES task_projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    position BIGINT NOT NULL DEFAULT 0,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_lists_workspace_project
    ON task_lists (workspace_id, project_id, archived_at, position, id);

CREATE TABLE IF NOT EXISTS task_labels (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    color TEXT NOT NULL DEFAULT '#808080',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, name)
);

CREATE TABLE IF NOT EXISTS task_label_links (
    task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    label_id BIGINT NOT NULL REFERENCES task_labels(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (task_id, label_id)
);

CREATE TABLE IF NOT EXISTS task_assignees (
    task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    assigned_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (task_id, user_id)
);

CREATE TABLE IF NOT EXISTS task_watchers (
    task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (task_id, user_id)
);

CREATE TABLE IF NOT EXISTS task_comments (
    id BIGSERIAL PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    body TEXT NOT NULL CHECK (length(trim(body)) > 0),
    edited_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_comments_task
    ON task_comments (task_id, deleted_at, created_at, id);

CREATE TABLE IF NOT EXISTS task_dependencies (
    task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    depends_on_task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (task_id, depends_on_task_id),
    CHECK (task_id <> depends_on_task_id)
);

CREATE TABLE IF NOT EXISTS task_recurrence_rules (
    task_id BIGINT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
    frequency TEXT NOT NULL CHECK (frequency IN ('daily','weekly','monthly')),
    interval_count INTEGER NOT NULL DEFAULT 1 CHECK (interval_count BETWEEN 1 AND 365),
    timezone TEXT NOT NULL DEFAULT 'UTC',
    next_run_at TIMESTAMPTZ,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS task_custom_field_definitions (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    field_type TEXT NOT NULL CHECK (field_type IN ('text','number','boolean','date')),
    required BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, name)
);

CREATE TABLE IF NOT EXISTS task_custom_field_values (
    task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    field_id BIGINT NOT NULL REFERENCES task_custom_field_definitions(id) ON DELETE CASCADE,
    value JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (task_id, field_id)
);

CREATE TABLE IF NOT EXISTS task_activities (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    task_id BIGINT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    actor_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL CHECK (length(trim(action)) > 0),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_activities_task
    ON task_activities (task_id, created_at DESC, id DESC);

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_project_fk;
ALTER TABLE tasks ADD CONSTRAINT tasks_project_fk
    FOREIGN KEY (project_id) REFERENCES task_projects(id) ON DELETE SET NULL;

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_list_fk;
ALTER TABLE tasks ADD CONSTRAINT tasks_list_fk
    FOREIGN KEY (list_id) REFERENCES task_lists(id) ON DELETE SET NULL;

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_parent_fk;
ALTER TABLE tasks ADD CONSTRAINT tasks_parent_fk
    FOREIGN KEY (parent_task_id) REFERENCES tasks(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_workspace_status_due
    ON tasks (workspace_id, status, due_at, id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_workspace_project_list
    ON tasks (workspace_id, project_id, list_id, position, id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_tasks_parent
    ON tasks (parent_task_id, position, id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_task_assignees_user
    ON task_assignees (user_id, task_id);

CREATE INDEX IF NOT EXISTS idx_task_watchers_user
    ON task_watchers (user_id, task_id);
