# Phase 40 — Developer Platform & Public API Management

Phase 40 exposes the existing v1 platform contract to governed third-party applications and internal integration teams.

## Core flow

```text
DEVELOPER
  -> APPLICATION
  -> REVIEW / APPROVAL
  -> OAUTH CLIENT OR API KEY
  -> SCOPES + QUOTA
  -> SANDBOX OR PRODUCTION API
  -> USAGE ANALYTICS / WEBHOOK CONSOLE
```

The implementation reuses the Phase 25 enterprise OAuth and service API-key runtime rather than introducing a second authentication stack. Developer credentials are linked to the underlying OAuth client ID or service API-key ID so issued access tokens can be resolved back to one developer application.

## Application lifecycle

Applications are workspace-scoped and move through:

```text
draft -> submitted -> approved
                  \-> rejected
approved -> suspended
```

Production credentials can only be created for an approved app. Sandbox credentials can be created earlier when the app sandbox is enabled, which lets an integration team validate authentication, scope and quota behavior before approval.

Applications define:

- allowed scopes;
- daily API request quota;
- monthly API request quota;
- sandbox enablement;
- review actor/note/timestamps.

Lifecycle transitions and credential actions are written to the existing workspace audit stream.

## OAuth clients and API keys

Two credential kinds are supported:

- `oauth_client`: backed by the existing OAuth client implementation and authorization-code/client-credentials token endpoint;
- `api_key`: backed by the existing service API-key implementation and API-key exchange endpoint.

Only a one-time secret is returned at creation/rotation. Developer-platform storage retains the external credential identifier, prefix and scopes, not the secret itself.

OAuth authorization-code access tokens now retain the OAuth `client_id` in JWT claims so the same app quota/governance path applies to user-delegated OAuth traffic as it does to client-credentials and API-key traffic.

Credential rotation creates a replacement credential first, revokes the underlying old credential, and marks the developer credential as revoked.

## Scope and quota enforcement

Developer app traffic passes through a quota/governance middleware after authentication.

The middleware:

1. resolves `client_id` back to a developer app credential;
2. leaves legacy/non-developer enterprise credentials unchanged;
3. validates workspace and app state;
4. enforces sandbox isolation;
5. atomically consumes daily/monthly quota;
6. records response status and latency for analytics.

PostgreSQL serializes quota consumption on the application row before incrementing daily usage. The API returns HTTP 429 with `Retry-After` when quota is exhausted.

## Sandbox

Sandbox credentials are explicitly isolated from normal workspace API routes. They may call only:

- `GET /api/developer/sandbox`
- `POST /api/developer/sandbox`
- `GET /api/developer/sandbox/echo`
- `POST /api/developer/sandbox/echo`

The sandbox echo contract validates authentication, scopes, developer credential resolution, quota accounting and request shape without mutating production task data.

## API analytics

Per-app daily aggregates store:

- requests;
- errors;
- total response latency;
- last request time.

The analytics API returns the current daily/monthly quota usage plus total request/error counts and average latency.

## Webhook developer console

The app webhook test console supports:

- safe URL validation using the existing SSRF protections;
- dry-run validation without transmitting data;
- optional real HTTP POST test delivery;
- event type and payload preview;
- response status/preview/error history.

Dry-run is the default.

## Developer documentation search

`GET /api/developer/docs/search?q=...` searches the embedded OpenAPI contract by path, method and summary. This keeps portal discovery tied to the same governed contract used by API compatibility checks.

## Generated SDKs

The repository contains deterministic generated SDK baselines for:

- TypeScript;
- Go;
- Python;
- Java;
- C#.

Run:

```bash
bash scripts/generate-sdks.sh
```

CI regenerates the SDK outputs from the public OpenAPI contract and fails on drift. The current generated clients are intentionally small transport foundations rather than hand-maintained per-operation wrappers; the OpenAPI contract remains the source of truth.

## API surface

Developer management:

- `GET|POST /api/workspaces/{id}/developer/apps`
- `GET /api/workspaces/{id}/developer/apps/{app_id}`
- `POST /api/workspaces/{id}/developer/apps/{app_id}/submit`
- `POST /api/workspaces/{id}/developer/apps/{app_id}/review`
- `GET|POST /api/workspaces/{id}/developer/apps/{app_id}/credentials`
- `DELETE /api/workspaces/{id}/developer/apps/{app_id}/credentials/{credential_id}`
- `POST /api/workspaces/{id}/developer/apps/{app_id}/credentials/{credential_id}/rotate`
- `GET /api/workspaces/{id}/developer/apps/{app_id}/analytics`
- `GET|POST /api/workspaces/{id}/developer/apps/{app_id}/webhook-tests`

Portal/runtime:

- `GET /api/developer/docs/search`
- `GET /api/developer/sdks`
- `GET|POST /api/developer/sandbox`
- `GET|POST /api/developer/sandbox/echo`

Existing OAuth/API-key token exchange endpoints remain backward compatible.

## Delivery guarantees and boundaries

The Phase 40 implementation provides the roadmap's developer application lifecycle, scoped credentials, quotas, sandbox contract, developer-console testing, API analytics, credential rotation and SDK generation gate.

Generated SDKs are baseline transport clients driven by the current OpenAPI contract. They are not separate handwritten feature implementations and do not replace OpenAPI as the canonical public API definition.
