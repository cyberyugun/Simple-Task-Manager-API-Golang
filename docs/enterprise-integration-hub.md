# Phase 32 — Enterprise Integration Hub & Workflow Federation

Phase 32 introduces organization-scoped external-system connections and reliable event federation.

## Connector catalog

Built-in connector capability metadata is available for:

- Slack
- Microsoft Teams
- Jira
- GitHub
- Generic Webhook

The hub is provider-neutral. Connections specify an auth type, non-secret configuration, and encrypted credential payload. OAuth-capable providers are represented by the same connection abstraction so provider-specific authorization flows can be added without changing delivery persistence.

## Credential vault

Credential JSON is encrypted with AES-GCM using a key derived from the server JWT secret and a domain-separation prefix. Random nonces are generated per encryption. Credentials are only decrypted inside the service during signature verification or outbound dispatch and are never returned by the REST connection models.

Supported credential shapes include:

- `access_token` / bearer token
- `signing_secret` for HMAC federation

## Outbound federation

Administrators queue an event against a connection. The dispatcher persists:

- Unique event key
- Event type and JSON payload
- Attempt count and maximum attempts
- Availability/backoff timestamp
- Worker lock
- Last HTTP status/error
- Delivered or dead-letter timestamp

Workers claim jobs with PostgreSQL `FOR UPDATE SKIP LOCKED`, deliver over HTTP, and use exponential retry. Stale worker locks are released automatically.

## Inbound federation

`POST /api/integrations/inbound/{connection_id}` is intentionally unauthenticated by user JWT. Instead, the exact request body must carry:

`X-Integration-Signature: sha256=<HMAC-SHA256>`

using that connection's encrypted `signing_secret`.

The envelope contains:

- `event_id`
- `event_type`
- `payload`

A unique constraint on connection + provider event ID makes inbound processing idempotent. Duplicate events return success without creating another record.

## Health and rate limits

Successful outbound/inbound traffic marks a connection healthy. Failed outbound attempts mark it degraded and increment consecutive failures. Standard `X-RateLimit-Remaining` and Unix `X-RateLimit-Reset` response headers are captured when present.

## Security controls

- HTTP targets require HTTPS unless `WEBHOOK_ALLOW_INSECURE_HTTP=true`; the insecure setting is intended for local/test environments.
- Credentials are never exposed in API responses.
- Inbound payloads are capped at 1 MiB.
- Integration mutations require organization owner/admin/delegated-admin access.
- Connection create/update, queue, and replay operations write organization audit events.

## Worker settings

- `INTEGRATION_POLL_INTERVAL=2s` by default
- `INTEGRATION_BATCH_SIZE=25` by default

## APIs

- `GET /api/integrations/connectors`
- `POST /api/integrations/inbound/{connection_id}`
- `GET|POST /api/organizations/{id}/integrations/connections`
- `PUT /api/organizations/{id}/integrations/connections/{connection_id}`
- `GET|POST /api/organizations/{id}/integrations/deliveries`
- `POST /api/organizations/{id}/integrations/deliveries/{delivery_id}/replay`
- `GET /api/organizations/{id}/integrations/inbound-events`
