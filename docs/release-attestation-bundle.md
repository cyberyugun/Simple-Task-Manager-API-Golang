# Production Release Attestation Bundle

The production release workflow now emits one integrity-bound attestation bundle after the immutable image has passed staging promotion, provider policy evaluation, production canary promotion, and stable telemetry verification.

The bundle is evidence aggregation. It does not replace the underlying provider contract, operational review, cluster verification, or external audit record.

## Bundle contents

A successful production release uploads an artifact named:

```text
release-attestation-<git-sha>
```

The artifact is retained for 180 days and contains:

```text
release-attestation.json
release-attestation.md
release-evidence.json
staging-promotion-evidence.json
provider-validation-release-registry.json        # when provider gate is active
provider-validation-release-policy.json          # when provider gate is active
provider-operational-release-expiry.json          # when operational evidence is required
```

`release-attestation.json` is the machine-readable root manifest. `release-attestation.md` is a human-readable release/governance dashboard derived from the same state.

## Integrity chain

Each bound component is hashed with SHA-256.

The root manifest records:

- production commit;
- immutable container image digest;
- workflow run and attempt;
- source ref;
- provider gate mode;
- operational-evidence and approval requirements;
- component file names, component status, and SHA-256 values;
- provider decisions including operational/approval/revocation state;
- operational-evidence expiry snapshot;
- explicit warning reasons and limitations.

The manifest then receives a canonical SHA-256 `root_digest` over the complete manifest excluding the `root_digest` field itself.

Verification therefore checks both levels:

```text
component bytes
    |
    +--> SHA-256 recorded in release-attestation.json
    |
    v
canonical release-attestation.json
    |
    +--> root_digest
```

Changing a bound component after bundle creation makes verification fail even when the JSON remains syntactically valid.

## GitHub artifact provenance

After the release manifest has passed local component/root verification, the production workflow also submits `release-attestation.json` to GitHub artifact attestation using the repository's OIDC-backed release workflow identity.

This creates an external provenance record for the root manifest itself:

```text
release components
      |
      v
release-attestation.json + root_digest
      |
      v
GitHub artifact attestation / Sigstore-backed provenance
```

The GitHub provenance layer does not replace the local SHA-256 chain. The local chain proves which retained component bytes are bound by the manifest; GitHub artifact attestation separately proves that the manifest subject was attested by the repository workflow identity.


## Live provider evidence binding

Provider-validation registry cells now record `evidence_sha256`, the SHA-256 of the selected `provider-validation-evidence.json` manifest.

The release bundle binds the complete production provider registry. Therefore the release root digest indirectly binds the exact live-provider evidence manifest selected for each provider target, while the provider evidence manifest already binds its live-contract and failure-injection logs.

This chain is:

```text
live-provider log + failure-injection log
            |
            v
provider-validation-evidence.json
            |
            +--> evidence_sha256
            v
provider-validation-release-registry.json
            |
            v
release-attestation root_digest
```

The raw provider logs remain in their original validation artifacts. They are not duplicated into every release bundle.

## Operational evidence state

When `PROVIDER_OPERATIONAL_EVIDENCE_REQUIRED=true`, the deploy workflow also creates a release-time operational expiry snapshot.

The snapshot records the selected target state at release time, including:

- passed/expiring/expired/failed/revoked/unapproved/not-run status;
- days remaining;
- evidence digest;
- submitter;
- approver when applicable.

When `PROVIDER_OPERATIONAL_EVIDENCE_REQUIRE_APPROVAL=true`, the snapshot must itself be generated with approval enforcement enabled.

The provider policy component remains the authoritative release gate. The expiry snapshot exists to preserve the operational-governance state visible when the release was attested.

## Release status

The attestation status is:

- `passed` when the production release succeeded and no attestation warning was recorded;
- `passed_with_warnings` when deployment was allowed but a warn-mode provider policy failed or the operational expiry snapshot requires attention.

An `enforce` provider policy must be `passed` or bundle creation fails.

A failed production deployment does not produce a successful release attestation bundle. The existing `release-evidence.json` remains available separately for failure/rollback investigation when generated.

## Local verification

Given an extracted artifact:

```bash
python3 scripts/release-attestation-bundle.py verify \
  --file release-attestation.json \
  --component-dir . \
  --commit <git-sha> \
  --image-digest sha256:<digest> \
  --run-id <github-actions-run-id>
```

Use `--require-passed` when `passed_with_warnings` should be treated as a verification failure.

## Release Attestation Dashboard

Run **Release Attestation Dashboard** to inspect retained release bundles without provider or cluster credentials.

The workflow uses only read permissions:

```text
contents: read
actions: read
attestations: read
```

It discovers recent non-expired `release-attestation-...` artifacts, downloads each bundle, recomputes component hashes and root digests, and publishes:

```text
release-attestation-dashboard.json
release-attestation-dashboard.md
```

The dashboard shows:

- local bundle integrity;
- GitHub artifact provenance verification state;
- release status;
- provider-policy status;
- source commit;
- workflow run;
- root digest;
- successful production deploy coverage.

For every downloaded manifest the workflow runs `gh attestation verify <manifest> --repo <repository>` and records a release-local verification snapshot as `release-attestation-provenance.json` in the dashboard workspace.

The dashboard also queries successful runs of `.github/workflows/deploy.yml` using read-only Actions access and writes:

```text
expected-successful-deploy-runs.json
```

Every successful deploy run in scope must map to exactly one retained release attestation with the same workflow run ID and source commit. Coverage statuses are:

- `covered` — exactly one matching attestation exists and its commit matches the successful deploy;
- `missing_attestation` — a successful production deploy has no retained attestation;
- `commit_mismatch` — a release attestation references the deploy run but does not bind the same head SHA;
- `duplicate_attestation` — more than one retained attestation claims the same deploy run.

This closes an omission gap that per-artifact integrity/provenance checks cannot detect: a collection can contain only valid signed manifests while still silently missing a successful production release.

The coverage query is limited by `lookback_days`, `max_deploy_runs`, and optional `coverage_start_at`. Use `coverage_start_at` during rollout to exclude successful releases from before attestation coverage was introduced.

Set `require_valid=true` to fail on invalid local bundle integrity. Set `require_provenance=true` to fail when a release manifest has no valid GitHub artifact provenance. Set `require_complete=true` to fail when a successful production deploy in the configured coverage scope is missing, mismatched, or duplicated.

The workflow runs daily as well as by manual dispatch. Scheduled defaults can be configured with repository variables:

```text
RELEASE_ATTESTATION_DASHBOARD_MAX_ARTIFACTS=100
RELEASE_ATTESTATION_DASHBOARD_MAX_DEPLOY_RUNS=100
RELEASE_ATTESTATION_COVERAGE_LOOKBACK_DAYS=180
RELEASE_ATTESTATION_COVERAGE_START_AT=
RELEASE_ATTESTATION_REQUIRE_VALID=false
RELEASE_ATTESTATION_REQUIRE_PROVENANCE=false
RELEASE_ATTESTATION_REQUIRE_COMPLETE=false
```

A safe rollout is to inspect coverage first, set `RELEASE_ATTESTATION_COVERAGE_START_AT` to the first production release that was expected to emit a bundle, then enable strict completeness/provenance checks after the historical scope is clean.

## Limitations

The release root digest proves integrity of the retained repository artifacts included in the bundle. GitHub artifact provenance additionally proves the root manifest has a valid attestation associated with the repository workflow identity when verification succeeds. Neither layer independently proves that an external HTTPS operational-evidence reference still serves the same object, that cloud IAM is least-privilege, that an outage/game-day actually occurred, or that a provider billing portal is correct.

Those claims remain external operational evidence and are governed by the separate submit/approve/revoke process.
