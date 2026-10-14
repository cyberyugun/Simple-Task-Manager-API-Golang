ALTER TABLE report_runs
    ADD COLUMN IF NOT EXISTS payload_size_bytes BIGINT NOT NULL DEFAULT 0 CHECK (payload_size_bytes >= 0),
    ADD COLUMN IF NOT EXISTS payload_purged_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS aggregated_at TIMESTAMPTZ;

UPDATE report_runs
SET payload_size_bytes = octet_length(payload)
WHERE payload IS NOT NULL AND payload_size_bytes = 0;

CREATE TABLE IF NOT EXISTS report_run_daily_aggregates (
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    format TEXT NOT NULL CHECK (format IN ('json', 'csv')),
    status TEXT NOT NULL CHECK (status IN ('succeeded', 'failed')),
    run_count BIGINT NOT NULL DEFAULT 0 CHECK (run_count >= 0),
    total_row_count BIGINT NOT NULL DEFAULT 0 CHECK (total_row_count >= 0),
    error_count BIGINT NOT NULL DEFAULT 0 CHECK (error_count >= 0),
    payload_size_bytes BIGINT NOT NULL DEFAULT 0 CHECK (payload_size_bytes >= 0),
    duration_millis BIGINT NOT NULL DEFAULT 0 CHECK (duration_millis >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, day, format, status)
);

CREATE INDEX IF NOT EXISTS idx_report_runs_payload_retention
    ON report_runs (created_at, id)
    WHERE payload IS NOT NULL AND status IN ('succeeded', 'failed');

CREATE INDEX IF NOT EXISTS idx_report_runs_detail_retention
    ON report_runs (created_at, id)
    WHERE aggregated_at IS NULL AND status IN ('succeeded', 'failed');

CREATE INDEX IF NOT EXISTS idx_report_run_daily_aggregates_workspace_day
    ON report_run_daily_aggregates (workspace_id, day DESC, format, status);
