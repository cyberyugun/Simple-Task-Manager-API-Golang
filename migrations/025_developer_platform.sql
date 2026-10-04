CREATE TABLE IF NOT EXISTS developer_applications (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','submitted','approved','rejected','suspended')),
    allowed_scopes JSONB NOT NULL CHECK (jsonb_typeof(allowed_scopes) = 'array'),
    daily_request_limit BIGINT NOT NULL DEFAULT 1000 CHECK (daily_request_limit > 0),
    monthly_request_limit BIGINT NOT NULL DEFAULT 20000 CHECK (monthly_request_limit > 0),
    sandbox_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reviewed_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    review_note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    submitted_at TIMESTAMPTZ,
    reviewed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_developer_apps_workspace_status
    ON developer_applications (workspace_id, status, id);

CREATE TABLE IF NOT EXISTS developer_app_credentials (
    id BIGSERIAL PRIMARY KEY,
    app_id BIGINT NOT NULL REFERENCES developer_applications(id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('oauth_client','api_key')),
    environment TEXT NOT NULL CHECK (environment IN ('sandbox','production')),
    external_id TEXT NOT NULL UNIQUE,
    key_prefix TEXT NOT NULL DEFAULT '',
    scopes JSONB NOT NULL CHECK (jsonb_typeof(scopes) = 'array'),
    redirect_uris JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(redirect_uris) = 'array'),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked')),
    rotated_from_id BIGINT REFERENCES developer_app_credentials(id) ON DELETE SET NULL,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_developer_credentials_app
    ON developer_app_credentials (app_id, status, id DESC);

CREATE INDEX IF NOT EXISTS idx_developer_credentials_workspace_external
    ON developer_app_credentials (workspace_id, external_id)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS developer_api_usage_daily (
    app_id BIGINT NOT NULL REFERENCES developer_applications(id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    usage_date DATE NOT NULL,
    requests BIGINT NOT NULL DEFAULT 0 CHECK (requests >= 0),
    errors BIGINT NOT NULL DEFAULT 0 CHECK (errors >= 0),
    total_latency_ms BIGINT NOT NULL DEFAULT 0 CHECK (total_latency_ms >= 0),
    last_request_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (app_id, usage_date)
);

CREATE INDEX IF NOT EXISTS idx_developer_usage_workspace_date
    ON developer_api_usage_daily (workspace_id, usage_date DESC, app_id);

CREATE TABLE IF NOT EXISTS developer_webhook_tests (
    id BIGSERIAL PRIMARY KEY,
    app_id BIGINT NOT NULL REFERENCES developer_applications(id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    dry_run BOOLEAN NOT NULL DEFAULT TRUE,
    status TEXT NOT NULL CHECK (status IN ('validated','delivered','failed')),
    http_status INTEGER,
    response_preview TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_developer_webhook_tests_app
    ON developer_webhook_tests (app_id, created_at DESC, id DESC);
