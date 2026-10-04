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
