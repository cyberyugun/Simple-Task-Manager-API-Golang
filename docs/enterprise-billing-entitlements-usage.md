# Phase 29 — Enterprise Billing, Subscription, Entitlements & Usage Metering

Phase 29 adds provider-neutral organization billing without coupling the application core to Stripe, Xendit, Midtrans, or another payment provider.

## Plan catalog

Migration 014 seeds three plans:

| Plan | Members | Workspaces | Monthly API operations |
| --- | ---: | ---: | ---: |
| free | 100 | 20 | 10,000 |
| pro | 1,000 | 100 | 1,000,000 |
| enterprise | 100,000 | 10,000 | 1,000,000,000 |

Each plan also carries feature entitlements such as SSO, advanced governance, audit export, and priority support.

## Subscription lifecycle

Organization owners can subscribe, upgrade, downgrade, cancel at period end, or cancel immediately. A downgrade is rejected when existing member/workspace utilization is already above the target plan.

When no subscription exists, when a subscription is canceled, or when a past-due grace period expires, effective entitlement resolution falls back to the free plan.

## Phase 28 quota integration

Organization quotas remain valid as merchant/tenant-specific caps. The runtime uses the stricter value:

`effective_limit = min(organization_limit, plan_limit)`

This applies to bulk members, invitations, invitation acceptance, workspace attachment, quota updates, and organization capacity dashboards.

## Usage metering

Current-period usage is stored as additive counters keyed by organization, metric, and UTC month. Initial supported metrics are:

- `api_operations`
- `automation_runs`
- `storage_bytes`

The model is intentionally generic so producers can later meter usage directly from application events or asynchronous workers.

## Invoices

A plan change creates a period invoice record. Free invoices are immediately paid at zero value; paid plans create open invoices that can later be marked paid by a provider event.

## Provider webhooks

`POST /api/billing/webhooks/{provider}` uses:

- `X-Billing-Event-ID` for idempotency.
- `X-Billing-Signature` containing the hex HMAC-SHA256 of the raw request body.
- `BILLING_WEBHOOK_SECRET` as the shared signing secret.

Persisted provider event IDs are unique per provider. Duplicate delivery returns success without repeating state changes. Supported state updates include subscription active/trialing/past-due/canceled and invoice paid.

## Administration APIs

- `GET /api/billing/plans`
- `GET|PUT /api/organizations/{id}/billing/subscription`
- `POST /api/organizations/{id}/billing/subscription/cancel`
- `GET /api/organizations/{id}/billing/entitlements`
- `GET|POST /api/organizations/{id}/billing/usage`
- `GET /api/organizations/{id}/billing/invoices`
- `GET /api/organizations/{id}/billing/dashboard`
- `POST /api/billing/webhooks/{provider}`

Subscription changes and cancellation require the organization owner. Billing read/usage administration is available to organization owner/admin/delegated-admin roles.
