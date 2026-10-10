# Production Deployment Readiness

This document covers the infrastructure prerequisites that must be satisfied before the `Deploy` workflow can safely roll out the API.

## Readiness gate

The production workflow runs:

```bash
bash scripts/production-preflight.sh
```

The gate fails before database migration or application rollout when a required production dependency is missing.

It checks:

- Kubernetes API reachability
- the `task-manager` namespace
- cert-manager CRDs and controller
- the `nginx` IngressClass
- the `metrics.k8s.io` resource metrics API
- the `task-api-secrets` Secret and required keys
- optional/private GHCR pull credentials
- Kubernetes RBAC required to create or update production resources
- permission to create the cert-manager `ClusterIssuer`

## GitHub production environment

Create a GitHub Environment named `production`.

Required variables:

```text
KUBE_AUTH_MODE       aws-eks | azure-aks | gke | kubeconfig
KUBE_INGRESS_HOST    public DNS name for the API
CERT_MANAGER_EMAIL   email used by the Let's Encrypt ACME account
```

Optional readiness variables:

```text
REQUIRE_GHCR_PULL_SECRET=true
REQUIRE_METRICS_SERVER=true
REQUIRE_OBSERVABILITY=true
```

`REQUIRE_METRICS_SERVER` defaults to `true`. Leave `REQUIRE_GHCR_PULL_SECRET` unset when the GHCR image is public.

### AWS EKS

Configure GitHub OIDC trust in AWS and expose:

```text
Secret:
  AWS_ROLE_ARN

Variables:
  AWS_REGION
  EKS_CLUSTER_NAME
```

The trusted role must be restricted to this repository and the production deployment identity, and it must have only the Kubernetes/cloud permissions required by the deployment.

### Azure AKS

Configure an Azure federated credential for GitHub Actions and expose:

```text
Secrets:
  AZURE_CLIENT_ID
  AZURE_TENANT_ID
  AZURE_SUBSCRIPTION_ID

Variables:
  AZURE_RESOURCE_GROUP
  AKS_CLUSTER_NAME
```

### Google GKE

Configure Workload Identity Federation and expose:

```text
Secrets:
  GCP_WORKLOAD_IDENTITY_PROVIDER
  GCP_SERVICE_ACCOUNT

Variables:
  GCP_PROJECT_ID
  GKE_CLUSTER_NAME
  GKE_LOCATION
```

### Provider-neutral fallback

For a self-managed cluster that cannot use GitHub OIDC:

```text
Variable:
  KUBE_AUTH_MODE=kubeconfig

Secret:
  KUBE_CONFIG_B64
```

The workflow emits a warning when this long-lived credential fallback is used.

## Cluster prerequisites

Install these components before the first application deployment:

1. cert-manager, including its CRDs and controller.
2. an ingress controller that provides an IngressClass named `nginx`.
3. Metrics Server, or another implementation that serves the `metrics.k8s.io` API.
4. kube-prometheus-stack/Prometheus Operator with `ServiceMonitor` and `PrometheusRule` CRDs.
5. the `monitoring` namespace and production observability stack before the application overlay.
4. DNS that can point `KUBE_INGRESS_HOST` to the ingress load balancer.
5. PostgreSQL and Redis endpoints reachable from the application namespace.

The repository intentionally does not vendor third-party controller installation manifests. Pin and manage those platform components through the cluster's infrastructure lifecycle so their upgrades are independent from application releases.

## Attachment content-security readiness

Production deployments that accept file attachments should enable fail-closed malware scanning:

```text
ATTACHMENT_SCANNER_REQUIRED=true
ATTACHMENT_SCANNER_URL=https://scanner.internal.example/v1/scan
ATTACHMENT_SCANNER_SIGNING_SECRET=<secret-manager value>
# or ATTACHMENT_SCANNER_BEARER_TOKEN=<secret-manager value>
```

The scanner endpoint must be operator-controlled and HTTPS. Scanner transport/protocol failures quarantine attachments; they do not make files downloadable. Development/test environments may leave the scanner unconfigured and use the no-op implementation.

## Application secrets

Before the first deploy, create `task-api-secrets` in the `task-manager` namespace.

Required keys:

```text
DATABASE_URL
REDIS_URL
JWT_SECRET
```

Prefer External Secrets Operator or another secret manager integration for production. The repository includes `deploy/k8s/optional/external-secret.example.yaml` as a reference.

Do not commit real credentials to the repository.

## GHCR image access

When the container package is private, create `ghcr-pull-secret` and set:

```text
REQUIRE_GHCR_PULL_SECRET=true
```

When the package is public, the preflight gate only warns if the pull secret is absent.

## RBAC requirements

The GitHub deployment identity needs enough Kubernetes access to:

- read Secrets in `task-manager`
- create/delete migration Jobs
- create/patch Deployments
- create Services
- create Ingresses
- create HPAs
- create PodDisruptionBudgets
- create NetworkPolicies
- create cert-manager ClusterIssuers

Use a dedicated deployment role instead of cluster-admin where practical.

## Manual preflight

After authenticating to the production cluster:

```bash
kubectl apply -f deploy/k8s/base/namespace.yaml
KUBE_NAMESPACE=task-manager bash scripts/production-preflight.sh
```

For a private GHCR package:

```bash
REQUIRE_GHCR_PULL_SECRET=true \
KUBE_NAMESPACE=task-manager \
bash scripts/production-preflight.sh
```

A successful result ends with:

```text
Production readiness PASSED.
```

## First production rollout checklist

Before triggering `Deploy` for the first time, confirm:

- production GitHub Environment exists
- required reviewers are configured when approval is desired
- one OIDC provider mode is fully configured
- production DNS is prepared
- cert-manager is running
- the nginx IngressClass exists
- the resource metrics API responds
- application secrets exist
- GHCR pull access matches package visibility
- PostgreSQL and Redis are reachable from the cluster
- the readiness script passes
- a rollback owner and incident contact are known

After the workflow succeeds, verify:

```bash
kubectl -n task-manager get deployment,service,ingress,hpa,pdb
kubectl -n task-manager get pods -l app.kubernetes.io/name=task-api
kubectl get clusterissuer letsencrypt-prod
kubectl -n task-manager get certificate
```

The workflow also verifies that `GET /version` reports the exact Git commit that was deployed.

## Disaster recovery readiness

Before declaring production business-continuity ready:

- run `bash scripts/dr-readiness-check.sh`
- confirm at least two Kubernetes failure domains are available
- confirm three API replicas can become Ready
- run the dependency failure/recovery exercise
- enable the weekly isolated PostgreSQL restore drill
- complete a provider PITR/snapshot restore exercise
- record measured RPO/RTO against `docs/disaster-recovery.md`
- use `docs/dr-game-day-checklist.md` for regional recovery exercises

Repository-level HA does not by itself prove regional DR. Regional recovery must be exercised in the actual selected cloud account.

## External provider validation evidence

Before declaring provider integrations production-ready:

- run **Live Provider Contracts** for every enabled production provider/gateway with environment set to `production`
- retain `provider-validation-evidence.json`, `live-provider-contract.log`, and `provider-failure-injection.log`
- verify each manifest with `scripts/provider-validation-evidence.py verify --require-passed`
- run **Provider Validation Registry** for `production` and review `provider-validation-registry.md`
- configure the `production` GitHub Environment with only the provider targets actually enabled by the deployment
- start with `PROVIDER_VALIDATION_GATE_MODE=warn`, then move to `enforce` after required production evidence exists
- use `PROVIDER_VALIDATION_REQUIRE_CURRENT_COMMIT=true` only when policy requires provider validation on every release commit
- record provider validation, registry and production policy evidence/run IDs in the production-readiness/change record
- record external operational evidence with **Provider Operational Evidence** for IAM/policy review, real outage exercises, warehouse readback, AI pricing calibration, or regional game-day/RPO-RTO as required by the target
- after the operational-evidence rollout is established, set `PROVIDER_OPERATIONAL_EVIDENCE_REQUIRED=true` so required providers need both live validation and non-expired external evidence
- configure `PROVIDER_OPERATIONAL_EVIDENCE_ALLOWED_APPROVERS` with authorized GitHub identities and use **Provider Operational Evidence Governance** to approve production submissions
- set `PROVIDER_OPERATIONAL_EVIDENCE_REQUIRE_APPROVAL=true` only after approved evidence exists for every required production provider
- use the governance workflow to revoke compromised/stale attestations instead of deleting historical artifacts
- preserve the external HTTPS references and their production `reference_sha256` values according to organization retention policy
- review the daily **Provider Operational Evidence Expiry** report; optionally set `PROVIDER_OPERATIONAL_EVIDENCE_EXPIRY_FAIL=true` so invalid required evidence makes the scheduled workflow fail

The evidence manifests bind repository failure-injection results and live-contract results to their exact commits/runs. Operational evidence separately records operator-attested external exercises with explicit expiry and SHA-256 integrity metadata. Optional production dual-control binds the evidence to distinct authenticated submitter/approver identities, while revocation makes a previously approved digest unusable. The repository validates these manifests but does not independently inspect the referenced cloud account or external system. The registry exposes `not_run`, `failed`, `expired`, `revoked`, `unapproved`, and commit-drift gaps instead of assuming missing evidence is healthy.

See `docs/live-provider-contracts.md`, `docs/provider-validation-evidence.md`, `docs/provider-validation-registry.md`, `docs/provider-validation-policy.md`, and `docs/provider-operational-evidence.md`.

## Progressive release readiness

Before enabling production progressive delivery:

- ensure NGINX Ingress supports canary annotations
- ensure the public production hostname is reachable from the GitHub Actions runner
- keep `maxUnavailable: 0` on the stable Deployment
- configure `CANARY_WEIGHT` (default `10`) and `CANARY_OBSERVATION_SECONDS` (default `60`) when different values are required
- use `DEPLOYMENT_FREEZE=true` during an approved freeze window
- confirm all SQL migrations pass `scripts/check-migration-compatibility.py`
- use expand/contract database changes so the old stable version and new canary can run simultaneously
- retain the release evidence artifact for audit and incident review
- exercise `scripts/rollback-drill.sh` in staging before relying on emergency rollback procedures

See `docs/release-engineering.md` and `docs/production-release-checklist.md`.

## Staging promotion readiness

Production promotion now depends on the same immutable digest passing staging.

Before the first release:

- create a GitHub Environment named `staging`
- configure environment-scoped Kubernetes/cloud OIDC credentials
- configure staging `KUBE_INGRESS_HOST`
- provision an isolated PostgreSQL database and Redis service
- create `task-api-secrets` in `task-manager-staging`
- ensure staging DNS/TLS is reachable from the Actions runner
- keep staging and production Terraform state keys/prefixes separate
- run the staging overlay and k6 acceptance gate successfully
- verify `staging-promotion-evidence.json` is produced

Production must not bypass the `needs: [build, staging]` workflow dependency.

See `docs/staging-promotion.md`.
