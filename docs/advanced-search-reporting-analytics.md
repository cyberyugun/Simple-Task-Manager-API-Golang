# Phase 37 — Advanced Search, Reporting & Analytics

Phase 37 makes task data discoverable and measurable without replacing PostgreSQL as the default system of record.

## Architecture

```text
TASK / EVENT DATA
      |
      +--> PostgreSQL weighted full-text index
      |       |
      |       +--> Search API --> saved views
      |
      +--> Analytics queries --> dashboard / workload / trend
      |
      +--> Export service --> JSON / CSV
      |
      +--> Scheduled report queue --> worker --> report run history
```

The application depends on the `SearchAnalyticsRepository` abstraction. PostgreSQL is the first implementation. A future OpenSearch or Elasticsearch adapter can implement the same service contract without changing handlers.

## Search

`GET /api/search/tasks?q=...`

Supported filters:

- `q`: PostgreSQL `websearch_to_tsquery` expression.
- `status`
- `priority`
- `project_id`
- `assignee_id`
- `page`
- `limit` up to 100

The migration adds a generated `tsvector` with title weight **A** and description weight **B**, plus a GIN index. Search returns rank, a highlighted description headline, and the task payload.

The weighted index is the initial ranking control. Synonym dictionaries or OpenSearch analyzers can be introduced behind the repository abstraction later without changing the public API.

## Saved searches and views

`GET|POST /api/search/saved-views`

`DELETE /api/search/saved-views/{view_id}`

A view stores a bounded JSON filter object. Private views are visible only to their creator; shared views are readable by workspace members. Delete remains owner-scoped.

## Analytics

`GET /api/analytics/dashboard?days=30`

Dashboard metrics include:

- total/open/completed/overdue tasks;
- completion rate;
- average cycle time from task creation to completion;
- average lead time from task start to completion;
- counts by status and priority;
- assignment workload;
- estimated versus actual minutes;
- created/completed/overdue trends.

The reporting window is 7–365 days.

Convenience endpoints expose focused subsets:

- `GET /api/analytics/workload`
- `GET /api/analytics/trends?days=30`

## Exports

`GET /api/reports/export?format=json`

`GET /api/reports/export?format=csv`

Optional filters are `query`, `status`, `priority`, `project_id`, and `assignee_id`.

Exports are capped at 10,000 task rows per request so reporting cannot accidentally become an unbounded memory query. The response includes `X-Report-Row-Count`.

These task-oriented JSON/CSV contracts are intentionally flat and BI-friendly.

## Scheduled reports

`GET|POST /api/reports/schedules`

`DELETE /api/reports/schedules/{report_id}`

`GET /api/reports/runs`

Schedules support daily and weekly recurrence, timezone-aware local hour selection, JSON/CSV format, filters, and optional `run_now` for an immediate first execution.

The worker claims due rows with `FOR UPDATE SKIP LOCKED`. A stale running claim can be recovered after 15 minutes. Each execution is persisted in `report_runs` with status, row count, generated payload, timestamps, and failure text.

Runtime configuration:

```text
REPORT_SCHEDULE_POLL_INTERVAL=1m
REPORT_SCHEDULE_BATCH_SIZE=50
```

## Performance and retention

Migration `022_search_reporting_analytics.sql` adds:

- GIN full-text search index;
- workspace/status/priority/time analytics index;
- saved-view indexes;
- due-schedule index;
- report-run history indexes.

`tests/performance/search-api.js` provides a k6 search/dashboard regression workload.

Report-run retention is intentionally policy-driven rather than hard-coded in this phase. The `report_runs.created_at` index makes later retention/aggregation automation safe to add through the existing lifecycle/governance platform.

## Security

All endpoints use the resolved workspace scope and existing task read/write token scopes. Search, analytics, exports, and report history require task-read access; creating/deleting saved views or report schedules requires task-write access. Raw generated scheduled-report payloads are persisted for governed downstream delivery but are not exposed by the list-runs endpoint.

## Delivery gates

Phase 37 is considered complete when:

- migration 022 applies idempotently;
- full-text search uses the GIN-backed generated vector;
- saved views persist and enforce workspace ownership;
- dashboard/workload/trend analytics are available;
- CSV and JSON exports work;
- scheduled reports execute from the worker and create run history;
- PostgreSQL integration coverage passes;
- OpenAPI governance passes;
- Docker runtime smoke exercises search, analytics, export, and scheduling;
- the search/dashboard k6 workload is available for load testing.
