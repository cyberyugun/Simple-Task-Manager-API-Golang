ALTER TABLE developer_applications
    ADD COLUMN IF NOT EXISTS review_round INTEGER NOT NULL DEFAULT 0 CHECK (review_round >= 0),
    ADD COLUMN IF NOT EXISTS submitted_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;

UPDATE developer_applications
SET submitted_by_user_id = created_by_user_id
WHERE submitted_at IS NOT NULL
  AND submitted_by_user_id IS NULL;

CREATE TABLE IF NOT EXISTS developer_app_reviews (
    id BIGSERIAL PRIMARY KEY,
    app_id BIGINT NOT NULL REFERENCES developer_applications(id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    review_round INTEGER NOT NULL CHECK (review_round > 0),
    decision TEXT NOT NULL CHECK (decision IN ('approve','reject','suspend')),
    from_status TEXT NOT NULL CHECK (from_status IN ('submitted','approved')),
    to_status TEXT NOT NULL CHECK (to_status IN ('approved','rejected','suspended')),
    note TEXT NOT NULL DEFAULT '',
    submitted_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    reviewed_by_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_developer_app_reviews_app
    ON developer_app_reviews (app_id, review_round DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_developer_app_reviews_workspace
    ON developer_app_reviews (workspace_id, created_at DESC, id DESC);
