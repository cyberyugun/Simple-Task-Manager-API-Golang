# Provider Validation Policy and Production Release Gate

The provider validation policy converts the evidence registry into an environment-specific release decision.

The registry answers **what has been tested**. The policy answers **which tested providers are mandatory for this environment**.

This distinction allows deployments that use only a subset of the supported integrations. An unused provider does not block a release unless it is explicitly listed as required.

## Policy inputs

Production policy is configured with GitHub Environment variables on the `production` environment:

```text
PROVIDER_VALIDATION_GATE_MODE=off|warn|enforce
PROVIDER_VALIDATION_REQUIRED_TARGETS=storage_s3,secret_aws,warehouse_bigquery,ai_remote
PROVIDER_VALIDATION_MAX_AGE_DAYS=30
PROVIDER_VALIDATION_MAX_ARTIFACTS=200
PROVIDER_VALIDATION_REQUIRE_CURRENT_COMMIT=false
PROVIDER_OPERATIONAL_EVIDENCE_REQUIRED=false
PROVIDER_OPERATIONAL_EVIDENCE_REQUIRE_APPROVAL=false
```

`PROVIDER_VALIDATION_GATE_MODE` defaults to `off` for backward compatibility.

When the gate is `warn` or `enforce`, `PROVIDER_VALIDATION_REQUIRED_TARGETS` must contain at least one supported provider target.

Only listed targets are policy-critical. For example, if production uses S3, AWS Secrets Manager and BigQuery but does not use external AI, configure:

```text
PROVIDER_VALIDATION_REQUIRED_TARGETS=storage_s3,secret_aws,warehouse_bigquery
```

The unused `ai_remote` cell may remain `not_run` without blocking that release.

## Gate modes

### off

The deploy workflow does not read provider-validation artifacts and does not evaluate provider policy.

Use this only while the environment is being bootstrapped or when no external provider integration is enabled.

### warn

The deploy workflow builds the production registry and evaluates the required targets, but a failed policy does not stop deployment.

The generated policy evidence still records `status=failed`, which makes the exception visible in the release artifact and workflow summary.

This mode is useful when initially rolling out the gate.

### enforce

Every required target must have current, non-expired `passed` evidence according to the configured maximum age.

A required target with `not_run`, `failed`, or `expired` evidence blocks production before cluster mutation begins.

If `PROVIDER_VALIDATION_REQUIRE_CURRENT_COMMIT=true`, a required target also fails when its passed evidence was generated from a different source commit.

## Production release flow

The production job now includes the provider gate after staging promotion evidence is verified and before Kubernetes authentication/deployment work.

```text
build + sign image
        |
        v
deploy + validate staging
        |
        v
verify staging promotion evidence
        |
        v
discover retained provider-validation artifacts
        |
        v
build production registry for required targets
        |
        v
evaluate provider validation policy
        |
        +-- off     -> skip
        +-- warn    -> record failures, continue
        +-- enforce -> block on policy failure
        |
        v
production cluster authentication
        |
        v
preflight + progressive canary release
```

The deploy workflow receives only repository/action read access for evidence discovery. Provider credentials are not reused during deployment.

## Policy decision semantics

For each required target, `scripts/provider-validation-policy.py` selects the corresponding registry cell and evaluates:

- the cell exists for the requested environment;
- registry status is `passed`;
- evidence is not expired according to the registry maximum age;
- when exact-commit enforcement is enabled, `commit_matches_expected=true`.

Provider-specific limitations remain visible in the policy decision. When `PROVIDER_OPERATIONAL_EVIDENCE_REQUIRED=false`, those items remain separate production-readiness requirements.

When `PROVIDER_OPERATIONAL_EVIDENCE_REQUIRED=true`, each required target must also have a passed, non-expired `provider-operational-evidence.json` artifact for the same environment. The external evidence pack covers target-specific IAM/access review, real outage exercises, warehouse downstream readback, AI pricing calibration, and regional apply-gate/game-day/RPO-RTO evidence.

Set `PROVIDER_OPERATIONAL_EVIDENCE_REQUIRE_APPROVAL=true` to require dual-controlled evidence. The policy then requires a valid `provider-operational-approval.json` bound to the selected evidence digest. The approver must differ from the authenticated submitter. Any valid `provider-operational-revocation.json` invalidates the matching evidence regardless of whether approval enforcement is enabled.

See [External Provider Operational Evidence](provider-operational-evidence.md).

## Release evidence

When the production gate is active, the deploy workflow retains for 90 days:

```text
provider-validation-release-registry.json
provider-validation-release-registry.md
provider-validation-release-policy.json
provider-validation-release-policy.md

Operational evidence itself is retained separately by the **Provider Operational Evidence** workflow for 180 days. The release policy records the selected operational evidence status, reviewer, expiry and source workflow run for each required target.
```

The policy artifact is tied to the release `GITHUB_SHA`.

A policy decision includes the required targets, environment, gate mode, expected commit, commit-enforcement setting, evidence age, source validation workflow run/attempt, operational evidence digest, submitter, approval state/approver, revocation state, and failure reasons.

## Manual preview

The **Provider Validation Registry** workflow also supports policy evaluation without deploying anything.

Set:

- `required_targets`
- `policy_environment`
- `policy_mode`
- `policy_require_current_commit`
- `policy_require_operational_evidence`
- `policy_require_operational_approval`

This is the recommended way to preview a production policy before switching the deploy environment from `warn` to `enforce`.

## Local evaluation

Given a registry:

```bash
python3 scripts/provider-validation-policy.py evaluate \
  --registry provider-validation-registry.json \
  --environment production \
  --required-targets storage_s3,secret_aws,warehouse_bigquery \
  --expected-commit <git-sha> \
  --mode enforce \
  --output-json provider-validation-policy.json \
  --output-markdown provider-validation-policy.md
```

For exact-commit enforcement, add:

```text
--require-current-commit
```

For external operational controls, add:

```text
--operational-evidence-dir ./provider-operational-evidence
--require-operational-evidence
--require-operational-approval
```

Verify a generated policy artifact:

```bash
python3 scripts/provider-validation-policy.py verify \
  --file provider-validation-policy.json \
  --environment production \
  --required-targets storage_s3,secret_aws,warehouse_bigquery \
  --expected-commit <git-sha> \
  --require-passed
```

## Recommended rollout

A safe adoption sequence is:

1. configure only the providers actually enabled in production;
2. run live provider contracts with `environment=production`;
3. run the registry workflow manually and inspect gaps;
4. set the deploy gate to `warn`;
5. close missing/expired evidence and operational review gaps;
6. switch the production environment to `enforce`;
7. configure `PROVIDER_OPERATIONAL_EVIDENCE_ALLOWED_APPROVERS` and exercise submit/approve/revoke flows;
8. enable `PROVIDER_OPERATIONAL_EVIDENCE_REQUIRE_APPROVAL=true` after approved evidence exists for required targets;
9. enable exact-commit enforcement only when the organization requires provider contracts to be rerun for every release commit.

This keeps unused integrations out of the release critical path while making enabled external dependencies explicit and auditable.
