# Phase 41 — AI-Assisted Task & Workflow Intelligence

Phase 41 adds optional AI assistance behind organization policy, data-classification routing, redaction, cost controls, structured outputs, audit metadata and explicit human approval for AI-originated mutations.

## Core flow

```text
REQUEST
  -> ORGANIZATION AI POLICY
  -> CLASSIFICATION / PROVIDER ROUTING
  -> REDACTION
  -> STRUCTURED PROVIDER
  -> AUDIT + COST ACCOUNTING
  -> OPTIONAL HUMAN APPROVAL
  -> ALLOWLISTED ACTION
```

AI is disabled by default for every organization. An administrator must explicitly enable it through the AI policy API.

## Assistance features

The runtime supports:

- task summary;
- description improvement;
- suggested subtasks / task decomposition;
- priority suggestions;
- duplicate suggestions;
- semantic task search;
- project summaries;
- incident summaries;
- risk and workflow suggestions;
- natural-language task portfolio reports.

The bundled `local_rules` provider is deterministic and returns structured JSON. It allows CI/runtime validation without sending data to a remote provider. Post-roadmap hardening also adds an opt-in `remote_structured` provider for operator-managed AI gateways. `AIProviderRegistry` remains the provider-neutral extension boundary.

## Governance and privacy

Each request carries a classification using the existing governance classes:

- `public`;
- `internal`;
- `confidential`;
- `restricted`.

Policy controls which classifications may use AI. Provider metadata declares supported classifications. External providers are additionally constrained by `external_max_classification`, so a deployment can prevent confidential or restricted data from leaving the platform even if an external provider is registered.

When redaction is enabled, email addresses, phone-like values, bearer credentials and common secret/token/password assignments are replaced before provider invocation. For external providers the same redaction is applied recursively to structured context values such as task titles, descriptions, incident summaries and task collections, not only to the primary text prompt. Request persistence stores a SHA-256 input hash, redaction count, context-key metadata, provider/model metadata and structured result. Raw prompt text is not stored in `ai_requests`.

## Human approval and actions

AI-generated task mutations never execute directly. Description improvements and priority suggestions produce an action proposal that enters `pending_approval`.

The approval endpoint is admin-gated. Supported allowlisted actions are:

- `task.update_description`;
- `task.update_priority`;
- `task.archive`.

Archive is treated as destructive/reversible and still requires explicit approval. Unknown destructive actions such as hard delete or arbitrary workflow execution are not executed by the AI layer.

This preserves the roadmap invariant that destructive actions remain behind policy and human approval rather than giving providers direct write access.

## Cost budgets

Each organization policy may define `monthly_budget_cents`. Before a provider call, the service checks current month spend plus estimated request cost. A request that would exceed the budget is rejected before provider execution.

A zero budget means no monetary ceiling is configured; AI must still be explicitly enabled.

The usage API returns request count, input/output units, spend, remaining budget and utilization for a calendar month.

## Evaluation datasets and quality monitoring

Administrators can create evaluation cases containing:

- feature;
- representative input;
- classification;
- expected keywords;
- metadata.

Running an evaluation invokes the configured provider through the same classification/redaction/budget guard path and stores a score in basis points. The quality endpoint exposes pass rate, average score and recent runs.

## Semantic search

The bundled provider includes a local semantic-relevance baseline based on normalized concepts and a small synonym map. It is intentionally deterministic and does not claim embedding-level quality. Deployments can later replace or augment it through the provider abstraction.

## API

Organization-scoped endpoints:

- `GET /api/organizations/{id}/ai/providers`
- `GET|PUT /api/organizations/{id}/ai/policy`
- `POST /api/organizations/{id}/ai/assist`
- `GET /api/organizations/{id}/ai/requests`
- `POST /api/organizations/{id}/ai/requests/{request_id}/decision`
- `GET /api/organizations/{id}/ai/usage?month=YYYY-MM`
- `POST /api/organizations/{id}/ai/semantic-search`
- `GET|POST /api/organizations/{id}/ai/evaluation-cases`
- `POST /api/organizations/{id}/ai/evaluation-cases/{case_id}/run`
- `GET /api/organizations/{id}/ai/quality`

## Remote structured provider

Post-roadmap hardening adds an opt-in `remote_structured` provider for production AI gateways. It deliberately uses a small provider-neutral HTTPS contract instead of embedding a vendor SDK, so organizations can place their preferred model/vendor behind an internal gateway while preserving the platform's governance boundary.

The provider sends:

- deterministic `request_id` plus the same value in `Idempotency-Key` for safe retry deduplication;
- configured model, feature and classification;
- redacted input and redacted structured context;
- `max_output_units` as an operator-enforced output budget.

The gateway must return structured JSON with `request_id`, `model`, `result`, and usage counters `input_units` / `output_units`. Results are validated per feature before they are accepted. Invalid priority values, missing required fields, malformed JSON, oversized responses and mismatched request IDs fail closed.

HTTP 408, 429 and 5xx responses are retried with bounded backoff. Redirects are refused and HTTPS is required unless insecure HTTP is explicitly enabled for local development.

Estimated and actual costs are calculated inside the application from operator-configured cents-per-1,000-unit rates rather than trusting cost values supplied by the remote gateway. Configure those rates to match the chosen provider's billing model.

Configuration:

```text
AI_REMOTE_PROVIDER_ENABLED=true
AI_REMOTE_PROVIDER_ENDPOINT=https://ai-gateway.example.com/v1/generate
AI_REMOTE_PROVIDER_TOKEN=<secret-injected-token>
AI_REMOTE_PROVIDER_MODEL=structured-task-v1
AI_REMOTE_PROVIDER_SUPPORTED_CLASSIFICATIONS=public,internal
AI_REMOTE_PROVIDER_TIMEOUT=30s
AI_REMOTE_PROVIDER_RETRY_ATTEMPTS=3
AI_REMOTE_PROVIDER_RETRY_BACKOFF=300ms
AI_REMOTE_PROVIDER_MAX_REQUEST_BYTES=262144
AI_REMOTE_PROVIDER_MAX_RESPONSE_BYTES=1048576
AI_REMOTE_PROVIDER_MAX_OUTPUT_UNITS=2048
AI_REMOTE_PROVIDER_INPUT_COST_CENTS_PER_1K_UNITS=1
AI_REMOTE_PROVIDER_OUTPUT_COST_CENTS_PER_1K_UNITS=2
```

AI remains disabled by default at the organization-policy layer, and `local_rules` remains available for deterministic/offline operation. External routing is still constrained by both the provider's declared supported classifications and each organization's `external_max_classification`.
