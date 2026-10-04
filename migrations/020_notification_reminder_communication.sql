CREATE TABLE IF NOT EXISTS notification_preferences (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    locale TEXT NOT NULL DEFAULT 'en',
    timezone TEXT NOT NULL DEFAULT 'UTC',
    quiet_hours_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    quiet_start TEXT NOT NULL DEFAULT '22:00',
    quiet_end TEXT NOT NULL DEFAULT '07:00',
    digest_frequency TEXT NOT NULL DEFAULT 'off'
        CHECK (digest_frequency IN ('off','daily','weekly')),
    digest_hour INTEGER NOT NULL DEFAULT 8 CHECK (digest_hour BETWEEN 0 AND 23),
    channels JSONB NOT NULL DEFAULT '{"in_app":true,"email":false,"push":false,"webhook":false}'::jsonb,
    events JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (jsonb_typeof(channels) = 'object'),
    CHECK (jsonb_typeof(events) = 'object')
);

CREATE TABLE IF NOT EXISTS notification_endpoints (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel TEXT NOT NULL CHECK (channel IN ('email','push','webhook')),
    address TEXT NOT NULL CHECK (length(trim(address)) > 0),
    secret TEXT NOT NULL DEFAULT '',
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_notification_endpoints_user
    ON notification_endpoints (user_id, channel, active, id)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_notification_endpoint_active
    ON notification_endpoints (user_id, channel, address)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS notification_templates (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
    template_key TEXT NOT NULL CHECK (length(trim(template_key)) > 0),
    locale TEXT NOT NULL CHECK (length(trim(locale)) > 0),
    channel TEXT NOT NULL CHECK (channel IN ('in_app','email','push','webhook')),
    version INTEGER NOT NULL CHECK (version > 0),
    status TEXT NOT NULL CHECK (status IN ('draft','published','archived')),
    subject TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL CHECK (length(body) > 0),
    created_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    published_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_notification_template_org_version
    ON notification_templates (organization_id, template_key, locale, channel, version)
    WHERE organization_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_notification_template_global_version
    ON notification_templates (template_key, locale, channel, version)
    WHERE organization_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_notification_template_lookup
    ON notification_templates (organization_id, template_key, locale, channel, status, version DESC);

CREATE TABLE IF NOT EXISTS notifications (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT REFERENCES organizations(id) ON DELETE SET NULL,
    workspace_id BIGINT REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL CHECK (length(trim(event_type)) > 0),
    template_key TEXT NOT NULL CHECK (length(trim(template_key)) > 0),
    title TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    dedupe_key TEXT NOT NULL CHECK (length(trim(dedupe_key)) > 0),
    read_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, dedupe_key)
);

CREATE INDEX IF NOT EXISTS idx_notifications_user_recent
    ON notifications (user_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_notifications_user_unread
    ON notifications (user_id, created_at DESC, id DESC)
    WHERE read_at IS NULL;

CREATE TABLE IF NOT EXISTS notification_deliveries (
    id BIGSERIAL PRIMARY KEY,
    notification_id BIGINT NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel TEXT NOT NULL CHECK (channel IN ('email','push','webhook')),
    destination TEXT NOT NULL CHECK (length(trim(destination)) > 0),
    endpoint_id BIGINT REFERENCES notification_endpoints(id) ON DELETE SET NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','retry','sent','dead_letter')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 8 CHECK (max_attempts BETWEEN 1 AND 50),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error TEXT NOT NULL DEFAULT '',
    sent_at TIMESTAMPTZ,
    locked_at TIMESTAMPTZ,
    locked_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (notification_id, channel, destination)
);

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_ready
    ON notification_deliveries (available_at, id)
    WHERE status IN ('pending','retry') AND locked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_user
    ON notification_deliveries (user_id, id DESC);

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_dead
    ON notification_deliveries (user_id, updated_at DESC, id DESC)
    WHERE status = 'dead_letter';
