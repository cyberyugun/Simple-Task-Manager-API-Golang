# Live Provider Contract Harness

The repository includes an opt-in contract harness for validating Phase 36 attachment storage/malware-scanner integrations and Phase 38 external secret backends against real provider environments. Normal unit/CI runs never contact external providers.

## Safety model

The harness is disabled unless `LIVE_PROVIDER_CONTRACTS=true`. The supplied runner enables it only for an explicitly selected target.

Storage contracts use a small fixed text payload under the `provider-contracts/` object prefix and exercise the same native adapter used by the application:

```text
presign upload
  -> PUT fixture with provider-required metadata/encryption headers
  -> authoritative HEAD/metadata verification
  -> presign download
  -> byte-for-byte download verification
  -> provider delete
  -> adapter deletion verification
```

The scanner contract first uploads the same clean fixture through one selected native storage adapter, verifies it, asks the configured scanner gateway to scan that object, requires a clean result with a non-empty engine name, verifies download, and deletes the fixture.

Secret-backend contracts use a unique `provider-contracts/<run>` logical reference and exercise the same `ConnectorSecretStore` implementation used by the application:

```text
Put version 1
  -> Get and verify plaintext + version
  -> Put rotated version 2
  -> Get and verify latest plaintext + increased version
  -> Delete
  -> confirm the secret is no longer readable
```

The contract payload is synthetic and is never printed to the evidence log. Cleanup is attempted on failure as well as success.

The harness does not use production task/workspace records and does not write to the application database.

## Targets

- `storage_s3` — native Amazon S3 or S3-compatible adapter.
- `storage_azure` — native Azure Blob adapter.
- `storage_gcs` — native Google Cloud Storage adapter.
- `scanner` — remote scanner gateway plus one of the storage targets above.
- `secret_vault` — HashiCorp Vault KV v2.
- `secret_aws` — AWS Secrets Manager.
- `secret_azure` — Azure Key Vault.
- `secret_gcp` — Google Cloud Secret Manager.

For `scanner`, set `LIVE_PROVIDER_STORAGE_TARGET` to `storage_s3`, `storage_azure`, or `storage_gcs`.

## Local or deployment-runner execution

Example:

```bash
export LIVE_PROVIDER_CONTRACT_TARGET=storage_s3
export ATTACHMENT_STORAGE_BUCKET=my-contract-test-bucket
export ATTACHMENT_S3_REGION=ap-southeast-1
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...

bash scripts/live-provider-contracts.sh
```

For scanner validation:

```bash
export LIVE_PROVIDER_CONTRACT_TARGET=scanner
export LIVE_PROVIDER_STORAGE_TARGET=storage_s3
export ATTACHMENT_SCANNER_URL=https://scanner.example.com/v1/scan
export ATTACHMENT_SCANNER_BEARER_TOKEN=...
# plus the selected storage-provider variables

bash scripts/live-provider-contracts.sh
```

For a secret backend:

```bash
export LIVE_PROVIDER_CONTRACT_TARGET=secret_vault
export CONNECTOR_VAULT_ADDR=https://vault.example.com
export CONNECTOR_VAULT_TOKEN=...
export CONNECTOR_VAULT_KV_MOUNT=secret
export CONNECTOR_VAULT_PREFIX=simple-task-manager/provider-contracts

bash scripts/live-provider-contracts.sh
```

The runner automatically enables the AWS, Azure or GCP secret adapter when one of those secret targets is selected. Provider credentials and endpoints still come from the same environment variables used by the application.

Plain HTTP remains rejected unless `LIVE_PROVIDER_ALLOW_INSECURE=true` is deliberately set for a local emulator. The GitHub workflow fixes this value to `false`.

## GitHub Actions

`.github/workflows/live-provider-contracts.yml` is manual-only. Run **Live Provider Contracts** with the desired target after configuring the matching `CONTRACT_...` repository/environment secrets.

The workflow stores `live-provider-contract.log` as a 30-day artifact even when the contract fails. This provides provider-test evidence without putting credentials, provider tokens, or fixture contents into repository history.

A missing credential or provider setting for the selected target is a failure, not a skip. This makes a manually requested contract run meaningful.

## IAM expectations

Use a dedicated contract-test bucket/container and least-privilege identity. The identity should be restricted to the contract-test storage location and only the operations required by the selected adapter.

For storage validation, the contract needs the equivalent of object create/write, object metadata/head/read, signed URL support where applicable, and delete. GCS signing additionally requires the configured service-account signing permission. Azure user-delegation SAS requires the identity permissions needed to obtain a delegation key. S3 web-identity runs require the configured STS role trust and object permissions.

The scanner gateway should have read-only access to the same contract-test storage location. The application-side test deletes the fixture after the scan.

Secret-provider identities should be restricted to a dedicated contract-test prefix/project/vault scope and require only create/write, read/access, version rotation and delete permissions. AWS deletion uses the adapter's configured recovery window, so contract secrets are scheduled for deletion rather than force-deleted. Azure Key Vault may retain soft-deleted entries according to vault policy. Use an isolated prefix and lifecycle policy appropriate for repeated contract runs.

## What this proves

A successful storage/scanner run provides environment-specific evidence for authentication, signed upload/download behavior, metadata integrity verification, encryption headers configured by the adapter, provider read/delete permissions, and scanner-to-storage reachability. A successful secret-backend run additionally proves create/read/version-rotation/delete behavior and that the deleted logical secret is no longer readable through the adapter.

It does not by itself prove production retention policy, cross-account policy, outage recovery, replication/failover, or organization-specific compliance. Those still require deployment-specific exercises and review.
