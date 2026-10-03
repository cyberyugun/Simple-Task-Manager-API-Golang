# Infrastructure as Code

Phase 16 introduces provider-specific Terraform stacks plus a provider-neutral Kubernetes platform stack.

## Layout

```text
infra/terraform/
├── aws/
│   ├── versions.tf
│   ├── variables.tf
│   ├── main.tf
│   └── outputs.tf
├── azure/
│   ├── versions.tf
│   ├── variables.tf
│   ├── main.tf
│   └── outputs.tf
├── gcp/
│   ├── versions.tf
│   ├── variables.tf
│   ├── main.tf
│   └── outputs.tf
└── platform/
    ├── versions.tf
    ├── variables.tf
    ├── main.tf
    ├── outputs.tf
    └── terraform.tfvars.example
```

The cloud stacks are alternatives. Pick one cloud stack for a production environment; do not apply AWS, Azure, and GCP to represent one environment.

## What the cloud stacks create

### AWS

The AWS stack provisions:

- VPC with public/private subnets and NAT egress
- VPC flow logs
- encrypted EKS control plane with private API access by default
- managed EKS node group
- GitHub Actions OIDC deployment role and EKS access entry
- encrypted Multi-AZ PostgreSQL RDS with AWS-managed master password
- encrypted Redis replication group with TLS and AUTH

The account-level GitHub OIDC provider is accepted as `github_oidc_provider_arn`. Keep that provider account-scoped so multiple repositories do not create duplicate OIDC providers.

### Azure

The Azure stack provisions:

- resource group and VNet with dedicated AKS/PostgreSQL/Redis subnets
- private AKS by default with Azure RBAC, OIDC issuer and workload identity
- Log Analytics integration and Azure Policy
- private PostgreSQL Flexible Server with zone-redundant HA
- Premium Redis on a dedicated subnet
- user-assigned GitHub deployment identity
- GitHub federated credential restricted to the production environment
- AKS cluster-user and Azure Kubernetes RBAC cluster-admin assignments for deployment

### GCP

The GCP stack provisions:

- required Google APIs
- custom VPC/subnet, Cloud Router and Cloud NAT
- private-service networking
- private GKE control plane by default
- dedicated hardened GKE node service account and managed node pool
- private HA Cloud SQL for PostgreSQL
- private TLS/auth-enabled Memorystore Redis
- GitHub Workload Identity Pool/provider
- dedicated deployment service account

## Kubernetes platform stack

After the cloud cluster exists and kubeconfig is available, the platform stack installs pinned Helm chart versions for:

- cert-manager
- ingress-nginx with an `nginx` IngressClass
- Metrics Server
- External Secrets Operator, optionally

Copy the example variables file and replace every chart placeholder with an approved exact version:

```bash
cd infra/terraform/platform
cp terraform.tfvars.example terraform.tfvars
terraform init
terraform plan
```

Do not commit `terraform.tfvars` when it contains environment-specific or sensitive values.

## Remote state

Production Terraform state must not be stored in Git.

Use an encrypted remote backend with locking and tightly scoped access. Typical choices are:

- AWS: S3 with versioning/encryption plus a supported state-locking mechanism
- Azure: Storage Account blob backend with private access
- GCP: versioned, access-controlled GCS bucket

Bootstrap the remote-state storage separately, then initialize the selected stack with backend configuration supplied outside the repository.

Sensitive data can still exist in Terraform state even when variables and outputs are marked sensitive. Restrict state access as if it were a production secret.

Phase 16.1 adds concrete backend bootstrap stacks under `infra/terraform/bootstrap/` plus protected plan/apply promotion. See [Terraform Delivery and Environment Promotion](terraform-delivery.md).

## Validation

Every IaC pull request runs:

```text
terraform fmt -check -recursive
terraform init -backend=false
terraform validate
```

for AWS, Azure, GCP and the Kubernetes platform stack.

The repository Security workflow also scans Terraform along with other infrastructure configuration.

## Apply workflow

Phase 16 intentionally does not auto-apply Terraform from pull requests.

A production sequence should be:

```text
Terraform validate
      ↓
Terraform plan
      ↓
Human review / environment approval
      ↓
Terraform apply
      ↓
Cloud cluster + DB + cache ready
      ↓
Platform Terraform apply
      ↓
scripts/production-preflight.sh
      ↓
Application Deploy workflow
```

Keep infrastructure provisioning and application deployment separate so infrastructure changes cannot silently ride along with an application release.

## Private cluster runners

All cloud stacks default to private Kubernetes API endpoints.

That means the deployment job needs network access to the private control plane. Use a self-hosted/private runner, VPN-connected runner, or another approved private execution environment.

If a public control-plane endpoint is explicitly enabled, restrict its allowed CIDRs. Never expose a production Kubernetes API to `0.0.0.0/0`.

## First apply

For the selected provider:

```bash
cd infra/terraform/<aws|azure|gcp>
terraform init -backend-config=...
terraform plan -out=production.tfplan
terraform apply production.tfplan
```

Then configure the GitHub `production` Environment with the Terraform outputs required by `.github/workflows/deploy.yml`.

Finally apply the platform stack, run the production preflight, and trigger the signed application deployment.
