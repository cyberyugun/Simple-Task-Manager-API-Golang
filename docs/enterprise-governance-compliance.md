# Phase 26 — Enterprise Governance, Compliance & Data Protection

Phase 26 adds a workspace governance control plane on top of the Phase 25 enterprise identity foundation.

## Implemented

- Workspace governance policy with classification, retention, privacy SLA, DPA, and data-region controls.
- Data inventory/classification register for application resources and fields.
- Personal-data flags, data-region metadata, and per-field retention targets.
- Legal holds with active/released lifecycle.
- Active legal holds block deletion of a non-personal workspace.
- Privacy/DSAR workflow for access, export, delete, and correction requests.
- Privacy request due dates are derived from the workspace privacy SLA.
- Compliance evidence register for frameworks and controls such as SOC 2, ISO 27001, PCI DSS, or internal controls.
- Governance mutations are written to the existing workspace audit trail.
- PostgreSQL and in-memory repository implementations.
- REST endpoints and OpenAPI coverage.

## Policy

A workspace policy includes a default data classification, audit and operational retention targets, a privacy-request SLA, DPA requirement, and allowed data regions. Cross-region restrictions require at least one allowed region.

Retention values are policy inputs for purge/archival automation. Destructive processing must always check legal holds first.

## Data inventory

The inventory maps a resource type and field name to classification, personal-data status, data region, and retention days. This provides a machine-readable register for privacy reviews and residency controls.

## Legal holds

Legal holds can target the workspace or a named resource. Releasing a hold preserves its history. Any active hold blocks deletion of the non-personal workspace.

## Privacy requests

Supported request types are access, export, delete, and correct. Requests start pending and receive a due date from the configured SLA. Administrators can complete or reject requests; rejection requires a reason.

## Compliance evidence

Evidence records store framework, control ID, evidence type, description, structured metadata, actor, and timestamp. Evidence is append-only through the public API.
