# Provider Validation Evidence and Failure Injection

This repository records every manual live-provider validation run as a machine-readable evidence manifest. The evidence is designed to distinguish what the repository actually tested from claims that still require provider-specific operational exercises.

## Execution model

The **Live Provider Contracts** workflow now runs two independent layers for the selected provider target:

1. **Hermetic failure injection** — repository-local tests that exercise known retry, fail-closed, unsafe-configuration, malformed-response, checkpoint-safety, or approval-safety behavior without contacting the external provider.
2. **Live provider contract** — the existing opt-in test against the configured external provider or gateway using synthetic contract data.

Both steps run long enough to preserve their logs even when one fails. The workflow then creates and verifies a single evidence manifest. The job fails unless both layers succeeded.

This means a failed provider contract still leaves an artifact explaining which layer failed, while a successful workflow proves both the selected live contract and its curated repository failure behavior passed for the same source commit.

## Evidence schema

scripts/provider-validation-evidence.py emits provider-validation-evidence.json with schema version 1.

Key fields:

~~~json
{
  "schema_version": 1,
  "status": "passed",
  "target": "warehouse_bigquery",
  "environment": "staging",
  "commit": "<git-sha>",
  "workflow_run_id": "<run-id>",
  "workflow_run_attempt": "<attempt>",
  "source_ref": "<git-ref>",
  "completed_at": "<UTC timestamp>",
  "live_contract": {
    "status": "success",
    "log_sha256": "<sha256>",
    "claims": []
  },
  "failure_injection": {
    "status": "success",
    "log_sha256": "<sha256>",
    "checks": [],
    "scope": "hermetic_repository_tests"
  },
  "limitations": []
}
~~~

The manifest stores SHA-256 hashes of both logs rather than their contents. The workflow artifact retains the logs beside the manifest so an operator can later verify that the evidence corresponds to the exact captured output.

status=passed is only valid when both live_contract.status and failure_injection.status are success.

## Environment labels

The GitHub workflow records one of:

- development
- staging
- production

The label is evidence metadata only. Selecting production does not grant production access and does not change provider permissions. Access still depends entirely on the credentials, IAM policy, network path, protected GitHub Environment, and external provider configuration supplied by the operator.

For local execution, the evidence tool also accepts a short custom environment label such as ci or sandbox.

## Hermetic failure-injection coverage

The selected target determines the curated negative-path tests.

Storage contracts cover metadata mismatch and unsafe endpoint/configuration rejection where supported. Scanner validation covers insecure endpoint rejection, infected/error fail-closed behavior, and redirect refusal. Secret backends cover unsafe or incomplete configuration and, for Vault, unconfigured external rotation rejection.

Warehouse targets cover transient retry behavior, checkpoint non-advancement after failed delivery, and unsafe provider configuration. Remote AI covers stable idempotency across retries, malformed structured-output rejection, and unsafe configuration. Region automation covers stable signature/idempotency across retries, unsafe or unapproved migration rejection, and keeping a migration pending when planning fails.

These are hermetic tests. They prove repository behavior under controlled injected failures; they do not simulate an actual cloud/provider outage.

## Artifact contents

Each manual run retains for 30 days:

~~~text
live-provider-contract.log
provider-failure-injection.log
provider-validation-evidence.json
~~~

The artifact name includes environment, target, and GitHub run ID.

## Verification

Evidence can be verified independently:

~~~bash
python3 scripts/provider-validation-evidence.py verify \
  --file provider-validation-evidence.json \
  --target warehouse_bigquery \
  --environment staging \
  --commit <git-sha> \
  --run-id <run-id> \
  --run-attempt <attempt> \
  --require-passed
~~~

Verification checks the schema, target, environment, commit/run identity, step statuses, required target claims/checks, SHA-256 fields, overall status consistency, and that explicit limitations are present.

## Interpretation and remaining production evidence

A passed manifest is evidence for the selected live adapter/gateway contract plus the curated repository failure paths at one commit and environment label. It is not a blanket production certification.

Depending on target, additional evidence still includes:

- provider-specific least-privilege IAM review;
- real provider outage and recovery exercises;
- retention, replication, cross-account, or compliance-policy review;
- warehouse downstream query/readback verification;
- external AI pricing calibration and live outage exercises;
- multi-region external apply-gate review, real failover game days, and measured RPO/RTO.

Those external exercises should reference the provider-validation manifest/run ID rather than replacing it, so the final production-readiness record preserves both repository contract evidence and environment-specific operational evidence.

## Registry aggregation

Individual evidence manifests can be aggregated with `scripts/provider-validation-registry.py` or the manual **Provider Validation Registry** workflow. The registry selects the newest evidence per target/environment, reports `not_run`, `passed`, `failed`, or `expired`, preserves manifest limitations, records evidence age and tested commit, and flags `commit_drift` when the evidence commit differs from the expected release commit.

See [Provider Validation Registry](provider-validation-registry.md).
