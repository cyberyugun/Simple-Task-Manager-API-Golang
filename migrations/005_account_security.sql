ALTER TABLE users
    ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ;

ALTER TABLE refresh_tokens
    ADD COLUMN IF NOT EXISTS user_agent TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS ip_address TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ;

UPDATE refresh_tokens
SET last_used_at = created_at
WHERE last_used_at IS NULL;

ALTER TABLE refresh_tokens
    ALTER COLUMN last_used_at SET NOT NULL,
    ALTER COLUMN last_used_at SET DEFAULT NOW();

CREATE TABLE IF NOT EXISTS auth_action_tokens (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash CHAR(64) NOT NULL UNIQUE,
    purpose VARCHAR(32) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT auth_action_tokens_purpose_check
        CHECK (purpose IN ('password_reset', 'email_verification'))
);

CREATE INDEX IF NOT EXISTS idx_auth_action_tokens_user_purpose
    ON auth_action_tokens (user_id, purpose, expires_at DESC);

CREATE INDEX IF NOT EXISTS idx_auth_action_tokens_active
    ON auth_action_tokens (purpose, expires_at)
    WHERE consumed_at IS NULL;
