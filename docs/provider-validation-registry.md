# Provider Validation Registry

The provider validation registry turns individual `provider-validation-evidence.json` manifests into one operational matrix showing which provider contracts have actually been validated in each environment.

It does not manufacture missing evidence. A target/environment with no retained manifest is reported as `not_run`.

## Status model

Every selected target/environment cell is one of:

- `not_run` — no matching evidence manifest was found.
- `passed` — the newest matching manifest passed and is within the configured evidence age.
- `failed` — the newest matching manifest records a failed live contract or failure-injection layer.
- `expired` — the newest matching manifest passed but is older than the configured maximum age.

The default expiry is 30 days.

A passed cell can still contain gaps. For example, a provider contract may pass while the tested commit differs from the expected commit, producing `commit_drift`. Provider-specific limitations from the original evidence manifest are also preserved.

## Matrix coverage

The registry supports all live-provider targets:

- storage: `storage_s3`, `storage_azure`, `storage_gcs`
- content security: `scanner`
- secrets: `secret_vault`, `secret_aws`, `secret_azure`, `secret_gcp`
- warehouses: `warehouse_bigquery`, `warehouse_snowflake`, `warehouse_redshift`, `warehouse_databricks`
- AI: `ai_remote`
- multi-region planning: `region_automation`

The standard environment columns are `development`, `staging`, and `production`.

## Local build

Place one or more evidence artifacts under a directory. The builder searches recursively for files named `provider-validation-evidence.json`.

```bash
python3 scripts/provider-validation-registry.py build \
  --input-dir ./provider-evidence \
  --output-json provider-validation-registry.json \
  --output-markdown provider-validation-registry.md \
  --max-age-days 30 \
  --expected-commit <git-sha>
```

If multiple manifests exist for one target/environment pair, the newest `completed_at` wins. `workflow_run_attempt` is used as a tie-breaker.

The JSON output contains the full machine-readable state. The Markdown output is intended for release/change review and GitHub Actions job summaries.

## Verification

Basic structural verification:

```bash
python3 scripts/provider-validation-registry.py verify \
  --file provider-validation-registry.json \
  --expected-commit <git-sha>
```

Strict production gate:

```bash
python3 scripts/provider-validation-registry.py verify \
  --file provider-validation-registry.json \
  --expected-commit <git-sha> \
  --require-all-passed \
  --require-current-commit
```

`--require-all-passed` fails if any selected cell is `not_run`, `failed`, or `expired`.

`--require-current-commit` fails when a cell is `passed` but the evidence was produced from a different commit than the expected commit.

These flags are deliberately separate. A team may accept recently validated provider behavior from an earlier commit for a low-risk change, while a stricter release policy can require exact-commit coverage.

## GitHub Actions registry workflow

Run **Provider Validation Registry** manually.

The workflow can either:

1. scan recent non-expired repository artifacts named `provider-validation-...`; or
2. accept explicit Live Provider Contracts workflow run IDs.

When run IDs are omitted, the workflow scans up to the configured `max_artifacts` recent provider-validation artifacts. The default is 200.

Inputs include:

- evidence run IDs (optional)
- maximum artifacts to scan
- maximum evidence age
- environment selection: all, development, staging, or production
- expected commit
- require all passed
- require current commit

The workflow has only `contents: read` and `actions: read` permissions. It does not receive provider credentials and cannot execute provider contracts.

The output artifact contains:

```text
provider-validation-registry.json
provider-validation-registry.md
```

The Markdown matrix is also appended to the GitHub Actions job summary.

## Gaps and limitations

The `gaps` field is intentionally conservative. It contains limitations copied from the underlying evidence plus registry-derived conditions such as:

- `provider_validation_not_run`
- `validation_failed`
- `evidence_expired`
- `evidence_timestamp_in_future`
- `commit_drift`

Provider evidence limitations remain visible even when the status is `passed`, including provider-specific IAM review, organization-specific compliance review, real provider outage recovery, warehouse readback, AI pricing calibration, and regional game-day/RPO-RTO evidence.

Therefore, an all-green registry means the selected contract matrix is current and passed. It does not convert repository/provider contract validation into a blanket cloud compliance or disaster-recovery certification.
