CREATE TABLE IF NOT EXISTS notification_preferences (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
    in_app_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    email_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    push_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    webhook_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    digest TEXT NOT NULL DEFAULT 'immediate'
        CHECK (digest IN ('immediate','hourly','daily')),
    timezone TEXT NOT NULL DEFAULT 'UTC',
    locale TEXT NOT NULL DEFAULT 'en',
    quiet_hours_start TIME,
    quiet_hours_end TIME,
    muted_event_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_preferences_scope
    ON notification_preferences (user_id, COALESCE(organization_id, 0));

CREATE TABLE IF NOT EXISTS notification_templates (
    id BIGSERIAL PRIMARY KEY,
    template_key TEXT NOT NULL,
    channel TEXT NOT NULL CHECK (channel IN ('in_app','email','push','webhook')),
    locale TEXT NOT NULL DEFAULT 'en',
    version INTEGER NOT NULL CHECK (version > 0),
    subject TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (template_key, channel, locale, version)
);

CREATE INDEX IF NOT EXISTS idx_notification_templates_active
    ON notification_templates (template_key, channel, locale, active, version DESC);

CREATE TABLE IF NOT EXISTS notifications (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
    workspace_id BIGINT REFERENCES workspaces(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    dedup_key TEXT NOT NULL DEFAULT '',
    template_key TEXT NOT NULL DEFAULT '',
    template_version INTEGER NOT NULL DEFAULT 0,
    read_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_notifications_dedup
    ON notifications (user_id, dedup_key)
    WHERE dedup_key <> '';

CREATE INDEX IF NOT EXISTS idx_notifications_inbox
    ON notifications (user_id, read_at, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_notifications_org
    ON notifications (organization_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS notification_deliveries (
    id BIGSERIAL PRIMARY KEY,
    notification_id BIGINT NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel TEXT NOT NULL CHECK (channel IN ('in_app','email','push','webhook')),
    destination TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','sent','failed','dead_letter','suppressed')),
    attempt INTEGER NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 5 CHECK (max_attempts BETWEEN 1 AND 20),
    scheduled_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    next_attempt_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (notification_id, channel)
);

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_ready
    ON notification_deliveries (COALESCE(next_attempt_at, scheduled_at), id)
    WHERE status IN ('pending','failed');

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_dead_letter
    ON notification_deliveries (status, updated_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS notification_suppression_rules (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL DEFAULT '*',
    channel TEXT NOT NULL DEFAULT '*',
    reason TEXT NOT NULL DEFAULT '',
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notification_suppression_rules_active
    ON notification_suppression_rules (organization_id, active, event_type, channel);

CREATE TABLE IF NOT EXISTS notification_audit_events (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT REFERENCES organizations(id) ON DELETE CASCADE,
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    notification_id BIGINT REFERENCES notifications(id) ON DELETE SET NULL,
    delivery_id BIGINT REFERENCES notification_deliveries(id) ON DELETE SET NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notification_audit_recent
    ON notification_audit_events (organization_id, created_at DESC, id DESC);


INSERT INTO notification_templates (template_key,channel,locale,version,subject,body,active)
VALUES
('task.assigned','in_app','en',1,'Task assigned','You were assigned to {{task_title}}',TRUE),
('task.assigned','email','en',1,'Task assigned','You were assigned to {{task_title}}',TRUE),
('task.mentioned','in_app','en',1,'Mentioned in a comment','{{comment_body}}',TRUE),
('task.due_soon','in_app','en',1,'Task due soon','{{task_title}} is due at {{due_at}}',TRUE),
('task.overdue','in_app','en',1,'Task overdue','{{task_title}} was due at {{due_at}}',TRUE),
('workflow.approval.requested','in_app','en',1,'Workflow approval required','Execution {{execution_id}} is waiting for approval',TRUE),
('operations.incident.created','in_app','en',1,'Operational incident','{{title}} ({{severity}})',TRUE),
('billing.event','in_app','en',1,'Billing update','{{message}}',TRUE),
('task.assigned','in_app','id',1,'Tugas diberikan','Anda ditugaskan ke {{task_title}}',TRUE),
('task.mentioned','in_app','id',1,'Anda disebut di komentar','{{comment_body}}',TRUE),
('task.due_soon','in_app','id',1,'Tugas segera jatuh tempo','{{task_title}} jatuh tempo pada {{due_at}}',TRUE),
('task.overdue','in_app','id',1,'Tugas melewati jatuh tempo','{{task_title}} jatuh tempo pada {{due_at}}',TRUE)
ON CONFLICT (template_key,channel,locale,version) DO NOTHING;
