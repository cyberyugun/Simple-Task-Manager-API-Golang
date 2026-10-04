# Phase 42 — Global Multi-Region & Data Residency

Phase 42 adds the application control-plane foundation for global routing, tenant home regions, residency enforcement, regional placement metadata, governed region migration, cross-region transfer approval, and failover evidence.

## Core flow

```text
GLOBAL ROUTING
    -> ORGANIZATION HOME REGION
    -> RESIDENCY POLICY
    -> DATABASE / OBJECT / METADATA PLACEMENT
    -> REPLICA / FAILOVER REGION
    -> MIGRATION OR TRANSFER APPROVAL
    -> AUDIT + COMPLIANCE REPORT
```

## Capabilities

- Organization home-region and allowed-region policy.
- Configurable failover regions with RPO/RTO objectives.
- Governance integration with workspace `allowed_data_regions` and `restrict_cross_region_transfer`.
- Regional placement inventory for databases, object storage, metadata, search, event fabric, or other governed resources.
- Region migration workflow with explicit approval and durable checkpoint metadata.
- Cross-region data-transfer approval and audit trail.
- Failover game-day records with measured RPO/RTO and pass/fail evidence.
- Route-decision API exposing the current home and first failover region without embedding cloud-provider routing logic in the application.
- Compliance report that identifies placement and workspace-policy violations.

## API

- `GET /api/regions`
- `GET|PUT /api/organizations/{id}/regions/policy`
- `GET|POST /api/organizations/{id}/regions/placements`
- `GET|POST /api/organizations/{id}/regions/migrations`
- `POST /api/organizations/{id}/regions/migrations/{migration_id}/decision`
- `POST /api/organizations/{id}/regions/migrations/{migration_id}/complete`
- `GET|POST /api/organizations/{id}/regions/transfers`
- `POST /api/organizations/{id}/regions/transfers/{transfer_id}/decision`
- `POST /api/organizations/{id}/regions/transfers/{transfer_id}/complete`
- `GET|POST /api/organizations/{id}/regions/failover-exercises`
- `POST /api/organizations/{id}/regions/failover-exercises/{exercise_id}/complete`
- `GET /api/organizations/{id}/regions/route`
- `GET /api/organizations/{id}/regions/report`

## Safety boundaries

The service stores control-plane intent and evidence; it does not directly execute cloud database failovers or copy customer data. External deployment automation can consume approved migrations and transfer records through the existing governed integration/event layers. This keeps destructive infrastructure actions outside the core API process.

A region key is provider-neutral and normalized to lowercase (for example `ap-southeast`, `eu-west`, or `aws-ap-southeast-1`). Organizations can align those keys with the values already configured in workspace governance policies.

## Definition of done

- Migration `027_global_region_management.sql`.
- In-memory and PostgreSQL repository parity.
- Organization-admin authorization and organization audit events.
- Workspace governance residency validation.
- Region policy, placement, migration, transfer, route, failover and compliance APIs.
- Unit and PostgreSQL integration coverage.
- OpenAPI compatibility update and runtime smoke coverage.
