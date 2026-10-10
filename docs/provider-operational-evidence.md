# External Provider Operational Evidence

Live provider contracts prove repository/provider integration behavior. They do not independently prove cloud-account IAM policy, real outage recovery, downstream warehouse readback, AI billing accuracy, or regional disaster recovery.

The provider operational-evidence layer records those external exercises as operator-attested artifacts and can add dual-control approval, integrity binding, revocation, supersede metadata, and expiry monitoring.

## Evidence model

The **Provider Operational Evidence** workflow creates:

```text
provider-operational-evidence.json
```

The artifact is retained for 180 days.

The manifest records:

- target and environment;
- reviewer identity supplied with the external review;
- authenticated GitHub submitter in `submitted_by`;
- completion and expiry timestamps;
- optional related source commit;
- workflow run/attempt;
- target-specific required checks;
- HTTPS references to external evidence;
- optional or required SHA-256 reference digests;
- target-specific measurements;
- a canonical SHA-256 `evidence_digest`;
- explicit limitations.

The repository validates the shape and measurements, but it does not fetch or independently certify the contents behind supplied evidence URLs. The manifest therefore retains the limitation `operator_attested_external_evidence`.

## Required checks by target

| Target group | Required checks |
| --- | --- |
| S3 / Azure Blob / GCS | `iam_least_privilege_review`, `real_outage_recovery_exercise`, `data_policy_review` |
| Scanner | `gateway_access_policy_review`, `real_outage_fail_closed_exercise` |
| Vault / AWS / Azure / GCP secrets | `iam_least_privilege_review`, `real_outage_recovery_exercise`, `key_policy_review` |
| BigQuery / Snowflake / Redshift / Databricks | `iam_least_privilege_review`, `real_outage_recovery_exercise`, `downstream_readback_verification` |
| Remote AI | `gateway_access_policy_review`, `real_outage_recovery_exercise`, `pricing_calibration` |
| Region automation | `planning_gateway_iam_review`, `external_apply_gate_review`, `regional_game_day`, `rpo_rto_measurement` |

Every required check needs a status and HTTPS evidence reference. Production submissions also require a lowercase SHA-256 digest of the referenced/exported evidence:

```json
{
  "status": "passed",
  "reference": "https://evidence.example.com/change-or-report",
  "reference_sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "notes": "optional short context"
}
```

A required check marked `failed`, missing, lacking its HTTPS reference, or missing a production reference digest causes the manifest to fail verification.

The digest binds the manifest to the operator-supplied external evidence representation. The repository does not download the URL to prove that the remote object still has that digest.

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

## Submission workflow

Run **Provider Operational Evidence** and provide:

- target;
- environment;
- reviewer;
- validity period;
- optional related commit;
- checks JSON;
- measurements JSON.

The workflow records `github.actor` as `submitted_by`, creates the canonical evidence digest, and verifies the artifact before upload.

For production, every check must include `reference_sha256`.

No provider credentials are used by this workflow.

## Dual-control approval

Run **Provider Operational Evidence Governance** with action `approve` and the source submission workflow run ID.

The governance workflow:

1. downloads the immutable submission artifact;
2. re-verifies evidence integrity;
3. records the current authenticated `github.actor` as `approved_by`;
4. rejects self-approval when `approved_by == submitted_by`;
5. requires the production approver to exist in `PROVIDER_OPERATIONAL_EVIDENCE_ALLOWED_APPROVERS`;
6. creates `provider-operational-approval.json`;
7. uploads the original evidence and approval together as an approved evidence bundle.

Configure the allowlist as a comma- or space-separated repository/environment variable, for example:

```text
PROVIDER_OPERATIONAL_EVIDENCE_ALLOWED_APPROVERS=platform-lead,security-lead,sre-lead
```

The approval manifest has its own canonical `approval_digest`.

An approval may also include `supersedes_digest` to document that the newly approved evidence replaces an older evidence digest. The release policy always evaluates the newest evidence by completion time; the supersede relation is retained for audit history.

## Revocation

Run **Provider Operational Evidence Governance** with action `revoke` and the workflow run ID that produced the approved evidence bundle.

The revocation path re-verifies the evidence and approval, requires an authorized actor for production, records a mandatory reason, and creates:

```text
provider-operational-revocation.json
```

The revocation manifest has a canonical `revocation_digest` and points to both the evidence and approval digests.

A valid revocation always invalidates the matching operational evidence in the provider policy, even when approval enforcement itself is disabled.

To restore readiness after revocation, submit and approve new evidence rather than deleting the revocation history.

## Local creation and verification

Example production warehouse evidence:

```bash
python3 scripts/provider-operational-evidence.py create \
  --output provider-operational-evidence.json \
  --target warehouse_bigquery \
  --environment production \
  --reviewer "platform-reviewer" \
  --submitted-by "evidence-submitter" \
  --checks-json '{"iam_least_privilege_review":{"status":"passed","reference":"https://example.com/iam","reference_sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},"real_outage_recovery_exercise":{"status":"passed","reference":"https://example.com/outage","reference_sha256":"1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},"downstream_readback_verification":{"status":"passed","reference":"https://example.com/readback","reference_sha256":"2123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}' \
  --measurements-json '{"readback_expected_rows":10,"readback_actual_rows":10,"readback_checksum_match":true}' \
  --valid-days 90 \
  --require-reference-digests
```

Verify:

```bash
python3 scripts/provider-operational-evidence.py verify \
  --file provider-operational-evidence.json \
  --target warehouse_bigquery \
  --environment production \
  --require-passed \
  --require-integrity \
  --require-submitter \
  --require-reference-digests
```

Approve locally:

```bash
python3 scripts/provider-operational-approval.py approve \
  --evidence provider-operational-evidence.json \
  --output provider-operational-approval.json \
  --approved-by security-lead \
  --allowed-approvers security-lead,sre-lead \
  --require-allowlist \
  --target warehouse_bigquery \
  --environment production
```

## Production policy integration

The production provider policy can require operational evidence in addition to live provider validation.

Set on the `production` GitHub Environment:

```text
PROVIDER_OPERATIONAL_EVIDENCE_REQUIRED=true
PROVIDER_OPERATIONAL_EVIDENCE_REQUIRE_APPROVAL=true
```

When the first variable is enabled, every target listed in `PROVIDER_VALIDATION_REQUIRED_TARGETS` must have non-expired passed operational evidence for `production`.

When approval enforcement is also enabled, the selected evidence must have a valid approval manifest from a different submitter/approver identity. Missing approval becomes `operational_evidence_unapproved`.

Valid revocation becomes `operational_evidence_revoked` and blocks the required provider.

Operational evidence is intentionally not required to match every source commit. IAM reviews, game days, outage exercises, and pricing calibrations have their own validity windows and may remain valid across multiple releases.

The **Provider Validation Registry** workflow can preview the same rules with:

```text
policy_require_operational_evidence=true
policy_require_operational_approval=true
```

## Expiry monitoring

The **Provider Operational Evidence Expiry** workflow runs daily and can also be dispatched manually.

It reports the newest production evidence for configured targets as:

- `passed`
- `expiring`
- `expired`
- `failed`
- `revoked`
- `unapproved`
- `not_run`

Scheduled defaults are controlled by repository variables:

```text
PROVIDER_OPERATIONAL_EVIDENCE_EXPIRY_WARNING_DAYS=14
PROVIDER_OPERATIONAL_EVIDENCE_EXPIRY_FAIL=false
PROVIDER_OPERATIONAL_EVIDENCE_MAX_ARTIFACTS=300
```

The workflow emits GitHub Actions warnings for evidence nearing expiry or already invalid and publishes JSON/Markdown expiry reports. Setting `PROVIDER_OPERATIONAL_EVIDENCE_EXPIRY_FAIL=true` makes invalid required-target evidence fail the scheduled workflow.

The required target list is reused from `PROVIDER_VALIDATION_REQUIRED_TARGETS`.

## What this still does not prove

A passed and approved operational manifest proves that two distinct GitHub identities participated in submission/approval, required repository checks passed, the manifest integrity digests are consistent, and an auditable external reference was supplied.

It still does not independently inspect a cloud account, ticketing system, billing portal, query console, disaster-recovery environment, or the content behind the external HTTPS reference.

Production governance must preserve the referenced external records according to organization policy and restrict repository/environment-variable administration so the approver allowlist itself remains trustworthy.
