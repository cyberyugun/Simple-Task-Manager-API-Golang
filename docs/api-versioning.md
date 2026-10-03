# API Contract Governance and Versioning

`internal/apidocs/openapi.yaml` is the governed public API contract.

## Compatibility line

- API compatibility version: `v1`
- OpenAPI document version: `1.3.0`
- Runtime header: `X-API-Version: v1`
- Supported versions header: `API-Supported-Versions: v1`

The existing `/api/...` routes remain unchanged. `v1` identifies the compatibility line rather than introducing a new URL prefix.

An incompatible future API must preserve v1 while adding a separately addressable v2 surface. Breaking v1 in place is not accepted.

## Contract gates

The `API Contract` workflow enforces:

- OpenAPI 3.1 structure and internal `$ref` integrity
- unique, stable `operationId` values
- route parity with the Go HTTP router
- bearer security declarations for protected endpoints
- consumer expectations from `tests/contracts/consumer-v1.yaml`
- backward-compatible diff against the PR base / previous main commit
- TypeScript SDK generation
- live provider responses validated against OpenAPI schemas

## Breaking changes

The compatibility gate rejects removal of paths, operations, response statuses, parameters, schemas, or response properties; operation ID changes; authentication becoming newly required; newly required parameters/request bodies; incompatible type changes; enum value removal; and newly required fields on existing object schemas.

Additive endpoints, optional fields, optional parameters, examples, and documentation are normally compatible, but still pass through all contract gates.

## Deprecation

Do not remove a v1 operation directly. Mark it with:

```yaml
deprecated: true
x-sunset-date: "2027-03-31"
x-replacement: "replacementOperationId"
```

The governance gate requires both extension fields for deprecated operations. Use a minimum 90-day deprecation window unless an urgent security issue requires faster action. Runtime endpoints should also emit appropriate deprecation/sunset headers before final removal.

## Consumer-driven contract

`tests/contracts/consumer-v1.yaml` records the minimum operations, success statuses, and response fields required by the known consumer. This is intentionally smaller than the complete OpenAPI surface: OpenAPI protects the provider contract, while the consumer contract protects concrete consumer dependencies.

## Generated TypeScript SDK

The API Contract workflow generates `task-manager-api.ts` as an artifact. It contains schema interfaces, API version metadata, an operation ID/method/path registry, and a minimal fetch-based client.

Local generation:

```bash
ruby scripts/generate-typescript-sdk.rb internal/apidocs/openapi.yaml /tmp/task-manager-api.ts
```

## Review rule

Implementation and contract changes belong in the same PR. Keep operation IDs stable, preserve old request compatibility, preserve documented response fields/statuses, update `docs/api-changelog.md`, and use deprecation before removal.
