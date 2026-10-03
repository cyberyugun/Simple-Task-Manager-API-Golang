CREATE TABLE IF NOT EXISTS workspaces (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    is_personal BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_workspaces_personal_owner
    ON workspaces (created_by_user_id)
    WHERE is_personal = TRUE;

CREATE TABLE IF NOT EXISTS workspace_members (
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_workspace_members_user
    ON workspace_members (user_id, workspace_id);

ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS workspace_id BIGINT REFERENCES workspaces(id) ON DELETE CASCADE;

ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;

UPDATE tasks
SET created_by_user_id = user_id
WHERE created_by_user_id IS NULL;

INSERT INTO workspaces (name, created_by_user_id, is_personal, created_at, updated_at)
SELECT 'Personal Workspace', u.id, TRUE, NOW(), NOW()
FROM users u
WHERE NOT EXISTS (
    SELECT 1
    FROM workspaces w
    WHERE w.created_by_user_id = u.id
      AND w.is_personal = TRUE
);

INSERT INTO workspace_members (workspace_id, user_id, role, created_at, updated_at)
SELECT w.id, w.created_by_user_id, 'owner', NOW(), NOW()
FROM workspaces w
WHERE w.is_personal = TRUE
ON CONFLICT (workspace_id, user_id) DO NOTHING;

UPDATE tasks t
SET workspace_id = w.id
FROM workspaces w
WHERE t.workspace_id IS NULL
  AND w.created_by_user_id = t.user_id
  AND w.is_personal = TRUE;

CREATE INDEX IF NOT EXISTS idx_tasks_workspace_id
    ON tasks (workspace_id);

CREATE INDEX IF NOT EXISTS idx_tasks_created_by_user_id
    ON tasks (created_by_user_id);

CREATE INDEX IF NOT EXISTS idx_tasks_workspace_completed_created_at
    ON tasks (workspace_id, completed, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS audit_events (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT REFERENCES workspaces(id) ON DELETE SET NULL,
    actor_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL CHECK (length(trim(action)) > 0),
    resource_type TEXT NOT NULL CHECK (length(trim(resource_type)) > 0),
    resource_id TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_events_workspace_created_at
    ON audit_events (workspace_id, created_at DESC);
