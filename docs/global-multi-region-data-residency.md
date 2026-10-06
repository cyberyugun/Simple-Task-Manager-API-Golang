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

## Automation planning handoff

Post-roadmap hardening adds an opt-in signed planning handoff for explicitly approved region migrations. When `REGION_AUTOMATION_ENABLED=true`, an approval is not persisted until the external planning gateway accepts a `plan_only` request. The handoff:

- requires the migration to be in the explicit admin-approval flow;
- sends source/target/scope plus current allowed-region, residency, RPO and RTO guardrails;
- sets both payload and `X-Region-Automation-Mode` to `plan_only`;
- sets `execution_requires_external_gate=true`, so the contract never authorizes an apply/failover action;
- uses a deterministic request ID as the `Idempotency-Key`;
- HMAC-SHA256 signs `timestamp.payload` with an operator-managed secret;
- retries 408/429/5xx responses with bounded backoff while keeping the same idempotency key and signature;
- refuses redirects and requires HTTPS outside explicitly enabled local development;
- stores the returned plan ID/status/evidence URL in the migration checkpoint after successful approval.

If the planning gateway fails, the migration remains `pending_approval` so the administrator can retry without persisting a half-approved workflow.

Configuration:

```text
REGION_AUTOMATION_ENABLED=true
REGION_AUTOMATION_ENDPOINT=https://deployment-gateway.example.com/v1/region-plans
REGION_AUTOMATION_SIGNING_SECRET=<32+-character-secret>
REGION_AUTOMATION_BEARER_TOKEN=<optional-secret-injected-token>
REGION_AUTOMATION_TIMEOUT=20s
REGION_AUTOMATION_RETRY_ATTEMPTS=3
REGION_AUTOMATION_RETRY_BACKOFF=300ms
REGION_AUTOMATION_MAX_RESPONSE_BYTES=262144
```

## Safety boundaries

The service stores control-plane intent, approved automation plans and evidence; it still does not directly execute DNS changes, cloud database failovers, infrastructure mutations, or customer-data copies. The planning contract contains no apply/execute mode. Promotion of a plan to live infrastructure execution remains an external deployment-system gate, outside the API process.

A region key is provider-neutral and normalized to lowercase (for example `ap-southeast`, `eu-west`, or `aws-ap-southeast-1`). Organizations can align those keys with the values already configured in workspace governance policies.

## Definition of done

- Migration `027_global_region_management.sql`.
- In-memory and PostgreSQL repository parity.
- Organization-admin authorization and organization audit events.
- Workspace governance residency validation.
- Region policy, placement, migration, transfer, route, failover and compliance APIs.
- Unit and PostgreSQL integration coverage.
- OpenAPI compatibility update and runtime smoke coverage.
