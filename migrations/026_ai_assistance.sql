CREATE TABLE IF NOT EXISTS ai_policies (
    organization_id BIGINT PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    provider TEXT NOT NULL DEFAULT 'local_rules',
    monthly_budget_cents BIGINT NOT NULL DEFAULT 0 CHECK (monthly_budget_cents >= 0),
    redaction_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    max_input_chars INTEGER NOT NULL DEFAULT 12000 CHECK (max_input_chars BETWEEN 256 AND 200000),
    allowed_classifications JSONB NOT NULL DEFAULT '["public","internal"]'::jsonb
        CHECK (jsonb_typeof(allowed_classifications) = 'array'),
    external_max_classification TEXT NOT NULL DEFAULT 'internal'
        CHECK (external_max_classification IN ('public','internal','confidential','restricted')),
    require_human_approval_for_actions BOOLEAN NOT NULL DEFAULT TRUE,
    updated_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ai_requests (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    workspace_id BIGINT REFERENCES workspaces(id) ON DELETE SET NULL,
    feature TEXT NOT NULL CHECK (length(trim(feature)) > 0),
    status TEXT NOT NULL CHECK (status IN ('completed','pending_approval','approved','rejected','blocked','failed')),
    provider TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    classification TEXT NOT NULL CHECK (classification IN ('public','internal','confidential','restricted')),
    input_hash TEXT NOT NULL,
    redaction_count INTEGER NOT NULL DEFAULT 0 CHECK (redaction_count >= 0),
    prompt_metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(prompt_metadata) = 'object'),
    structured_result JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(structured_result) = 'object'),
    proposed_action JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(proposed_action) = 'object'),
    requires_approval BOOLEAN NOT NULL DEFAULT FALSE,
    destructive_action BOOLEAN NOT NULL DEFAULT FALSE,
    input_units BIGINT NOT NULL DEFAULT 0 CHECK (input_units >= 0),
    output_units BIGINT NOT NULL DEFAULT 0 CHECK (output_units >= 0),
    estimated_cost_cents BIGINT NOT NULL DEFAULT 0 CHECK (estimated_cost_cents >= 0),
    actual_cost_cents BIGINT NOT NULL DEFAULT 0 CHECK (actual_cost_cents >= 0),
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    decision_comment TEXT NOT NULL DEFAULT '',
    decided_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_ai_requests_org_created
    ON ai_requests (organization_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_ai_requests_budget
    ON ai_requests (organization_id, created_at)
    WHERE status IN ('completed','pending_approval','approved');

CREATE INDEX IF NOT EXISTS idx_ai_requests_pending_approval
    ON ai_requests (organization_id, id)
    WHERE status = 'pending_approval';

CREATE TABLE IF NOT EXISTS ai_evaluation_cases (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    feature TEXT NOT NULL CHECK (length(trim(feature)) > 0),
    input TEXT NOT NULL CHECK (length(trim(input)) > 0),
    classification TEXT NOT NULL CHECK (classification IN ('public','internal','confidential','restricted')),
    expected_keywords JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(expected_keywords) = 'array'),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_evaluation_cases_org
    ON ai_evaluation_cases (organization_id, id);

CREATE TABLE IF NOT EXISTS ai_evaluation_runs (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    case_id BIGINT NOT NULL REFERENCES ai_evaluation_cases(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    score_basis_points INTEGER NOT NULL CHECK (score_basis_points BETWEEN 0 AND 10000),
    input_units BIGINT NOT NULL DEFAULT 0 CHECK (input_units >= 0),
    output_units BIGINT NOT NULL DEFAULT 0 CHECK (output_units >= 0),
    actual_cost_cents BIGINT NOT NULL DEFAULT 0 CHECK (actual_cost_cents >= 0),
    passed BOOLEAN NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_evaluation_runs_org
    ON ai_evaluation_runs (organization_id, created_at DESC, id DESC);
