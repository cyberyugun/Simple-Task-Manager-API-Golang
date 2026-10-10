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

The workflow has only:

```text
contents: read
actions: read
```

It discovers recent non-expired `release-attestation-...` artifacts, downloads each bundle, recomputes component hashes and root digests, and publishes:

```text
release-attestation-dashboard.json
release-attestation-dashboard.md
```

The dashboard shows:

- bundle integrity;
- release status;
- provider-policy status;
- source commit;
- workflow run;
- root digest.

Set `require_valid=true` to make the dashboard workflow fail when any downloaded bundle has invalid integrity.

## Limitations

The release root digest proves integrity of the retained repository artifacts included in the bundle. It does not independently prove that an external HTTPS operational-evidence reference still serves the same object, that cloud IAM is least-privilege, that an outage/game-day actually occurred, or that a provider billing portal is correct.

Those claims remain external operational evidence and are governed by the separate submit/approve/revoke process.
