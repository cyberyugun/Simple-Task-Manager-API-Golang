# Phase 43 — Zero Trust & Advanced Security

Phase 43 implements the roadmap flow:

```text
IDENTITY / DEVICE / NETWORK SIGNAL
    -> RISK ENGINE
    -> POLICY
    -> ALLOW / STEP-UP / REVOKE
```

## Scope

- Service/workload identity with SPIFFE-style identities.
- X.509 certificate registration and rotation.
- mTLS-bound workload token issuance.
- Certificate-thumbprint token binding enforced by authentication middleware.
- Device trust records and session-risk scoring.
- Impossible-travel detection from optional geolocation observations.
- Adaptive allow / step-up / revoke decisions.
- Automatic high-risk refresh-session revocation.
- Network allow/deny CIDR controls and WAF risk-score integration.
- Tenant security dashboard.
- Tamper-evident SHA-256 hash-chained security audit checkpoints signed with HMAC.
- Immutable WORM export manifests.
- SIEM destination metadata and incremental security-event federation feed.

## mTLS model

The workload token endpoint requires a TLS peer certificate presented to the Go process. The certificate must be registered to an active workload identity, must be valid at request time, and its URI SAN must match the workload SPIFFE ID. The issued access token is bound to the certificate SHA-256 fingerprint. Subsequent service-token requests must present the same certificate.

A TLS-terminating proxy must not downgrade this guarantee. If mTLS terminates before the application, the proxy should re-establish authenticated TLS to the API rather than forwarding an untrusted fingerprint header.

## Risk model

Signals are additive and capped at 100. Policy thresholds determine whether a request is allowed, requires step-up authentication, or causes session revocation. MFA-authenticated sessions can satisfy a step-up decision, while critical network/token-binding violations remain revocation candidates.

## Audit chain

Each checkpoint hashes the ordered security-event payload since the prior checkpoint, then hashes that payload hash with the previous checkpoint hash, organization ID, and sequence. A keyed HMAC signature covers the checkpoint hash. This detects event mutation, deletion inside a checkpointed range, and checkpoint-chain modification.

## APIs

- `GET|PUT /api/organizations/{id}/security/policy`
- `GET|POST /api/organizations/{id}/security/workloads`
- `DELETE /api/organizations/{id}/security/workloads/{workload_id}`
- `GET|POST /api/organizations/{id}/security/workloads/{workload_id}/certificates`
- `POST /api/security/workload/token` (mTLS)
- `GET|POST /api/organizations/{id}/security/devices`
- `DELETE /api/organizations/{id}/security/devices/{device_id}`
- `POST /api/organizations/{id}/security/risk/evaluate`
- `GET /api/organizations/{id}/security/events`
- `GET /api/organizations/{id}/security/dashboard`
- `GET|POST /api/organizations/{id}/security/checkpoints`
- `POST /api/organizations/{id}/security/worm-exports`
- `GET|POST /api/organizations/{id}/security/siem`
- `GET /api/organizations/{id}/security/siem/feed`

## Definition of done mapping

The implementation supplies risk-based controls, workload mTLS and certificate rotation, signed audit checkpoints, and security regression/integration/runtime smoke coverage required by the Phase 43 roadmap.
