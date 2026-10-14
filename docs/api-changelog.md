# API Changelog

## 1.14.0 — Phase 40 post-roadmap developer portal governance hardening

Compatibility line: **v1**.

Additive developer-platform hardening:

- enriches developer documentation search with deterministic relevance ranking across path, method, operation ID, tags, summary and description
- adds bounded `limit` support to documentation search while preserving the existing `q` query behavior
- exposes OpenAPI operation IDs and tags in documentation search results
- adds review rounds and submitter metadata to developer applications
- appends immutable approve/reject/suspend review history with from/to status, submitter, reviewer, note and timestamp
- adds a read-only application review-history endpoint
- preserves the existing submit/review routes, lifecycle decisions, credential issuance rules and v1 authentication behavior
- adds migration 036 plus unit, PostgreSQL integration and Docker smoke coverage

## 1.13.0 — Phase 39 post-roadmap event governance hardening

Compatibility line: **v1**.

Additive event-fabric governance release:

- expands schema ownership metadata with optional owning service and operational contact fields
- records deprecation actor, reason and optional replacement schema ID on every deprecated schema version
- keeps the existing no-body deprecation call compatible by defaulting the reason to `operator_requested`
- validates replacement schemas are active versions of the same event type
- automatically records the newest active sibling as the replacement when one exists and none is explicitly supplied
- preserves deprecated schema definitions for exact-version validation so historical outbox events and range replay remain processable
- adds migration 035 plus unit, PostgreSQL integration and Docker smoke coverage
- preserves existing Phase 39 routes, operation IDs and v1 event-delivery contracts

## 1.12.0 — Phase 38 post-roadmap connector credential governance

Compatibility line: **v1**.

Additive connector secret-governance hardening:

- adds organization credential policy for rotation cadence, automatic stored-secret rotation, and strict least-privilege OAuth scope enforcement
- adds a rotation worker that rewrites due credentials through the active database/Vault/AWS/Azure/GCP backend without changing provider-facing OAuth tokens
- rejects OAuth completion and refresh when strict policy is enabled and the provider grants scopes outside the configured requested scope set
- adds required/granted/missing/excess scope assessment to connection tests
- adds next rotation due metadata without returning secret material
- audits successful and failed credential reads, database fallbacks, writes, refreshes, rotations, revocation and connection-test access with actor attribution when available
- preserves existing connector OAuth, secret-backend, integration connection and v1 contracts

## 1.11.0 — Phase 35 post-roadmap notification hardening

Compatibility line: **v1**.

Additive notification analytics and suppression-rule release:

- adds organization-scoped notification suppression rules with exact-event or wildcard matching
- supports whole-notification suppression plus email, push, and webhook route suppression
- adds optional suppression start/end windows and required operator reasons
- records idempotent suppression events for rule enforcement and user event-preference suppression
- adds organization notification analytics for generated/read/unread counts, delivery states, attempts, suppression totals, event/channel breakdowns, and UTC daily trends
- keeps suppression administration and analytics restricted to organization notification administrators
- preserves existing notification preference, quiet-hours, digest, retry/DLQ, template, and delivery contracts
- preserves all existing v1 paths, operation IDs, request requirements and response fields

## 1.10.0 — Phase 36 post-roadmap attachment hardening

Compatibility line: **v1**.

Additive files and content-security hardening:

- adds explicit attachment version chains with immutable version numbers and previous-version links
- adds list/create-version endpoints while preserving the existing upload contract
- adds owner/admin manual quarantine and reviewed-release endpoints with audit metadata
- adds optional remote image/PDF thumbnail-preview generation with fail-closed HTTPS configuration and signed gateway requests
- adds preview lifecycle metadata and short-lived preview URLs that remain blocked while the attachment is quarantined or infected
- avoids deleting a shared deduplicated source object while another live attachment still references it
- preserves all existing v1 paths, operation IDs, request requirements and response fields

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
