# Progressive Delivery and Release Engineering

Phase 20 changes production delivery from a direct rolling deployment into a health-gated progressive release.

The application remains on native Kubernetes Deployments and NGINX Ingress. No additional rollout controller is required.

## Release flow

```text
Build immutable image
        |
        v
Trivy scan + SBOM
        |
        v
Provenance + SBOM attestations
        |
        v
Cosign keyless signature
        |
        v
Deployment freeze gate
        |
        v
Migration compatibility gate
        |
        v
Required-provider validation policy gate
        |
        v
Production readiness preflight
        |
        v
Expand-compatible DB migration
        |
        v
Canary Deployment (1 replica)
        |
        +--> weighted normal traffic
        +--> X-Canary: always deterministic validation
        |
        v
Canary observation window
        |
        v
Stable Deployment promotion
        |
        v
Zero-unavailable replica verification
        |
        v
Telemetry verification
        |
        v
Move :latest tag to promoted digest
        |
        v
Upload release evidence
```

A failure before stable promotion removes the canary without changing the stable image.

A failure after stable promotion automatically restores the previous stable image and waits for that rollback to become Ready.

Database migrations are **not** automatically reverted. Migrations must therefore follow expand/contract compatibility.

## Deployment freeze

The GitHub `production` Environment can define:

```text
DEPLOYMENT_FREEZE=true
```

When enabled:

- tag-triggered production deployment is blocked
- normal manual deployment is blocked
- a manual `workflow_dispatch` can override only when `override_freeze=true`

Use freeze overrides only for an approved emergency release.

Environment protection rules and required reviewers should remain the human approval layer around the workflow.

## Canary traffic

Defaults:

```text
CANARY_WEIGHT=10
CANARY_OBSERVATION_SECONDS=60
```

Allowed repository policy:

```text
canary weight: 1-50 percent
observation:   15-900 seconds
```

Manual workflow dispatch exposes reviewed choices for:

- 5%, 10%, or 20% traffic
- 30, 60, or 120 seconds observation

The NGINX canary Ingress also supports:

```text
X-Canary: always
```

This header deterministically routes release validation to the canary while normal clients continue to receive weighted traffic.

The workflow validates through both:

1. direct canary Service port-forward
2. the public HTTPS NGINX path using `X-Canary: always`

That catches pod-level failure and ingress/TLS/routing failure before stable promotion.

## Automatic rollback

Before promotion the workflow records the current stable image.

If stable promotion fails:

```text
new stable image
      |
      X health/rollout failure
      |
      v
previous immutable image
      |
      v
kubectl rollout status
```

For an initial bootstrap where no prior stable Deployment exists, a failed promoted Deployment is removed.

The release evidence artifact records:

- release status
- start/end time
- Git commit
- new image digest
- previous image
- canary traffic weight
- observation duration
- migration policy

Artifacts are retained for 90 days by the deployment workflow.

## Stable image tagging

The build job no longer publishes the mutable `:latest` tag.

It publishes only the immutable commit tag and digest.

`:latest` is moved to the release digest **after**:

- canary validation succeeds
- observation succeeds
- stable rollout succeeds
- zero-downtime replica checks succeed

A rejected candidate therefore never becomes `:latest`.

Production itself deploys by immutable digest; the mutable tag is convenience metadata only.

## Migration compatibility

`scripts/check-migration-compatibility.py` blocks operations that cannot safely coexist with the old application version during canary rollout:

- `DROP TABLE`
- `DROP COLUMN`
- `TRUNCATE`
- table/column rename
- incompatible column type changes

It also blocks `CREATE INDEX CONCURRENTLY` because the current migration runner executes each migration inside a transaction.

Use an expand/contract sequence instead.

Example:

### Release A — expand

```sql
ALTER TABLE tasks ADD COLUMN new_value TEXT;
```

Deploy code that can work with both the old and new schema.

### Release B — migrate/read new shape

Backfill and move reads/writes to the new structure.

### Release C — contract

Only after no supported running version needs the old structure should destructive cleanup be considered.

Because Phase 20 deliberately blocks destructive migrations in the standard pipeline, contract work requires a separately reviewed maintenance procedure or future migration capability that proves compatibility.

## Production runtime apply

During progressive rollout the workflow updates runtime resources without prematurely changing the stable Deployment:

- ServiceAccount
- ConfigMap
- Service
- HPA
- PodDisruptionBudget
- NetworkPolicy
- ClusterIssuer
- stable Ingress
- observability resources

The stable Deployment image is changed only after the canary gate passes.

The NetworkPolicy selects the shared:

```text
app.kubernetes.io/part-of: simple-task-manager
```

label so both stable and canary API pods receive the same ingress restrictions.

## Zero-downtime verification

The stable Deployment retains:

```text
maxUnavailable: 0
maxSurge: 1
```

After promotion the workflow checks:

- rollout completed
- no unavailable replicas remain
- available replicas >= desired replicas
- public `/version` reports the expected commit
- Prometheus metrics are exposed
- ServiceMonitor/PrometheusRule/Grafana resources remain present

This proves the deployment converged without intentionally reducing stable availability.

It does not guarantee that every network client experienced zero packet loss; external load balancers and clients can have their own behavior.

## Rollback drill

`scripts/rollback-drill.sh` is intended for staging.

It:

1. captures the current staging image
2. switches to a different known image
3. waits for rollout
4. optionally verifies its expected commit through `/version`
5. restores the original image
6. waits for the restoration rollout

By default it refuses to run in the production `task-manager` namespace.

Production execution requires:

```text
ALLOW_PRODUCTION_ROLLBACK_DRILL=true
```

and should only be used during an approved game day.

Example staging drill:

```bash
KUBE_NAMESPACE=task-manager-staging \
ROLLBACK_DRILL_IMAGE=ghcr.io/cyberyugun/simple-task-manager-api-golang@sha256:<digest> \
ROLLBACK_DRILL_EXPECTED_COMMIT=<commit> \
ROLLBACK_DRILL_BASE_URL=https://staging-api.example.com \
bash scripts/rollback-drill.sh
```

The drill image should already be signed and approved.

## Required production variables

Existing variables remain required, plus these optional controls:

```text
DEPLOYMENT_FREEZE=false
CANARY_WEIGHT=10
CANARY_OBSERVATION_SECONDS=60
PROVIDER_VALIDATION_GATE_MODE=off
PROVIDER_VALIDATION_REQUIRED_TARGETS=
PROVIDER_VALIDATION_MAX_AGE_DAYS=30
PROVIDER_VALIDATION_MAX_ARTIFACTS=200
PROVIDER_VALIDATION_REQUIRE_CURRENT_COMMIT=false
PROVIDER_OPERATIONAL_EVIDENCE_REQUIRED=false
```

Manual dispatch values override the canary weight/observation variables.

The public production hostname must be resolvable and reachable from the GitHub Actions runner because the canary gate validates the real HTTPS ingress.

## CI release policy

Normal CI now validates:

- progressive release shell syntax
- rollback drill syntax
- migration checker syntax
- all current migrations against expand/contract policy
- invalid destructive migration is rejected
- invalid canary weight is rejected
- canary Kubernetes resources are valid YAML
- stable Deployment keeps `maxUnavailable: 0`
- build step does not publish `:latest`
- freeze and progressive release workflow steps remain present
- production provider validation gate remains present and read-only evidence access is explicit
- `:latest` promotion remains after successful release

These controls make accidental removal of the release safety model a merge-blocking change.

## Provider validation release policy

Production can make external-provider evidence part of the release critical path without requiring every supported integration.

The `production` GitHub Environment defines `PROVIDER_VALIDATION_REQUIRED_TARGETS`. Only those targets are evaluated. The gate supports `off`, `warn` and `enforce` modes, configurable evidence expiry, and optional exact-commit enforcement.

When active, the production deploy job discovers retained provider-validation artifacts with read-only Actions permission, builds a production-only registry for the required targets, evaluates `scripts/provider-validation-policy.py`, and stores the release registry/policy evidence for 90 days before any Kubernetes authentication or mutation.

Use `warn` during rollout. Use `enforce` after the enabled production providers have passing evidence. When external operational exercises are also mandatory, set `PROVIDER_OPERATIONAL_EVIDENCE_REQUIRED=true`; required providers must then have non-expired operational evidence before production cluster mutation begins.

See [provider validation policy](provider-validation-policy.md) and [external provider operational evidence](provider-operational-evidence.md).

## Staging and production promotion

This repository now provides the production progressive-release engine and a staging-safe rollback drill, but it does **not** claim that a separate staging cluster/namespace is already provisioned.

For organizations requiring mandatory environment promotion, use the same immutable digest:

```text
development validation
        |
        v
staging deployment and acceptance
        |
        v
record approved image digest
        |
        v
production canary using the identical digest
```

Do not rebuild the image between staging approval and production. Promotion should move the already-signed digest, not create a new artifact.

A future phase can provision a dedicated staging environment and make staging evidence a mandatory production dependency.
