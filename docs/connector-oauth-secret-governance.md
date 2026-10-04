# Phase 38 — Connector OAuth Lifecycle & Enterprise Secret Governance

Phase 38 completes the Phase 32 integration hub authentication lifecycle with OAuth 2.0 authorization-code + PKCE, refresh-token handling, credential metadata, rotation, revocation, health validation and auditable secret access.

## OAuth flow

1. Create an OAuth integration connection with provider configuration:
   - `authorize_url`
   - `token_url`
   - `client_id`
   - `redirect_uri`
   - optional `scopes`
   - optional `health_url`
   - optional `revoke_url`
2. `POST .../oauth/start` creates a 10-minute single-use state and PKCE verifier.
3. Redirect the user to the returned authorization URL.
4. After the provider callback, submit the authorization `code` and returned `state` to `POST .../oauth/callback`.
5. The API exchanges the code with `code_verifier`, validates scopes, encrypts credentials and records expiry/version metadata.
6. The worker refreshes OAuth credentials before expiry when a refresh token is available.

OAuth state is stored only as a SHA-256 digest. PKCE verifiers are encrypted at rest and sessions are single-use.

## Credential lifecycle

Credential metadata records:

- status: active, expired, revoked or error;
- granted scopes;
- expiry and last-refresh timestamps;
- logical secret backend/reference;
- credential version and key version;
- validation/rotation/revocation timestamps.

The connection APIs support:

- credential metadata inspection;
- reconnect through a new OAuth PKCE flow;
- manual envelope re-encryption/rotation;
- provider revocation when a `revoke_url` is configured;
- connection health testing;
- access-audit history.

Raw access tokens, refresh tokens and client secrets are never returned by these APIs.

## Secret-store abstraction

The database envelope backend remains the default. The backend catalog also lists AWS Secrets Manager, Azure Key Vault, GCP Secret Manager and HashiCorp Vault.

This hardening release adds a native HashiCorp Vault KV v2 adapter. Connector credential reads used by OAuth refresh, provider health checks, inbound webhook verification and outbound integration delivery are routed through the active credential backend instead of assuming the database envelope.

When a credential is rotated from the database backend to HashiCorp Vault, the service writes the credential payload to the configured KV v2 path, persists only backend/reference/version metadata, and removes the database credential copy after cutover. Subsequent connector updates and refresh-token writes continue to the Vault backend. Rotation back to the database envelope remains supported.

The backend catalog reports `configured` and `native_adapter`. AWS, Azure and GCP remain declared roadmap capabilities but cannot be selected for new rotations until an implementation is registered.

### HashiCorp Vault configuration

```text
CONNECTOR_VAULT_ADDR=https://vault.example.internal
CONNECTOR_VAULT_KV_MOUNT=secret
CONNECTOR_VAULT_PREFIX=simple-task-manager/connectors
CONNECTOR_VAULT_NAMESPACE=<optional>
CONNECTOR_VAULT_TOKEN_FILE=/var/run/secrets/vault/token
CONNECTOR_VAULT_TIMEOUT=10s
```

A direct token environment variable is also supported for deployments that do not use an injected token file. The token-file mode is preferred for Vault Agent or Kubernetes-auth workflows because the application rereads the file for each Vault request.

Vault transport is HTTPS-only unless the explicit development-only insecure HTTP flag is enabled. Redirects are rejected, response bodies are bounded, logical paths are validated, and normal connector APIs never return stored credential material.

AWS/Azure/GCP native secret/KMS adapters remain post-roadmap hardening work and should implement the same store contract with workload identity rather than provider credentials embedded in connector configuration.

## Worker

`cmd/worker` runs automatic OAuth refresh:

```text
CONNECTOR_OAUTH_REFRESH_POLL_INTERVAL=1m
CONNECTOR_OAUTH_REFRESH_BATCH_SIZE=50
```

Refresh failures transition credential metadata to `error`, or `expired` when the access token has already expired. Successful refreshes preserve refresh tokens when providers omit a replacement and increment the credential version.

## API routes

- `GET /api/integrations/secret-backends`
- `POST /api/organizations/{id}/integrations/connections/{connection_id}/oauth/start`
- `POST /api/organizations/{id}/integrations/connections/{connection_id}/oauth/callback`
- `GET /api/organizations/{id}/integrations/connections/{connection_id}/credential`
- `POST /api/organizations/{id}/integrations/connections/{connection_id}/credential/rotate`
- `POST /api/organizations/{id}/integrations/connections/{connection_id}/credential/revoke`
- `POST /api/organizations/{id}/integrations/connections/{connection_id}/test`
- `GET /api/organizations/{id}/integrations/connections/{connection_id}/credential-audit`

Organization owner/admin/delegated-admin authorization is enforced on all organization-scoped connector security operations.
