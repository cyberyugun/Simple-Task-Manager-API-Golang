CREATE TABLE IF NOT EXISTS outbox_events (
    id BIGSERIAL PRIMARY KEY,
    event_id TEXT NOT NULL UNIQUE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    schema_version INTEGER NOT NULL DEFAULT 1 CHECK (schema_version > 0),
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'processed', 'dead')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at TIMESTAMPTZ,
    processed_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_outbox_events_dispatch
    ON outbox_events (status, available_at, id);

CREATE INDEX IF NOT EXISTS idx_outbox_events_workspace_created
    ON outbox_events (workspace_id, created_at DESC);

CREATE TABLE IF NOT EXISTS webhook_subscriptions (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    url TEXT NOT NULL CHECK (url ~ '^https?://'),
    signing_secret TEXT NOT NULL CHECK (length(signing_secret) >= 32),
    event_types JSONB NOT NULL DEFAULT '["task.*"]'::jsonb,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_subscriptions_workspace_active
    ON webhook_subscriptions (workspace_id, active);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id BIGSERIAL PRIMARY KEY,
    subscription_id BIGINT NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    event_id TEXT NOT NULL REFERENCES outbox_events(event_id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'delivered', 'dead')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    response_status INTEGER,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (subscription_id, event_id)
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_dispatch
    ON webhook_deliveries (status, available_at, id);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_event
    ON webhook_deliveries (event_id);
