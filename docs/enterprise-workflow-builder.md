# Phase 34 — Enterprise Workflow Builder & Event-Driven Orchestration

Phase 34 turns the Phase 31 single-rule automation layer into a durable, versioned multi-step workflow engine. Workflows remain organization-scoped and are administered by owners, admins, and delegated admins.

## Safety boundary

The core API never evaluates arbitrary shell commands, user code, templates with code execution, or dynamically loaded executables. Action nodes resolve only through the allowlisted executor registry. The initial registry includes:

- `workflow.noop`
- `task.set_status`
- `operations.open_incident`

The node-schema endpoint exposes the currently allowed node and executor types.

## Lifecycle

A workflow definition owns immutable published versions.

```text
CREATE WORKFLOW
  -> DRAFT VERSION
  -> VALIDATE DAG
  -> PUBLISH (immutable snapshot + SHA-256 checksum)
  -> ACTIVATE
  -> START / EVENT TRIGGER
```

Editing a published version is rejected. Create a new draft version, optionally cloned from an earlier version, then publish and activate it. Each execution persists the published graph as `workflow_snapshot`, so later version changes cannot alter in-flight or historical execution behavior.

## DAG node types

- `trigger` — manual, event, scheduled, connector, or internal.
- `condition` — evaluates a variable expression and routes true/false edges.
- `transform` — deterministic variable assignment/mapping without arbitrary code.
- `approval` — pauses until an authorized administrator approves or rejects.
- `action` — invokes an allowlisted executor with timeout/retry policy.
- `delay` — durable timer backed by `resume_at`.
- `branch` — explicit true/false routing.
- `parallel` — fan-out to multiple outgoing DAG branches.
- `join` — waits for `all` or `any` predecessor branches.
- `subworkflow` — invokes another active workflow and polls until completion.
- `end` — ends a path.

Graph validation rejects cycles, missing node references, incoming edges to the trigger, outgoing edges from end nodes, invalid compensation links, unknown executors, unsafe retry/timeout values, and oversized graphs.

## Variable mapping

Executions have a JSON variable context. Trigger payload is stored under `trigger`. Input mappings resolve paths such as `$trigger.task_id`. Output mappings persist executor or transform outputs into named workflow variables.

Example action node:

```json
{
  "id": "set-status",
  "type": "action",
  "input_mapping": {
    "task_id": "$trigger.task_id",
    "workspace_id": "$trigger.workspace_id"
  },
  "config": {
    "executor": "task.set_status",
    "params": {
      "status": "IN_REVIEW"
    }
  },
  "retry": {
    "max_attempts": 3,
    "backoff_seconds": 10
  },
  "timeout_seconds": 30
}
```

## Durable execution and recovery

Every execution stores:

- immutable workflow snapshot;
- trigger payload and variable context;
- next node IDs;
- status and optional `resume_at`;
- per-node execution attempts and outputs;
- approval records;
- ordered checkpoints.

The worker polls delayed/retry/subworkflow resumptions via `WORKFLOW_POLL_INTERVAL` (default 2 seconds). Delay and retry nodes return control to the worker rather than blocking an API process. The next worker pass reloads the persisted execution and resumes from the recorded node queue.

Execution states include `running`, `waiting_approval`, `waiting_delay`, `succeeded`, `failed`, `cancelled`, `compensating`, and `compensated`.

## Retry and compensation

Action nodes support bounded retry attempts, fixed backoff, and per-node timeout. After retry exhaustion, successful action nodes with `compensation_node_id` are compensated in reverse successful-execution order. Compensation nodes are themselves allowlisted action nodes. The final execution becomes `compensated` when all compensation steps succeed.

Manual retry can restart a failed execution from an explicitly supplied node, or from the latest failed node.

## Dry-run

`dry_run=true` keeps the orchestration path, mappings, conditions, branching, checkpoints, and observability active while action executors report intended effects rather than mutating task or incident state. Approval and delay nodes are treated as immediately successful in dry-run mode.

## API

Organization workflow control plane:

```text
GET  /api/organizations/{id}/workflows/node-schemas
GET  /api/organizations/{id}/workflows
POST /api/organizations/{id}/workflows
GET  /api/organizations/{id}/workflows/{workflow_id}

GET  /api/organizations/{id}/workflows/{workflow_id}/versions
POST /api/organizations/{id}/workflows/{workflow_id}/versions
PUT  /api/organizations/{id}/workflows/{workflow_id}/versions/{version_id}
POST /api/organizations/{id}/workflows/{workflow_id}/versions/{version_id}/publish
POST /api/organizations/{id}/workflows/{workflow_id}/versions/{version_id}/activate

POST /api/organizations/{id}/workflows/{workflow_id}/start
POST /api/organizations/{id}/workflows/trigger

GET  /api/organizations/{id}/workflow-executions
GET  /api/organizations/{id}/workflow-executions/{execution_id}
POST /api/organizations/{id}/workflow-executions/{execution_id}/cancel
POST /api/organizations/{id}/workflow-executions/{execution_id}/retry
POST /api/organizations/{id}/workflow-approvals/{approval_id}/decision
```

The execution-detail response includes the execution, node runs, approvals, and checkpoints for operational observability.

## Persistence

Migration `019_workflow_engine.sql` adds:

- `workflow_definitions`
- `workflow_versions`
- `workflow_executions`
- `workflow_node_executions`
- `workflow_approvals`
- `workflow_checkpoints`

PostgreSQL is the durable system of record; the in-memory repository keeps service/unit-test parity.

## Example graph

```json
{
  "nodes": [
    {"id":"trigger","type":"trigger","config":{"source":"event","key":"task.status_changed"}},
    {"id":"needs-review","type":"condition","config":{"variable":"$trigger.status","operator":"eq","value":"IN_REVIEW"}},
    {"id":"approve","type":"approval"},
    {"id":"notify","type":"action","config":{"executor":"workflow.noop","params":{"reason":"review approved"}}},
    {"id":"delay","type":"delay","config":{"seconds":30}},
    {"id":"end","type":"end"}
  ],
  "edges": [
    {"from":"trigger","to":"needs-review"},
    {"from":"needs-review","to":"approve","when":"true"},
    {"from":"needs-review","to":"end","when":"false"},
    {"from":"approve","to":"notify"},
    {"from":"notify","to":"delay"},
    {"from":"delay","to":"end"}
  ]
}
```

## Delivery gates

Phase 34 is complete only after formatting/vet/unit/race tests, PostgreSQL integration, migration idempotency, OpenAPI compatibility, Docker API+worker smoke, security scans, and the existing CI/security gates are green.
