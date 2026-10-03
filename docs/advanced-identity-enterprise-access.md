# Phase 25 — Advanced Identity & Enterprise Access

Phase 25 extends the workspace tenancy model with enterprise identity controls while preserving the existing v1 username/password flow.

## Implemented core

- OAuth2 authorization-code flow with mandatory PKCE S256.
- OAuth2 client-credentials flow for workspace-scoped machine identities.
- Hashed OAuth client secrets and one-time secret disclosure.
- Workspace-scoped service API keys with explicit scopes and optional expiry.
- JWT access-token identifiers (jti), service/user token-use claims, scopes, client ID and workspace claims.
- Access-token introspection and revocation.
- Enterprise security policies per workspace with MFA and maximum-session-age enforcement.
- TOTP MFA with encrypted-at-rest secrets and MFA assurance persisted through refresh rotation.
- Standards-based WebAuthn registration/login ceremonies with one-time server ceremony state.
- Session/device risk assessment using IP, user-agent, age, and MFA assurance.
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

## MFA and WebAuthn

TOTP uses 30-second HMAC-SHA1 codes with a one-step clock-skew window. TOTP secrets are encrypted with AES-GCM before persistence. When MFA is active, password login requires a valid TOTP code or the WebAuthn MFA flow.

WebAuthn uses `github.com/go-webauthn/webauthn` for protocol verification. Registration and authentication ceremony state is stored server-side, short-lived, and single-use. Credentials are stored as complete serialized credential records so authenticator counters and flags can be updated after successful assertions.

Relying-party configuration is supplied through `WEBAUTHN_RP_ID`, `WEBAUTHN_RP_ORIGINS`, and `WEBAUTHN_RP_DISPLAY_NAME`.

## Session and device risk

`GET /api/auth/sessions/risk` compares active refresh sessions with the current request. User-agent mismatch, IP mismatch, and stale sessions increase risk; missing MFA assurance is surfaced as an explicit indicator. Workspace policy can require an MFA-authenticated token and enforce a maximum token session age.

## Organization OIDC

The OIDC connection stores issuer URL, client ID, requested scopes and an external client_secret_ref. Secrets are intentionally expected to live in a secret manager or Kubernetes Secret rather than the application database.

The connection object is the control-plane foundation for organization SSO. Provider discovery/callback cryptographic verification remains the final Phase 25 SSO item before this branch can be considered complete.

## SCIM-style provisioning

Provisioning records bind an enterprise external ID to a local user and workspace membership. Deactivation removes effective workspace membership while preserving the directory record for audit/reconciliation.

## Remaining hardening

The remaining functional gap is the organization OIDC redirect/callback exchange against the configured external IdP. The configuration, workspace policy, scopes, provisioning, MFA, and WebAuthn foundations are already implemented.
