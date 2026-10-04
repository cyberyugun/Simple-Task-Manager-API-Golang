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

The API exposes backend capabilities for:

- database envelope encryption;
- AWS Secrets Manager;
- Azure Key Vault;
- GCP Secret Manager;
- HashiCorp Vault.

The current runtime persists the encrypted credential envelope through the integration repository while recording the selected backend reference and key version. This is the provider-neutral governance boundary for plugging in native cloud-vault clients without changing connector business logic. Deployments that require native cloud persistence should bind the backend adapter to their cloud identity/KMS implementation rather than putting vault credentials in connector configuration.

This phase therefore establishes the pluggable vault contract, envelope/key-version metadata and BYOK foundation; it does not embed cloud-provider long-lived credentials in source code.

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
