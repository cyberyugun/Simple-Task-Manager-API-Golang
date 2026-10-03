# Phase 25 — Advanced Identity & Enterprise Access

Phase 25 extends the workspace tenancy model with enterprise identity controls while preserving the existing v1 username/password flow.

## Implemented core

- OAuth2 authorization-code flow with mandatory PKCE S256.
- OAuth2 client-credentials flow for workspace-scoped machine identities.
- Hashed OAuth client secrets and one-time secret disclosure.
- Workspace-scoped service API keys with explicit scopes and optional expiry.
- JWT access-token identifiers (jti), service/user token-use claims, scopes, client ID and workspace claims.
- Access-token introspection and revocation.
- Enterprise security policies per workspace.
- OIDC organization connection configuration using an external secret reference rather than storing an IdP secret in the database.
- SCIM-style user provisioning records linked to workspace membership.
- Enterprise identity changes are written to the existing workspace audit trail.

## OAuth2

Authorization codes are short lived and can be consumed once. PKCE uses S256 and redirect URIs are exact-match validated against the registered OAuth client.

Supported grants:

- authorization_code
- client_credentials

Machine access tokens are workspace scoped and carry the client's allowed scopes.

## API keys

API keys are generated from cryptographically random bytes. Only the SHA-256 hash is persisted. The plaintext key is returned once at creation time. Keys can be expired or revoked and carry a workspace and scope set.

## Scopes

Initial scopes:

- tasks:read
- tasks:write
- workspace:read
- workspace:admin
- audit:read
- identity:admin

Unknown scopes are rejected. Requested scopes must be a subset of the OAuth client or API-key grant.

## Token revocation and introspection

Access tokens include a random jti. Revocation stores only the jti and token expiry, allowing middleware and the introspection endpoint to reject a token until it naturally expires.

## Enterprise policy

Workspace owners/admins can configure:

- require_mfa
- allow_service_accounts
- allowed_email_domains
- max_session_age_minutes

The Phase 25 policy schema is designed so authentication and provisioning flows can enforce the same workspace policy consistently.

## Organization OIDC

The OIDC connection stores issuer URL, client ID, requested scopes and an external client_secret_ref. Secrets are intentionally expected to live in a secret manager or Kubernetes Secret rather than the application database.

The connection object is the control-plane foundation for organization SSO. Provider discovery/callback cryptographic verification is handled separately from configuration so deployments can integrate their preferred enterprise IdP and secret backend.

## SCIM-style provisioning

Provisioning records bind an enterprise external ID to a local user and workspace membership. Deactivation removes effective workspace membership while preserving the directory record for audit/reconciliation.

## Remaining hardening

WebAuthn/passkey ceremony verification and step-up MFA enforcement are intentionally isolated from the OAuth/API-key core and should use a standards-tested WebAuthn implementation rather than custom signature validation.
