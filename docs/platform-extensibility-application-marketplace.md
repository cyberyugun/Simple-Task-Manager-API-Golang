# Phase 45 — Platform Extensibility & Application Marketplace

Phase 45 completes the master roadmap with safe third-party extensibility without loading arbitrary extension code into the API process.

## Core flow

```text
APP MANIFEST
  -> PERMISSIONS
  -> PUBLISHER + APP REVIEW
  -> ORGANIZATION APPROVAL
  -> INSTALL
  -> EVENTS / SCOPED API
  -> USAGE
  -> UNINSTALL
```

## Security boundary

Extensions use the fixed execution model `remote_webhook_api`. No plugin binary, script, WASM module, dynamic library, or user-supplied code is executed inside the core API process.

An installation receives a one-time `stm_ext_...` secret. The secret is stored only as a hash. Exchange creates a short-lived service JWT restricted to:

- the installation workspace;
- the scopes approved by the organization administrator;
- the active installation lifecycle;
- the installation daily/monthly API quota.

Existing API scope guards remain authoritative. An extension token cannot access first-party-only organization administration endpoints.

## Publisher and marketplace review

Publisher records are workspace-owned and move through:

```text
draft -> submitted -> verified
                  \-> rejected
```

Applications move through:

```text
draft -> submitted -> approved
                  \-> rejected
approved -> suspended
```

Only applications from verified publishers can be approved and installed.

Marketplace metadata includes slug, version, summary, categories, homepage/privacy URLs, requested scopes, supported events, quota defaults, configuration schema and optional workflow/compliance/reporting packs.

## Installation and configuration

Organization owners/admins install an approved marketplace app into one workspace already attached to the organization. Requested scopes must be a subset of the app manifest.

Non-secret configuration is stored separately from secret references. Secret material is represented by external `secret_ref` values rather than plaintext configuration values.

Uninstall immediately marks the installation inactive, removes its extension webhook subscriptions, and causes middleware to reject any still-unexpired installation JWT.

## Events and webhooks

An installation may create webhook subscriptions only for event types declared in the approved manifest. The subscriptions reuse the durable outbox/webhook worker, URL SSRF validation and HMAC signing already used by the platform.

The webhook signing secret is returned only when a subscription is created.

## Quota and usage

Extension service-token traffic passes through an extension middleware after authentication. It resolves `client_id=extension:<installation_id>`, validates the active installation and approved marketplace app, consumes daily/monthly quota, and records response/error/latency analytics.

## Packs

Marketplace applications may carry declarative packs:

- `workflow`
- `compliance`
- `reporting`

Packs are metadata/definitions only; they do not grant executable code access.

## Main API surface

Marketplace and publisher management:

- `GET /api/marketplace/apps`
- `GET /api/marketplace/apps/{app_id}`
- `GET|POST /api/workspaces/{id}/marketplace/publishers`
- `POST /api/workspaces/{id}/marketplace/publishers/{publisher_id}/submit`
- `POST /api/workspaces/{id}/marketplace/publishers/{publisher_id}/review`
- `GET|POST /api/workspaces/{id}/marketplace/apps`
- `POST /api/workspaces/{id}/marketplace/apps/{app_id}/submit`
- `POST /api/workspaces/{id}/marketplace/apps/{app_id}/review`

Organization installations:

- `GET|POST /api/organizations/{id}/extensions/installations`
- `DELETE /api/organizations/{id}/extensions/installations/{installation_id}`
- `POST /api/organizations/{id}/extensions/installations/{installation_id}/secret/rotate`
- `GET /api/organizations/{id}/extensions/installations/{installation_id}/usage`
- `GET|POST /api/organizations/{id}/extensions/installations/{installation_id}/subscriptions`
- `DELETE /api/organizations/{id}/extensions/installations/{installation_id}/subscriptions/{subscription_id}`
- `POST /api/extensions/token`

## Migration

`030_platform_extensions.sql`

## Definition of done mapping

The implementation covers install/uninstall lifecycle, scoped API access, event webhook subscriptions, quota enforcement, organization/workspace audit plus extension lifecycle domain events, publisher verification/app review, and marketplace-ready metadata.
