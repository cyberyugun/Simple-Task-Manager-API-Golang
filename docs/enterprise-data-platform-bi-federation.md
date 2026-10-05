# Phase 44 — Enterprise Data Platform & BI Federation

Phase 44 implements the roadmap flow:

```text
OPERATIONAL DB / EVENT FABRIC
    -> EXPORT CHECKPOINT
    -> MASK / GOVERN
    -> WAREHOUSE
    -> BI
```

## Scope

- Provider-neutral warehouse connections for BigQuery, Snowflake, Redshift and Databricks.
- Power BI, Tableau and Looker-oriented analytical contracts.
- Full and incremental task-analytics exports with deterministic checkpoints.
- Organization/workspace tenant isolation.
- Field masking using none, redact or SHA-256 hash modes.
- Versioned analytical dataset schemas with backward-compatibility checks.
- Persisted export jobs, failure state and repeatable recovery.
- Lineage metadata and governance tags for every successful export.
- Reverse-ETL hook metadata.
- Per-connection freshness SLO and estimated export-cost budget.
- Operational dashboard for export success/failure, rows, cost and freshness.

## Canonical dataset

The canonical `task_analytics` dataset contains organization and workspace tenant keys plus task analytical fields. Export collection only scans workspaces attached to the requested organization. This keeps organization boundaries explicit even when multiple organizations share infrastructure.

## Incremental checkpoint

Incremental ordering uses `updated_at` followed by a deterministic `workspace_id:task_id` record key. A successful delivery atomically advances the stored checkpoint metadata. Failed exports do not advance it.

## Masking and governance

Connections can configure `title` masking as `redact` or `hash`. Masking is applied before warehouse-adapter delivery and before payload hashing. Each successful export stores lineage with the canonical source fields and governance tags.

## Warehouse adapters

The runtime exposes provider adapters for:

- BigQuery
- Snowflake
- Amazon Redshift
- Databricks

Adapters validate provider-specific connection metadata and produce a deterministic delivery receipt. Secrets are referenced by `secret_ref`; credentials are never stored in export payloads.

### Native BigQuery delivery

A production BigQuery adapter is available when `DATA_PLATFORM_BIGQUERY_NATIVE=true`. It uses short-lived Google access tokens from the metadata server by default (GKE Workload Identity / Compute identity) or an explicit access token for controlled local tests. The adapter:

- resolves project and dataset from the connection configuration and table from `config.table` or the connection target;
- checks the target table schema before delivery and creates the canonical table when it is absent;
- uses BigQuery `tabledata.insertAll` with deterministic `insertId` values derived from tenant/task/update identity;
- batches rows, retries 429/408/5xx provider failures with bounded backoff, and treats row-level insert errors as export failures;
- advances the repository checkpoint only after every provider batch is confirmed successful;
- returns a provider delivery URI and payload hash for lineage/audit evidence.

Configuration:

```text
DATA_PLATFORM_BIGQUERY_NATIVE=true
DATA_PLATFORM_BIGQUERY_USE_METADATA=true
DATA_PLATFORM_BIGQUERY_TIMEOUT=20s
DATA_PLATFORM_BIGQUERY_RETRY_ATTEMPTS=3
DATA_PLATFORM_BIGQUERY_RETRY_BACKOFF=250ms
# Optional test/nonstandard endpoints:
# DATA_PLATFORM_BIGQUERY_ACCESS_TOKEN=<short-lived-token>
# DATA_PLATFORM_BIGQUERY_ENDPOINT=https://bigquery.googleapis.com/bigquery/v2
# DATA_PLATFORM_BIGQUERY_METADATA_ENDPOINT=http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token
```

The existing deterministic contract adapters remain the default so local development and existing deployments are unchanged unless native BigQuery delivery is explicitly enabled.

### Native Snowflake delivery

A production Snowflake adapter is available when `DATA_PLATFORM_SNOWFLAKE_NATIVE=true`. It uses the Snowflake SQL API and remains opt-in so current contract-adapter deployments are unchanged. The adapter:

- derives the account endpoint from connection `config.account` unless an operator endpoint override is configured;
- creates the canonical task analytics table when it does not exist;
- performs batched `MERGE` upserts keyed by organization, workspace and task;
- uses deterministic SQL API request IDs so transient retries remain idempotent;
- supports OAuth, programmatic access-token, and pre-issued key-pair JWT bearer token modes;
- polls asynchronous statements and retries 408/429/5xx responses with bounded backoff;
- advances the export checkpoint only after every remote batch succeeds.

Connection configuration requires `account`, `database`, and `schema`; `table`, `warehouse`, and `role` are optional.

Configuration:

```text
DATA_PLATFORM_SNOWFLAKE_NATIVE=true
DATA_PLATFORM_SNOWFLAKE_TOKEN=<secret-injected-token>
DATA_PLATFORM_SNOWFLAKE_TOKEN_TYPE=OAUTH
DATA_PLATFORM_SNOWFLAKE_TIMEOUT=30s
DATA_PLATFORM_SNOWFLAKE_RETRY_ATTEMPTS=3
DATA_PLATFORM_SNOWFLAKE_RETRY_BACKOFF=300ms
DATA_PLATFORM_SNOWFLAKE_POLL_INTERVAL=250ms
# Optional test/nonstandard endpoint:
# DATA_PLATFORM_SNOWFLAKE_ENDPOINT=https://<account>.snowflakecomputing.com
```

BigQuery and Snowflake now have opt-in native delivery.

### Native Amazon Redshift delivery

A production Redshift adapter is available when `DATA_PLATFORM_REDSHIFT_NATIVE=true`. It uses the Redshift Data API and AWS SigV4. The adapter:

- uses AWS static credentials or EKS/IRSA web identity through the existing shared AWS credential provider;
- derives the Data API endpoint from the configured AWS region unless an operator endpoint override is supplied;
- creates the canonical analytics table when it does not exist;
- performs batched `MERGE` upserts keyed by organization, workspace and task;
- supplies deterministic 64-character `ClientToken` values so transient `ExecuteStatement` retries are idempotent;
- polls `DescribeStatement` until `FINISHED`, while surfacing `FAILED` and `ABORTED` states;
- retries 408/429/5xx provider failures with bounded backoff;
- advances the export checkpoint only after all remote statements finish successfully.

Connection configuration requires `cluster`, `database`, and `schema`. Native delivery additionally requires either `db_user` or `secret_arn`; `table` is optional.

Configuration:

```text
DATA_PLATFORM_REDSHIFT_NATIVE=true
DATA_PLATFORM_REDSHIFT_REGION=us-east-1
DATA_PLATFORM_REDSHIFT_TIMEOUT=30s
DATA_PLATFORM_REDSHIFT_RETRY_ATTEMPTS=3
DATA_PLATFORM_REDSHIFT_RETRY_BACKOFF=300ms
DATA_PLATFORM_REDSHIFT_POLL_INTERVAL=300ms
# Uses AWS_ROLE_ARN + AWS_WEB_IDENTITY_TOKEN_FILE for IRSA,
# or AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN.
# Optional test/nonstandard endpoints:
# DATA_PLATFORM_REDSHIFT_ENDPOINT=https://redshift-data.us-east-1.amazonaws.com
# DATA_PLATFORM_REDSHIFT_STS_ENDPOINT=https://sts.us-east-1.amazonaws.com
```

BigQuery, Snowflake and Redshift now have opt-in native delivery.

### Native Databricks delivery

A production Databricks adapter is available when `DATA_PLATFORM_DATABRICKS_NATIVE=true`. It uses the Databricks SQL Statement Execution API and remains opt-in so deployments can retain the deterministic contract adapter by default. The adapter:

- resolves the workspace endpoint from connection `workspace_url` unless an operator endpoint override is configured;
- authenticates with an operator-injected Databricks bearer token;
- executes statements against the configured SQL `warehouse_id`, catalog and schema;
- creates the canonical analytics table as a Delta table when it does not exist;
- performs batched `MERGE` upserts keyed by organization, workspace and task;
- submits statements asynchronously with `wait_timeout=0s` and polls statement state until `SUCCEEDED`;
- surfaces `FAILED`, `CANCELED`, and `CLOSED` terminal states as export failures;
- retries 408/429/5xx HTTP failures with bounded backoff;
- advances the export checkpoint only after every Databricks statement succeeds.

Connection configuration requires `workspace_url`, `warehouse_id`, `catalog`, and `schema`; `table` is optional.

Configuration:

```text
DATA_PLATFORM_DATABRICKS_NATIVE=true
DATA_PLATFORM_DATABRICKS_TOKEN=<secret-injected-token>
DATA_PLATFORM_DATABRICKS_TIMEOUT=30s
DATA_PLATFORM_DATABRICKS_RETRY_ATTEMPTS=3
DATA_PLATFORM_DATABRICKS_RETRY_BACKOFF=300ms
DATA_PLATFORM_DATABRICKS_POLL_INTERVAL=300ms
# Optional test/nonstandard endpoint:
# DATA_PLATFORM_DATABRICKS_ENDPOINT=https://<workspace-host>
```

BigQuery, Snowflake, Redshift and Databricks now all have opt-in native delivery implementations. Remaining work is live-provider IAM, outage and deployment-specific contract validation.

## BI contracts

`GET /api/data-platform/bi-contracts` returns Power BI, Tableau and Looker mappings for the canonical dataset.

## Main APIs

- `GET /api/data-platform/adapters`
- `GET /api/data-platform/bi-contracts`
- `GET|POST /api/organizations/{id}/data-platform/connections`
- `GET|POST /api/organizations/{id}/data-platform/connections/{connection_id}/schemas`
- `GET|POST /api/organizations/{id}/data-platform/connections/{connection_id}/exports`
- `GET /api/organizations/{id}/data-platform/connections/{connection_id}/checkpoint`
- `GET /api/organizations/{id}/data-platform/lineage`
- `GET|POST /api/organizations/{id}/data-platform/reverse-etl-hooks`
- `GET /api/organizations/{id}/data-platform/dashboard`

## Definition of done mapping

The implementation includes warehouse adapters, incremental checkpoints, masking regression tests, lineage metadata and an operational dashboard. The PostgreSQL integration suite validates migration 029 and persistent export state.
