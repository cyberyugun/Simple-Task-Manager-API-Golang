# External Provider Operational Evidence

Live provider contracts prove repository/provider integration behavior. They do not independently prove cloud-account IAM policy, real outage recovery, downstream warehouse readback, AI billing accuracy, or regional disaster recovery.

The **Provider Operational Evidence** workflow records those external exercises as a separate, operator-attested artifact.

## Evidence model

Each run creates:

```text
provider-operational-evidence.json
```

The artifact is retained for 180 days.

The manifest records:

- target and environment;
- reviewer identity;
- completion and expiry timestamps;
- optional related source commit;
- workflow run/attempt;
- target-specific required checks;
- HTTPS references to external evidence;
- target-specific measurements;
- explicit limitations.

The repository validates the shape and measurements, but it does not fetch or independently certify the contents behind the supplied evidence URLs. The manifest therefore includes the limitation `operator_attested_external_evidence`.

## Required checks by target

| Target group | Required checks |
| --- | --- |
| S3 / Azure Blob / GCS | `iam_least_privilege_review`, `real_outage_recovery_exercise`, `data_policy_review` |
| Scanner | `gateway_access_policy_review`, `real_outage_fail_closed_exercise` |
| Vault / AWS / Azure / GCP secrets | `iam_least_privilege_review`, `real_outage_recovery_exercise`, `key_policy_review` |
| BigQuery / Snowflake / Redshift / Databricks | `iam_least_privilege_review`, `real_outage_recovery_exercise`, `downstream_readback_verification` |
| Remote AI | `gateway_access_policy_review`, `real_outage_recovery_exercise`, `pricing_calibration` |
| Region automation | `planning_gateway_iam_review`, `external_apply_gate_review`, `regional_game_day`, `rpo_rto_measurement` |

Every required check needs:

```json
{
  "status": "passed",
  "reference": "https://evidence.example.com/change-or-report",
  "notes": "optional short context"
}
```

A required check marked `failed`, missing, or without an HTTPS reference causes the manifest to be `failed`.

## Measurement rules

Warehouse readback evidence must include:

```json
{
  "readback_expected_rows": 10,
  "readback_actual_rows": 10,
  "readback_checksum_match": true
}
```

The expected and actual row counts must match and the checksum flag must be true.

AI pricing calibration must include:

```json
{
  "pricing_estimated_cost_cents": 25,
  "pricing_observed_cost_cents": 24,
  "pricing_tolerance_percent": 10
}
```

The observed-vs-estimated percentage variance must be within the declared tolerance.

Region automation RPO/RTO evidence must include:

```json
{
  "target_rpo_seconds": 300,
  "achieved_rpo_seconds": 120,
  "target_rto_seconds": 1800,
  "achieved_rto_seconds": 900
}
```

Achieved RPO/RTO must be less than or equal to their targets.

## Manual workflow

Run **Provider Operational Evidence** and provide:

- target;
- environment;
- reviewer;
- validity period;
- optional related commit;
- checks JSON;
- measurements JSON.

Example AI checks:

```json
{
  "gateway_access_policy_review": {
    "status": "passed",
    "reference": "https://change.example.com/iam-review"
  },
  "real_outage_recovery_exercise": {
    "status": "passed",
    "reference": "https://runbook.example.com/outage-2026-10"
  },
  "pricing_calibration": {
    "status": "passed",
    "reference": "https://billing.example.com/calibration-2026-10"
  }
}
```

No provider credentials are used by this workflow.

## Local creation and verification

```bash
python3 scripts/provider-operational-evidence.py create \
  --output provider-operational-evidence.json \
  --target warehouse_bigquery \
  --environment production \
  --reviewer "platform-reviewer" \
  --checks-json '{"iam_least_privilege_review":{"status":"passed","reference":"https://example.com/iam"},"real_outage_recovery_exercise":{"status":"passed","reference":"https://example.com/outage"},"downstream_readback_verification":{"status":"passed","reference":"https://example.com/readback"}}' \
  --measurements-json '{"readback_expected_rows":10,"readback_actual_rows":10,"readback_checksum_match":true}' \
  --valid-days 90
```

Verify:

```bash
python3 scripts/provider-operational-evidence.py verify \
  --file provider-operational-evidence.json \
  --target warehouse_bigquery \
  --environment production \
  --require-passed
```

## Production policy integration

The production provider policy can require operational evidence in addition to live provider validation.

Set on the `production` GitHub Environment:

```text
PROVIDER_OPERATIONAL_EVIDENCE_REQUIRED=true
```

When enabled, every target listed in `PROVIDER_VALIDATION_REQUIRED_TARGETS` must have a non-expired, passed operational evidence artifact for `production`.

A missing artifact adds `operational_evidence_not_run`; a failed artifact adds `operational_evidence_failed`; an expired artifact adds `operational_evidence_expired`.

Operational evidence is intentionally not required to match every source commit. IAM reviews, game days, outage exercises and pricing calibrations have their own validity windows and may remain valid across multiple releases.

The **Provider Validation Registry** workflow can preview the same rule using `policy_require_operational_evidence=true`.

## What this still does not prove

A passed operational manifest proves that an authorized reviewer attested the required exercise and supplied an auditable reference plus required measurements. It does not independently inspect a cloud account, ticketing system, billing portal, query console or disaster-recovery environment.

Production governance should therefore preserve the referenced external records according to organization policy and ensure reviewer authorization is enforced outside this repository.
