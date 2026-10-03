# API Changelog

## 1.3.0 — Phase 22

Compatibility line: **v1**.

Non-breaking governance release:

- declares the existing public API as compatibility line v1
- adds `X-API-Version: v1` and `API-Supported-Versions: v1` runtime headers
- documents existing `429 Too Many Requests` behavior for register, login, refresh, and logout
- adds backward-compatibility diff checks
- adds consumer-driven contract expectations
- adds live provider/schema contract validation
- adds generated TypeScript SDK artifacts
- adds formal versioning and deprecation policy

No existing endpoint, request property, response property, operation ID, or authentication requirement was removed or incompatibly changed.

## 1.2.0

Pre-Phase-22 OpenAPI contract for system, authentication, session/account security, and task-management APIs.
