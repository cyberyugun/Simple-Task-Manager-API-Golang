CREATE TABLE IF NOT EXISTS billing_plans (
    id BIGSERIAL PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    currency TEXT NOT NULL DEFAULT 'USD',
    monthly_price_cents BIGINT NOT NULL DEFAULT 0 CHECK (monthly_price_cents >= 0),
    max_members INTEGER NOT NULL CHECK (max_members > 0),
    max_workspaces INTEGER NOT NULL CHECK (max_workspaces > 0),
    monthly_api_operations BIGINT NOT NULL CHECK (monthly_api_operations > 0),
    features JSONB NOT NULL DEFAULT '{}'::jsonb,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO billing_plans (
    code, name, currency, monthly_price_cents, max_members, max_workspaces,
    monthly_api_operations, features
) VALUES
(
    'free', 'Free', 'USD', 0, 100, 20, 10000,
    '{"advanced_governance":false,"sso":false,"audit_export":false,"priority_support":false}'::jsonb
),
(
    'pro', 'Pro', 'USD', 4900, 1000, 100, 1000000,
    '{"advanced_governance":true,"sso":true,"audit_export":true,"priority_support":false}'::jsonb
),
(
    'enterprise', 'Enterprise', 'USD', 24900, 100000, 10000, 1000000000,
    '{"advanced_governance":true,"sso":true,"audit_export":true,"priority_support":true}'::jsonb
)
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS billing_subscriptions (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL UNIQUE REFERENCES organizations(id) ON DELETE CASCADE,
    plan_id BIGINT NOT NULL REFERENCES billing_plans(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('active','trialing','past_due','grace','canceled')),
    provider TEXT NOT NULL DEFAULT 'internal',
    provider_customer_id TEXT NOT NULL DEFAULT '',
    provider_subscription_id TEXT NOT NULL DEFAULT '',
    current_period_start TIMESTAMPTZ NOT NULL,
    current_period_end TIMESTAMPTZ NOT NULL,
    grace_until TIMESTAMPTZ,
    cancel_at_period_end BOOLEAN NOT NULL DEFAULT FALSE,
    canceled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_billing_subscriptions_provider
    ON billing_subscriptions (provider, provider_subscription_id)
    WHERE provider_subscription_id <> '';

CREATE TABLE IF NOT EXISTS billing_usage (
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    metric TEXT NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    quantity BIGINT NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (organization_id, metric, period_start, period_end)
);

CREATE INDEX IF NOT EXISTS idx_billing_usage_period
    ON billing_usage (organization_id, period_start DESC, metric);

CREATE TABLE IF NOT EXISTS billing_invoices (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    subscription_id BIGINT REFERENCES billing_subscriptions(id) ON DELETE SET NULL,
    provider TEXT NOT NULL DEFAULT 'internal',
    external_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft','open','paid','void','uncollectible')),
    currency TEXT NOT NULL,
    amount_due_cents BIGINT NOT NULL DEFAULT 0 CHECK (amount_due_cents >= 0),
    amount_paid_cents BIGINT NOT NULL DEFAULT 0 CHECK (amount_paid_cents >= 0),
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    due_at TIMESTAMPTZ,
    paid_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, external_id)
);

CREATE INDEX IF NOT EXISTS idx_billing_invoices_organization
    ON billing_invoices (organization_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS billing_webhook_events (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, event_id)
);

CREATE INDEX IF NOT EXISTS idx_billing_webhook_events_created
    ON billing_webhook_events (created_at DESC, id DESC);
