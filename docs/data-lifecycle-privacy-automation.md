# Phase 27 — Data Lifecycle, Privacy Automation & Compliance Operations

Phase 27 turns the Phase 26 governance control plane into executable operations.

## Automated lifecycle

- The existing background worker now runs a lifecycle loop in addition to event delivery.
- Default cadence is one hour and can be overridden with `LIFECYCLE_POLL_INTERVAL`.
- Only workspaces with an explicit governance policy are selected for scheduled processing.
- Any active legal hold records the run as `skipped` and prevents destructive processing.
- Tasks older than `operational_retention_days` are copied to `archived_tasks` and removed through the normal task repository so task-deletion outbox events remain intact.
- Archived snapshots are purged after an additional operational-retention window.
- Every execution is recorded in `governance_lifecycle_runs`.

## Privacy automation

Access/export requests can generate JSON export packages containing request metadata and subject-created tasks. The payload is protected with a SHA-256 checksum.

Delete requests execute content anonymization for subject-created task titles/descriptions. Structural identifiers remain for tenant integrity and auditability. Erasure is blocked by any active legal hold.

## Consent ledger

Consent grants and withdrawals are append-only. Records contain subject, purpose, state, policy version, source, actor, and timestamp.

## Compliance operations

Manual lifecycle runs append compliance evidence using the existing evidence register. Scheduled runs remain independently auditable through lifecycle-run records and workspace audit events. The governance report summarizes classification and retention settings, personal-data inventory, legal holds, privacy SLA backlog, consent activity, and the latest lifecycle execution.

## API

- `GET|POST /api/workspaces/{id}/governance/lifecycle/runs`
- `POST /api/workspaces/{id}/governance/privacy-requests/{request_id}/export`
- `POST /api/workspaces/{id}/governance/privacy-requests/{request_id}/erase`
- `GET|POST /api/workspaces/{id}/governance/consents`
- `GET /api/workspaces/{id}/governance/report`
