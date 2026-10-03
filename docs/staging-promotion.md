# Staging Environment and Promotion Pipeline

Phase 21 makes staging a mandatory release gate before production.

The delivery model is:

```text
source commit / release tag
          |
          v
build image once
          |
          v
scan + SBOM + attest + sign
          |
          v
immutable digest
          |
          v
STAGING
  migration
  rollout
  health/readiness/version
  authenticated k6 smoke
          |
          v
promotion evidence
          |
          v
PRODUCTION
  verify exact staging digest
  verify exact staging commit
  verify same workflow run
          |
          v
Phase 20 canary
          |
          v
stable production
```

The image is never rebuilt between staging and production.

## GitHub Environments

Create two GitHub Environments:

```text
staging
production
```

Both environments can use the same variable names while holding different values and credentials.

Typical variables in each Environment:

```text
KUBE_AUTH_MODE
KUBE_INGRESS_HOST

AWS_REGION
EKS_CLUSTER_NAME

AZURE_RESOURCE_GROUP
AKS_CLUSTER_NAME

GCP_PROJECT_ID
GKE_CLUSTER_NAME
GKE_LOCATION
```

Typical environment-scoped secrets:

```text
AWS_ROLE_ARN

AZURE_CLIENT_ID
AZURE_TENANT_ID
AZURE_SUBSCRIPTION_ID

GCP_WORKLOAD_IDENTITY_PROVIDER
GCP_SERVICE_ACCOUNT

KUBE_CONFIG_B64
```

OIDC modes remain preferred over kubeconfig.

The `production` Environment should retain required reviewers. Staging normally does not require a human approval unless organizational policy requires it.

## Staging Kubernetes environment

The staging overlay is:

```text
deploy/k8s/overlays/staging/
```

Default namespace:

```text
task-manager-staging
```

It includes:

- dedicated Namespace
- staging ConfigMap
- API Deployment
- Service
- HPA
- PDB
- NetworkPolicy
- HTTPS Ingress
- ServiceMonitor

Staging defaults are intentionally smaller than production:

```text
API replicas: 1
HPA: 1-3
DB application pool: 5 connections/pod
Redis client pool: 10
```

The application image remains the exact production candidate digest.

## PostgreSQL and Redis isolation

Staging must not use production PostgreSQL or Redis.

The staging namespace must contain its own:

```text
task-api-secrets
```

with:

```text
DATABASE_URL
REDIS_URL
JWT_SECRET
```

When staging and production use the same Kubernetes cluster and the workflow can read both namespaces, `scripts/staging-preflight.sh` compares the encoded `DATABASE_URL` and `REDIS_URL` values and fails if they match.

When a completely separate staging cluster is used, the environment/cluster boundary itself separates Kubernetes secrets, and the staging cloud stack should create separate data services.

## Terraform staging infrastructure

The AWS, Azure, and GCP Terraform stacks already derive resource names from:

```hcl
environment = "staging"
```

Phase 21 adds examples:

```text
infra/terraform/aws/staging.tfvars.example
infra/terraform/azure/staging.tfvars.example
infra/terraform/gcp/staging.tfvars.example
```

A staging stack must use a **different remote-state object** from production.

Examples:

### AWS

```bash
terraform init \
  -backend-config='key=task-manager/staging/terraform.tfstate'
terraform plan -var-file=staging.tfvars
```

### Azure

Use a different backend `key`, for example:

```text
task-manager/staging/terraform.tfstate
```

### GCP

Use a different backend prefix, for example:

```text
task-manager/staging
```

Never reuse the production remote-state key/prefix for staging.

Because the cloud stacks create PostgreSQL and Redis resources using the environment-derived name, a separately initialized staging stack creates separate managed database/cache resources.

## Staging preflight

Before staging deployment, `scripts/staging-preflight.sh` verifies:

- Kubernetes API access
- staging Namespace
- NGINX IngressClass
- cert-manager API
- staging `task-api-secrets`
- non-empty database, Redis, and JWT keys
- database/Redis secret values differ from production when comparison is possible
- required deployment RBAC

Default:

```text
REQUIRE_DATA_ISOLATION=true
```

Do not disable this merely to make a release pass.

## Staging migration

The candidate's migration binary runs against the **staging database first**.

The same Phase 20 expand/contract compatibility policy applies before any deployment:

```text
DROP TABLE      blocked
DROP COLUMN     blocked
TRUNCATE        blocked
rename          blocked
unsafe type change blocked
```

A migration that fails in staging prevents production from starting.

## Acceptance gate

After rollout, the workflow validates the public HTTPS staging endpoint:

```text
GET /health
GET /ready
GET /version
```

`/version` must report the same commit that created the image.

Then the existing k6 smoke profile executes an authenticated task lifecycle against staging:

```text
register
list
create
complete
get
delete
```

The production job cannot start until this succeeds.

## Promotion evidence

After successful staging validation:

```text
staging-promotion-evidence.json
```

is generated and retained for 90 days.

Evidence records:

- status
- environment
- Git commit
- immutable image digest
- GitHub workflow run ID
- source ref
- staging URL
- completed timestamp
- required acceptance checks

The staging job also exposes the same identity values as job outputs.

Before production starts, it requires:

```text
staging status == passed
staging digest == build digest
staging commit == current commit
staging workflow run == current workflow run
```

This prevents a previously approved digest, unrelated commit, or evidence from another run from being silently substituted.

## Production promotion

Production remains protected by the Phase 20 controls:

- production Environment approval
- deployment freeze
- image signature verification
- canary traffic
- deterministic `X-Canary` validation
- observation window
- rollback on failed promotion
- zero-unavailable replica verification

The key difference in Phase 21 is that production is now downstream of staging.

## Staging host

The `staging` GitHub Environment must set:

```text
KUBE_INGRESS_HOST=staging-api.example.com
```

DNS must resolve to the staging ingress before the release workflow runs.

The workflow verifies the real HTTPS endpoint, not only a cluster-local Service.

## Failure behavior

Any of these blocks production:

- staging cloud authentication failure
- missing isolated staging secrets
- migration failure
- rollout failure
- health/readiness failure
- wrong version/commit
- k6 threshold failure
- missing or mismatched promotion evidence

A failed staging release leaves production unchanged.

## Staging rollback

Use the Phase 20 staging-safe rollback drill:

```bash
KUBE_NAMESPACE=task-manager-staging \
ROLLBACK_DRILL_IMAGE=ghcr.io/cyberyugun/simple-task-manager-api-golang@sha256:<known-digest> \
ROLLBACK_DRILL_EXPECTED_COMMIT=<known-commit> \
ROLLBACK_DRILL_BASE_URL=https://staging-api.example.com \
bash scripts/rollback-drill.sh
```

## Environment parity

Staging should match production in:

- Kubernetes version family
- ingress controller behavior
- cert-manager
- migration mechanism
- application image
- PostgreSQL major version
- Redis major/service family
- observability protocol
- authentication/security behavior

It may use smaller compute sizes and fewer replicas.

Do not intentionally give staging a different schema, custom application build, or production database.

## Phase 21 completion boundary

The repository now contains the staging overlay and mandatory promotion pipeline.

It does **not** claim a real staging cluster/database/cache has already been provisioned. Actual provisioning requires cloud credentials, DNS, GitHub Environment configuration, and environment-scoped secrets.
