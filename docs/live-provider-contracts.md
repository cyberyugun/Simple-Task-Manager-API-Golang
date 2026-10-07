# Live Provider Contract Harness

The repository includes an opt-in contract harness for validating Phase 36 attachment storage/malware-scanner integrations, Phase 38 external secret backends, Phase 44 native warehouse delivery, the Phase 41 remote structured AI gateway, and the Phase 42 plan-only region-automation gateway against real provider environments. Normal unit/CI runs never contact external providers.

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

Warehouse contracts use one dedicated contract table (default `stm_provider_contract`) and a synthetic analytics row. The first delivery exercises provider authentication plus table create/reconcile and write/merge. The same payload is then delivered again and must produce the same batch ID, payload hash and delivery URI, which verifies the adapter's deterministic idempotency contract. BigQuery uses deterministic streaming insert IDs; Snowflake, Redshift and Databricks use MERGE semantics.

Warehouse contracts intentionally do not drop the table after each run. Use a dedicated dataset/schema/catalog and lifecycle policy for contract data.

The remote-AI contract sends only a synthetic task-summary request. It accepts only `public` or `internal` classification, derives the same deterministic request ID twice, performs the same request twice through the production adapter, and requires the gateway to return an identical idempotent response (model, structured result, usage and calculated cost). No user task text, organization data or production context is used.

The region-automation contract creates only an in-memory synthetic migration marked `approved` for contract validation. It sends that migration twice through the production planner and requires a stable request ID and plan ID. The request remains `mode=plan_only`, carries `execution_requires_external_gate=true`, and contains no command that applies infrastructure, changes DNS, fails over a database, or copies customer data.

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
- `warehouse_bigquery` — native BigQuery streaming delivery.
- `warehouse_snowflake` — Snowflake SQL API table create + MERGE.
- `warehouse_redshift` — Redshift Data API table create + MERGE.
- `warehouse_databricks` — Databricks SQL Statement Execution + Delta MERGE.
- `ai_remote` — remote structured AI gateway using a synthetic task-summary request.
- `region_automation` — signed external region-migration planning gateway; plan-only, never apply/failover.

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

For a warehouse provider:

```bash
export LIVE_PROVIDER_CONTRACT_TARGET=warehouse_bigquery
export DATA_PLATFORM_BIGQUERY_ACCESS_TOKEN=...
export DATA_PLATFORM_BIGQUERY_USE_METADATA=false
export LIVE_WAREHOUSE_BIGQUERY_PROJECT_ID=my-project
export LIVE_WAREHOUSE_BIGQUERY_DATASET=provider_contracts
export LIVE_WAREHOUSE_TABLE=stm_provider_contract

bash scripts/live-provider-contracts.sh
```

Use the corresponding `LIVE_WAREHOUSE_SNOWFLAKE_...`, `LIVE_WAREHOUSE_REDSHIFT_...`, or `LIVE_WAREHOUSE_DATABRICKS_...` connection values for the other targets. The runner automatically enables the selected native warehouse adapter.

For the remote AI gateway:

```bash
export LIVE_PROVIDER_CONTRACT_TARGET=ai_remote
export AI_REMOTE_PROVIDER_ENDPOINT=https://ai-gateway.example.com/v1/generate
export AI_REMOTE_PROVIDER_TOKEN=...
export AI_REMOTE_PROVIDER_MODEL=provider-model-name
export AI_REMOTE_PROVIDER_SUPPORTED_CLASSIFICATIONS=public,internal
export LIVE_AI_CLASSIFICATION=public

bash scripts/live-provider-contracts.sh
```

The AI contract defaults to `public` classification. `LIVE_AI_CLASSIFICATION` may be set to `internal`, but confidential/restricted classifications are rejected by the live harness so synthetic validation cannot accidentally become a path for sensitive data.

For the region-automation planning gateway:

```bash
export LIVE_PROVIDER_CONTRACT_TARGET=region_automation
export REGION_AUTOMATION_ENDPOINT=https://deployment-gateway.example.com/v1/region-plans
# Load REGION_AUTOMATION_SIGNING_SECRET from your secret store (minimum 32 characters)
export REGION_AUTOMATION_BEARER_TOKEN=...
export LIVE_REGION_SOURCE_REGION=ap-southeast
export LIVE_REGION_TARGET_REGION=ap-northeast
export LIVE_REGION_ALLOWED_REGIONS=ap-southeast,ap-northeast
export LIVE_REGION_RPO_SECONDS=300
export LIVE_REGION_RTO_SECONDS=1800

bash scripts/live-provider-contracts.sh
```

The source and target must differ, and the allowed-region list must include both the source and target. The contract constructs its own synthetic organization/migration identifiers and never persists them to the application database.

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

Warehouse identities should be scoped to a dedicated contract dataset/database/schema/catalog and the minimum permissions needed to create or reconcile the contract table and write/merge rows. Redshift additionally needs Data API execution/statement-status permissions. Databricks needs SQL warehouse use plus catalog/schema/table permissions. Snowflake needs the configured warehouse/role privileges. BigQuery needs table metadata/create plus streaming insert permissions.

The remote AI gateway token should be a dedicated least-privilege contract credential when the gateway supports scoped credentials. The gateway is expected to honor the deterministic `Idempotency-Key`, echo the request ID when supported, return valid structured output and usage, and enforce its own model-access policy. The workflow never prints the bearer token or synthetic request body.

The region-automation gateway credential must be planning-only. Its policy should permit validation or creation of migration plans but deny infrastructure apply, DNS mutation, database failover, data-copy execution, or equivalent destructive actions. The gateway should verify the HMAC signature/timestamp, honor the deterministic idempotency key, and return a stable `plan_id` for duplicate requests. Any promotion from a plan to execution must remain behind a separate external authorization gate.

## What this proves

A successful storage/scanner run provides environment-specific evidence for authentication, signed upload/download behavior, metadata integrity verification, encryption headers configured by the adapter, provider read/delete permissions, and scanner-to-storage reachability. A successful secret-backend run additionally proves create/read/version-rotation/delete behavior and that the deleted logical secret is no longer readable through the adapter. A successful warehouse run proves that the configured native adapter can authenticate, create/reconcile its contract table, complete a real provider delivery and repeat the same payload with deterministic batch identity. A successful remote-AI run proves gateway authentication/connectivity, supported classification handling, structured task-summary output, usage bounds, local cost accounting and idempotent duplicate handling for the synthetic request. A successful region-automation run proves that the configured planning gateway accepts the production signed plan-only contract for a synthetic approved migration and preserves plan identity across duplicate submissions; it does not prove or execute cloud failover.

It does not by itself prove production retention policy, cross-account policy, outage recovery, replication/failover, or organization-specific compliance. Those still require deployment-specific exercises and review.
