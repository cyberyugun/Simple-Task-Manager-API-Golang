# Phase 30 — Enterprise Platform Operations, FinOps & Control Plane

Phase 30 adds persisted organization-level operational management above the existing runtime observability, billing, and administration layers.

## Operations policy

Each organization can define:

- Monthly FinOps budget in USD cents.
- Budget alert threshold percentage.
- Availability SLO target in basis points.
- Incident escalation threshold in minutes.
- Support tier: `standard`, `priority`, or `enterprise`.
- Optional support/escalation contact.

When no explicit policy exists, the API uses safe defaults: USD 100 monthly budget, 80% budget alert threshold, 99.90% availability target, and a 30-minute escalation threshold.

## Cost attribution and FinOps

Cost allocation records capture category, source, amount, period, and structured metadata. Dashboard spend is prorated to the portion of each allocation overlapping the current month and current time. The control plane calculates both current spend and projected month-end spend.

Alert evaluation creates idempotent monthly alerts when:

- Current spend reaches the configured threshold.
- Projected spend exceeds the monthly budget.

## Capacity forecasting

Phase 30 consumes Phase 29 billing usage plus effective plan entitlements. Current monthly API operations are projected to month end and compared with `monthly_api_operations`. Projected utilization of 80% or more generates a capacity alert.

## SLO calculation

SEV1 and SEV2 incident intervals count as availability-impacting downtime. Scheduled or completed maintenance windows are excluded from incident downtime; canceled maintenance is not excluded. Overlapping incident and maintenance intervals are merged before duration calculation so downtime is not double counted.

The dashboard reports:

- SLO period.
- Target availability.
- Calculated availability.
- Downtime seconds.
- Whether the target is currently met.

## Incidents and escalation

Incidents support `sev1` through `sev4` and move through `open`, `monitoring`, and `resolved`. Open incidents older than the configured escalation threshold generate operational alerts.

## Maintenance windows

Administrators can schedule maintenance and later mark it `completed` or `canceled`. Upcoming scheduled windows appear in the operations dashboard.

## Alerts

Operational alerts use deterministic fingerprints so repeated evaluation updates an existing condition instead of creating duplicates. Alerts can be acknowledged by organization administrators.

## APIs

- `GET|PUT /api/organizations/{id}/operations/policy`
- `GET|POST /api/organizations/{id}/operations/costs`
- `GET /api/organizations/{id}/operations/alerts`
- `POST /api/organizations/{id}/operations/alerts/{alert_id}/ack`
- `GET|POST /api/organizations/{id}/operations/maintenance`
- `PUT /api/organizations/{id}/operations/maintenance/{window_id}`
- `GET|POST /api/organizations/{id}/operations/incidents`
- `PUT /api/organizations/{id}/operations/incidents/{incident_id}`
- `POST /api/organizations/{id}/operations/evaluate`
- `GET /api/organizations/{id}/operations/dashboard`

Owner, admin, and delegated-admin roles can use this control plane. Every administrative mutation also writes the existing organization audit trail.
