ALTER TABLE outbox_events
    ADD COLUMN IF NOT EXISTS correlation_id TEXT,
    ADD COLUMN IF NOT EXISTS causation_id TEXT;

UPDATE outbox_events
SET correlation_id = event_key
WHERE correlation_id IS NULL OR trim(correlation_id) = '';

CREATE INDEX IF NOT EXISTS idx_outbox_events_correlation
    ON outbox_events (workspace_id, correlation_id, id DESC);

CREATE TABLE IF NOT EXISTS event_schema_versions (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL CHECK (length(trim(event_type)) > 0),
    version INTEGER NOT NULL CHECK (version > 0),
    compatibility TEXT NOT NULL CHECK (compatibility IN ('none','backward','forward','full')),
    status TEXT NOT NULL CHECK (status IN ('active','deprecated')),
    owner_name TEXT NOT NULL CHECK (length(trim(owner_name)) > 0),
    description TEXT NOT NULL DEFAULT '',
    schema JSONB NOT NULL CHECK (jsonb_typeof(schema) = 'object'),
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deprecated_at TIMESTAMPTZ,
    UNIQUE (workspace_id, event_type, version)
);

CREATE INDEX IF NOT EXISTS idx_event_schema_versions_lookup
    ON event_schema_versions (workspace_id, event_type, status, version DESC);

CREATE TABLE IF NOT EXISTS event_fabric_subscriptions (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    consumer_key TEXT NOT NULL CHECK (length(trim(consumer_key)) > 0),
    event_types JSONB NOT NULL CHECK (jsonb_typeof(event_types) = 'array'),
    adapter TEXT NOT NULL CHECK (adapter IN ('postgres_outbox','kafka','rabbitmq','nats','cloud_event_bus')),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','paused')),
    max_attempts INTEGER NOT NULL DEFAULT 8 CHECK (max_attempts BETWEEN 1 AND 50),
    retention_days INTEGER NOT NULL DEFAULT 30 CHECK (retention_days BETWEEN 1 AND 3650),
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, consumer_key)
);

CREATE INDEX IF NOT EXISTS idx_event_fabric_subscriptions_workspace
    ON event_fabric_subscriptions (workspace_id, status, id);

CREATE TABLE IF NOT EXISTS event_routes (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    event_pattern TEXT NOT NULL CHECK (length(trim(event_pattern)) > 0),
    subscription_id BIGINT NOT NULL REFERENCES event_fabric_subscriptions(id) ON DELETE CASCADE,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_event_routes_match
    ON event_routes (workspace_id, active, subscription_id, id);

CREATE TABLE IF NOT EXISTS event_consumer_offsets (
    subscription_id BIGINT PRIMARY KEY REFERENCES event_fabric_subscriptions(id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    last_event_id BIGINT NOT NULL DEFAULT 0,
    last_event_key TEXT NOT NULL DEFAULT '',
    last_occurred_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS event_fabric_deliveries (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    subscription_id BIGINT NOT NULL REFERENCES event_fabric_subscriptions(id) ON DELETE CASCADE,
    outbox_event_id BIGINT NOT NULL REFERENCES outbox_events(id) ON DELETE CASCADE,
    event_key TEXT NOT NULL,
    event_type TEXT NOT NULL,
    schema_version INTEGER NOT NULL CHECK (schema_version > 0),
    payload JSONB NOT NULL,
    correlation_id TEXT NOT NULL,
    causation_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('pending','retry','delivered','dead_letter')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL CHECK (max_attempts BETWEEN 1 AND 50),
    available_at TIMESTAMPTZ NOT NULL,
    locked_at TIMESTAMPTZ,
    locked_by TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    delivered_at TIMESTAMPTZ,
    dead_lettered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (subscription_id, outbox_event_id)
);

CREATE INDEX IF NOT EXISTS idx_event_fabric_deliveries_ready
    ON event_fabric_deliveries (available_at, id)
    WHERE status IN ('pending','retry') AND dead_lettered_at IS NULL AND locked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_event_fabric_deliveries_dlq
    ON event_fabric_deliveries (workspace_id, subscription_id, created_at DESC, id DESC)
    WHERE status = 'dead_letter';
