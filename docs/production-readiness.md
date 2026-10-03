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
