CREATE TABLE IF NOT EXISTS notification_suppression_rules (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL CHECK (length(trim(event_type)) > 0),
    channel TEXT NOT NULL CHECK (channel IN ('all','email','push','webhook')),
    reason TEXT NOT NULL CHECK (length(trim(reason)) > 0),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    starts_at TIMESTAMPTZ,
    ends_at TIMESTAMPTZ,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (starts_at IS NULL OR ends_at IS NULL OR ends_at > starts_at)
);

CREATE INDEX IF NOT EXISTS idx_notification_suppression_rules_active
    ON notification_suppression_rules (organization_id, event_type, channel, starts_at, ends_at, id)
    WHERE active = TRUE;

CREATE TABLE IF NOT EXISTS notification_suppression_events (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    workspace_id BIGINT REFERENCES workspaces(id) ON DELETE SET NULL,
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    notification_id BIGINT REFERENCES notifications(id) ON DELETE SET NULL,
    rule_id BIGINT REFERENCES notification_suppression_rules(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL CHECK (length(trim(event_type)) > 0),
    channel TEXT NOT NULL CHECK (channel IN ('all','email','push','webhook')),
    reason TEXT NOT NULL CHECK (length(trim(reason)) > 0),
    dedupe_key TEXT NOT NULL CHECK (length(trim(dedupe_key)) > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, dedupe_key)
);

CREATE INDEX IF NOT EXISTS idx_notification_suppression_events_org_created
    ON notification_suppression_events (organization_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_notification_suppression_events_org_event
    ON notification_suppression_events (organization_id, event_type, channel, created_at DESC);
