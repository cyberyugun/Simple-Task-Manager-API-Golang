# API Changelog

## 1.9.0 — Phase 45

Compatibility line: **v1**.

Additive platform extensibility and application marketplace release:

- adds remote-only marketplace application manifests that never execute arbitrary extension code in the core API process
- adds publisher verification and application review lifecycle
- adds marketplace metadata, categories and declarative workflow/compliance/reporting packs
- adds organization-approved install, secret rotation and uninstall lifecycle
- adds one-time hashed installation secrets and scoped short-lived extension token exchange
- adds active-installation and quota enforcement for extension API traffic
- adds manifest-constrained durable event webhook subscriptions using the existing outbox delivery runtime
- adds separate extension configuration and external secret-reference storage
- adds daily/monthly usage quota analytics and response latency/error tracking
- adds organization/workspace audit records and extension installation lifecycle domain events
- preserves all existing v1 paths, operation IDs, request requirements and response fields


## 1.8.0 — Phase 44

Compatibility line: **v1**.

Additive enterprise data platform and BI federation release:

- adds governed BigQuery, Snowflake, Redshift and Databricks warehouse connection metadata
- adds Power BI, Tableau and Looker-oriented analytical contracts
- adds full and incremental organization exports with deterministic checkpoints
- adds tenant-key isolation and configurable field masking before warehouse delivery
- adds backward-compatible analytical schema evolution
- adds export jobs, failure state and checkpoint-safe recovery
- adds data lineage records and governance tags
- adds reverse-ETL hook metadata
- adds per-connection freshness SLO and monthly estimated export-cost budget controls
- adds an operational data-platform dashboard
- preserves all existing v1 paths, operation IDs, request requirements and response fields


## 1.7.0 — Phase 43

Compatibility line: **v1**.

Additive zero-trust and advanced-security release:

- adds organization zero-trust risk policy and network CIDR controls
- adds workspace-scoped SPIFFE-style workload identities and X.509 certificate rotation
- adds mTLS workload token exchange with certificate-thumbprint token binding
- adds device trust, impossible-travel, anomaly, WAF and token-binding risk signals
- adds adaptive allow / step-up / revoke decisions and automatic high-risk refresh-session revocation
- adds tenant security posture dashboard and incremental security-event feed
- adds signed SHA-256 hash-chained audit checkpoints and WORM export manifests
- adds SIEM destination metadata and incremental federation feed
- preserves all existing v1 paths, operation IDs, request requirements and response fields


## 1.6.0 — Phase 42

Compatibility line: **v1**.

Additive global multi-region and data-residency release:

- adds organization home-region, allowed-region, failover-region, RPO and RTO policy
- adds regional resource-placement inventory for database, storage, metadata and other governed resources
- adds governed region-migration request, approval/rejection, checkpoint and completion lifecycle
- adds governed cross-region transfer approval, audit and completion lifecycle
- adds failover game-day evidence with measured RPO/RTO compliance
- integrates attached workspace `allowed_data_regions` and `restrict_cross_region_transfer` governance controls
- adds provider-neutral primary/failover routing intent and residency compliance reporting
- preserves all existing v1 paths, operation IDs, request requirements and response fields


## 1.5.0 — Phase 24

Compatibility line: **v1**.

Additive asynchronous-processing release:

- adds optional `X-Idempotency-Key` on `POST /api/tasks`
- adds workspace webhook subscription list/create/delete endpoints
- adds signing-secret-on-create behavior
- adds task domain events `task.created`, `task.updated`, and `task.deleted`
- adds transactional PostgreSQL outbox and background delivery workers
- adds retry, dead-letter, and replay semantics
- preserves existing task behavior when no idempotency key is supplied
- preserves all existing v1 paths, operation IDs, required request properties, and response fields


## 1.4.0 — Phase 23

Compatibility line: **v1**.

Additive authorization and tenancy release:

- adds personal and shared workspaces
- adds owner/admin/member RBAC
- adds optional `X-Workspace-ID` selection for existing task endpoints
- preserves no-header task behavior by resolving the user's personal workspace
- adds workspace membership-management APIs
- adds owner/admin workspace audit API
- adds optional `workspace_id` to task responses
- adds cross-tenant live contract validation
- preserves existing v1 endpoint paths, operation IDs, required request fields, and existing response fields

The migration retains legacy task ownership fields during the rolling-deployment compatibility window so old and new pods cannot accidentally cross tenant boundaries.


## 1.3.0 — Phase 22

Compatibility line: **v1**.

Non-breaking governance release:

- declares the existing public API as compatibility line v1
- adds `X-API-Version: v1` and `API-Supported-Versions: v1` runtime headers
- documents existing `429 Too Many Requests` behavior for register, login, refresh, and logout
- adds backward-compatibility diff checks
- adds consumer-driven contract expectations
- adds live provider/schema contract validation
- adds generated TypeScript SDK artifacts
- adds formal versioning and deprecation policy

No existing endpoint, request property, response property, operation ID, or authentication requirement was removed or incompatibly changed.

## 1.2.0

Pre-Phase-22 OpenAPI contract for system, authentication, session/account security, and task-management APIs.
