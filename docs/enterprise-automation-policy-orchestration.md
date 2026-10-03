# Phase 31 — Enterprise Automation, Policy-as-Code & Control Plane Orchestration

Phase 31 introduces a guarded orchestration layer above organization administration, billing entitlements, and platform operations.

## Structured policy-as-code

Policies are data, not executable source code. An organization administrator configures:

- Trigger type.
- Trigger key.
- Comparator.
- Numeric threshold.
- Whitelisted action.
- Structured action configuration.
- Approval mode.
- Cooldown.

No shell command, dynamic Go code, JavaScript, template execution, or arbitrary URL invocation is supported by the policy engine.

## Triggers

### `operations_alert`

Evaluates open Phase 30 operational alerts. `trigger_key` is an alert type such as `slo_breach`, `budget_threshold`, `capacity_forecast`, or `*`.

### `billing_usage_percent`

Evaluates Phase 29 `api_operations` usage against the effective billing-plan API-operation entitlement. The condition value is a percentage and can exceed 100 when usage exceeds the plan limit.

## Comparators

- `eq`
- `gte`
- `lte`

## Guarded runbooks

The initial allowlisted runbook is:

- `operations.open_incident`

Its action configuration accepts `severity`, `title`, and `summary`.

SEV1 and SEV2 incidents cannot use `automatic` approval mode. They require an explicit administrator approval decision. Automatic execution is limited to SEV3/SEV4 incident creation.

## Approval workflow

Execution states include:

`pending_approval → approved → running → succeeded|failed`

or:

`pending_approval → rejected`

Approval/rejection identity and timestamps are persisted. Execution history also stores the trigger snapshot, action result, failure detail, and lifecycle timestamps.

## Idempotency and cooldown

Operational-alert policies use the source alert ID in the execution deduplication key, so one alert cannot repeatedly execute the same rule.

Billing-utilization rules use a cooldown time bucket and also check the most recent policy execution. This prevents a scheduled worker from repeatedly acting on an unchanged condition.

Each organization is limited to 50 automation policies.

## Scheduled evaluation

The existing worker evaluates all enabled policies periodically. Default cadence:

`AUTOMATION_POLL_INTERVAL=5m`

Manual evaluation remains available for administrators.

## Cross-domain orchestration

Phase 31 can consume billing capacity signals from Phase 29 and create operational incidents in the Phase 30 control plane. This is deliberately narrow and allowlisted so the automation engine cannot mutate arbitrary application state.

## Audit trail

Policy create/update, execution request, approval/rejection, and completion write organization audit events. System-scheduled evaluation records a nil user actor for the request event, while a runbook records the effective approving or policy-creator user where appropriate.

## APIs

- `GET|POST /api/organizations/{id}/automation/policies`
- `PUT /api/organizations/{id}/automation/policies/{policy_id}`
- `GET /api/organizations/{id}/automation/executions`
- `POST /api/organizations/{id}/automation/executions/{execution_id}/decision`
- `POST /api/organizations/{id}/automation/evaluate`
