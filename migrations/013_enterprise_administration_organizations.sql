CREATE TABLE IF NOT EXISTS organizations (
    id BIGSERIAL PRIMARY KEY,
    parent_id BIGINT REFERENCES organizations(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active','suspended','deactivated')),
    owner_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    max_workspaces INTEGER NOT NULL DEFAULT 20 CHECK (max_workspaces BETWEEN 1 AND 10000),
    max_members INTEGER NOT NULL DEFAULT 100 CHECK (max_members BETWEEN 1 AND 100000),
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deactivated_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_organizations_parent ON organizations (parent_id, id);
CREATE INDEX IF NOT EXISTS idx_organizations_owner ON organizations (owner_user_id, status);

CREATE TABLE IF NOT EXISTS organization_members (
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('owner','admin','delegated_admin','member')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (organization_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_organization_members_user
    ON organization_members (user_id, organization_id);

CREATE TABLE IF NOT EXISTS organization_workspaces (
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL UNIQUE REFERENCES workspaces(id) ON DELETE CASCADE,
    attached_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    attached_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (organization_id, workspace_id)
);

CREATE TABLE IF NOT EXISTS organization_invitations (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('admin','delegated_admin','member')),
    token_hash CHAR(64) NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','accepted','revoked','expired')),
    invited_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    accepted_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_organization_invitations_lookup
    ON organization_invitations (organization_id, status, expires_at);

CREATE TABLE IF NOT EXISTS organization_teams (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, name)
);

CREATE TABLE IF NOT EXISTS organization_team_members (
    team_id BIGINT NOT NULL REFERENCES organization_teams(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (team_id, user_id)
);

CREATE TABLE IF NOT EXISTS organization_domains (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    domain TEXT NOT NULL,
    verification_hash CHAR(64) NOT NULL,
    created_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    verified_at TIMESTAMPTZ,
    verified_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    UNIQUE (organization_id, domain)
);

CREATE INDEX IF NOT EXISTS idx_organization_domains_verified
    ON organization_domains (organization_id, verified_at);

CREATE TABLE IF NOT EXISTS organization_audit_events (
    id BIGSERIAL PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    actor_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_organization_audit_events_recent
    ON organization_audit_events (organization_id, created_at DESC, id DESC);
