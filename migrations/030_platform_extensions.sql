CREATE TABLE IF NOT EXISTS extension_publishers (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    website_url TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft',
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    reviewed_by_user_id BIGINT REFERENCES users(id),
    review_note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    submitted_at TIMESTAMPTZ,
    reviewed_at TIMESTAMPTZ,
    CHECK (status IN ('draft','submitted','verified','rejected'))
);

CREATE INDEX IF NOT EXISTS idx_extension_publishers_workspace
    ON extension_publishers (workspace_id, status, id);

CREATE TABLE IF NOT EXISTS marketplace_applications (
    id BIGSERIAL PRIMARY KEY,
    publisher_id BIGINT NOT NULL REFERENCES extension_publishers(id) ON DELETE CASCADE,
    publisher_workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    summary TEXT NOT NULL,
    description TEXT NOT NULL,
    version TEXT NOT NULL,
    manifest_version INTEGER NOT NULL DEFAULT 1,
    execution_model TEXT NOT NULL DEFAULT 'remote_webhook_api',
    homepage_url TEXT NOT NULL DEFAULT '',
    privacy_url TEXT NOT NULL DEFAULT '',
    categories JSONB NOT NULL DEFAULT '[]'::jsonb,
    requested_scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    event_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    config_schema JSONB NOT NULL DEFAULT '[]'::jsonb,
    packs JSONB NOT NULL DEFAULT '[]'::jsonb,
    daily_request_limit BIGINT NOT NULL DEFAULT 1000 CHECK (daily_request_limit > 0),
    monthly_request_limit BIGINT NOT NULL DEFAULT 20000 CHECK (monthly_request_limit > 0),
    status TEXT NOT NULL DEFAULT 'draft',
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    reviewed_by_user_id BIGINT REFERENCES users(id),
    review_note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    submitted_at TIMESTAMPTZ,
    reviewed_at TIMESTAMPTZ,
    CHECK (execution_model = 'remote_webhook_api'),
    CHECK (status IN ('draft','submitted','approved','rejected','suspended'))
);

CREATE INDEX IF NOT EXISTS idx_marketplace_apps_publisher
    ON marketplace_applications (publisher_id, status, id);
CREATE INDEX IF NOT EXISTS idx_marketplace_apps_approved
    ON marketplace_applications (status, id)
    WHERE status = 'approved';

CREATE TABLE IF NOT EXISTS extension_installations (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    application_id BIGINT NOT NULL REFERENCES marketplace_applications(id) ON DELETE RESTRICT,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'active',
    granted_scopes JSONB NOT NULL,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    secret_refs JSONB NOT NULL DEFAULT '{}'::jsonb,
    install_secret_hash TEXT NOT NULL UNIQUE,
    install_secret_prefix TEXT NOT NULL,
    daily_request_limit BIGINT NOT NULL CHECK (daily_request_limit > 0),
    monthly_request_limit BIGINT NOT NULL CHECK (monthly_request_limit > 0),
    installed_by_user_id BIGINT NOT NULL REFERENCES users(id),
    installed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    uninstalled_by_user_id BIGINT REFERENCES users(id),
    uninstalled_at TIMESTAMPTZ,
    CHECK (status IN ('active','uninstalled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_extension_installation_active
    ON extension_installations (organization_id, application_id, workspace_id)
    WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_extension_installations_org
    ON extension_installations (organization_id, status, id);

CREATE TABLE IF NOT EXISTS extension_event_subscriptions (
    id BIGSERIAL PRIMARY KEY,
    installation_id BIGINT NOT NULL REFERENCES extension_installations(id) ON DELETE CASCADE,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    webhook_subscription_id BIGINT NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    event_types JSONB NOT NULL,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(installation_id, webhook_subscription_id)
);

CREATE INDEX IF NOT EXISTS idx_extension_event_subscriptions_installation
    ON extension_event_subscriptions (installation_id, id);

CREATE TABLE IF NOT EXISTS extension_usage_daily (
    installation_id BIGINT NOT NULL REFERENCES extension_installations(id) ON DELETE CASCADE,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    usage_date DATE NOT NULL,
    requests BIGINT NOT NULL DEFAULT 0 CHECK (requests >= 0),
    errors BIGINT NOT NULL DEFAULT 0 CHECK (errors >= 0),
    total_latency_ms BIGINT NOT NULL DEFAULT 0 CHECK (total_latency_ms >= 0),
    last_request_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (installation_id, usage_date)
);

CREATE INDEX IF NOT EXISTS idx_extension_usage_org_date
    ON extension_usage_daily (organization_id, usage_date DESC, installation_id);
