# Simple Task Manager API - Golang

A REST API built with Go using a Handler -> Service -> Repository architecture.

## Features

- Registration/login with bcrypt password hashing
- Short-lived HS256 JWT access tokens
- 256-bit opaque refresh tokens stored only as SHA-256 hashes
- Refresh-token rotation and replay rejection
- Logout/revocation for refresh sessions
- Configurable access/refresh token TTLs
- Change password with refresh-session revocation
- Forgot/reset password with one-time hashed action tokens
- Email verification with one-time hashed action tokens
- Active session/device listing and per-session revocation
- Logout all devices
- Per-IP authentication rate limiting with optional Redis-backed distributed storage
- Structured JSON request/application logs
- Correlation/request IDs via `X-Request-ID`
- Liveness, dependency readiness, and Prometheus metrics endpoints
- Versioned deployment migration runner with PostgreSQL advisory locking
- Kubernetes base + production Kustomize manifests
- Rolling deployment, startup/liveness/readiness probes, HPA, PDB, and NetworkPolicy
- GHCR image publishing and production deployment workflow
- CycloneDX SBOM generation for CI and release images
- Trivy source, secret, IaC, and container vulnerability scanning
- Gitleaks secret scanning and pull-request dependency review
- Keyless Cosign image signing with GitHub OIDC
- GitHub provenance + SBOM attestations stored with release images
- Immutable SHA-pinned GitHub Actions with CI enforcement
- Dependabot updates for Go, Actions, and Docker
- External Secrets Operator and cert-manager production examples
- Build/version endpoint with commit metadata
- Per-user task ownership
- Workspace RBAC and multi-tenant task isolation
- Personal workspace fallback for backward-compatible task clients
- Owner/admin/member workspace membership controls and audit trail
- Transactional outbox for task domain events
- Dedicated background workers with retry, dead-letter, and replay
- Signed tenant-aware webhook subscriptions
- Workspace-scoped idempotency keys for task creation
- Task CRUD and complete action
- Pagination, search, filtering, sorting, and ordering
- In-memory and PostgreSQL repositories
- SQL migrations
- OpenAPI 3.1 + Swagger UI
- Multi-stage production Docker image
- Docker Compose API + PostgreSQL stack
- Graceful shutdown and HTTP server timeouts
- Environment/config validation
- Unit, handler, PostgreSQL integration, and container smoke tests
- GitHub Actions CI

## Environment

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `PORT` | No | `8080` | HTTP server port, 1-65535 |
| `DATABASE_URL` | No | empty | PostgreSQL URL; empty uses in-memory storage |
| `REDIS_URL` | No | empty | Redis URL; when set, auth rate limits are shared across API instances |
| `JWT_SECRET` | Yes | none | JWT signing secret, minimum 32 characters |
| `LOG_LEVEL` | No | `info` | `debug`, `info`, `warn`, or `error` |
| `READINESS_TIMEOUT` | No | `2s` | Timeout for dependency readiness probes |
| `ACCESS_TOKEN_TTL` | No | `15m` | Access JWT lifetime |
| `REFRESH_TOKEN_TTL` | No | `720h` | Refresh token lifetime; must exceed access TTL |
| `PASSWORD_RESET_TTL` | No | `30m` | Password reset token lifetime |
| `EMAIL_VERIFICATION_TTL` | No | `24h` | Email verification token lifetime |
| `AUTH_RATE_LIMIT_REQUESTS` | No | `20` | Auth requests allowed per client IP/window |
| `AUTH_RATE_LIMIT_WINDOW` | No | `1m` | Authentication rate-limit window |
| `RATE_LIMIT_FAIL_OPEN` | No | `true` | Allow auth requests if Redis fails after startup |
| `EXPOSE_AUTH_TOKENS` | No | `false` | Show reset/verification tokens for local/testing only |
| `SHUTDOWN_TIMEOUT` | No | `10s` | Graceful shutdown timeout |
| `DB_MAX_OPEN_CONNS` | No | `10` | Maximum PostgreSQL connections per API process |
| `DB_MAX_IDLE_CONNS` | No | `5` | Maximum idle PostgreSQL connections |
| `DB_CONN_MAX_IDLE_TIME` | No | `5m` | Maximum PostgreSQL connection idle time |
| `DB_CONN_MAX_LIFETIME` | No | `30m` | Maximum PostgreSQL connection lifetime |
| `REDIS_POOL_SIZE` | No | `20` | Redis client pool size |
| `REDIS_MIN_IDLE_CONNS` | No | `5` | Redis minimum idle connections |
| `REDIS_POOL_TIMEOUT` | No | `4s` | Redis wait timeout for a pooled connection |
| `REDIS_DIAL_TIMEOUT` | No | `5s` | Redis connection dial timeout |
| `REDIS_READ_TIMEOUT` | No | `3s` | Redis read timeout |
| `REDIS_WRITE_TIMEOUT` | No | `3s` | Redis write timeout |
| `IDEMPOTENCY_TTL` | No | `24h` | Retention window for task-create idempotency records |
| `WEBHOOK_ALLOW_INSECURE_HTTP` | No | `false` | Allow plain HTTP webhook destinations for local testing only |
| `WORKER_POLL_INTERVAL` | No | `2s` | Background worker polling interval |
| `WORKER_BATCH_SIZE` | No | `50` | Maximum outbox events claimed per worker iteration |
| `WEBHOOK_TIMEOUT` | No | `10s` | Webhook HTTP request timeout |

## Run locally

Install dependencies:

```bash
go mod tidy
```

Run with in-memory storage:

```bash
export JWT_SECRET="replace-this-with-a-random-secret-at-least-32-characters"
go run ./cmd/api
```

PowerShell:

```powershell
$env:JWT_SECRET="replace-this-with-a-random-secret-at-least-32-characters"
go run ./cmd/api
```

## Run the complete Docker stack

Copy the example environment file if you want to customize values:

```bash
cp .env.example .env
```

Set a strong local secret and start API + PostgreSQL:

```bash
export JWT_SECRET="replace-this-with-a-random-secret-at-least-32-characters"
docker compose up -d --build
```

Then:

```text
API:         http://localhost:8080
Liveness:    http://localhost:8080/health
Readiness:   http://localhost:8080/ready
Metrics:     http://localhost:8080/metrics
Version:     http://localhost:8080/version
Swagger UI:  http://localhost:8080/docs
OpenAPI:     http://localhost:8080/openapi.yaml
PostgreSQL:  localhost:5432
Redis:       localhost:6379
```

Check containers:

```bash
docker compose ps
```

View logs:

```bash
docker compose logs -f api
```

Stop the stack:

```bash
docker compose down
```

Delete the local database volume too:

```bash
docker compose down -v
```

The Compose file contains a development-only fallback JWT secret. Set `JWT_SECRET` explicitly outside local development.

## Docker image

Build directly:

```bash
docker build \
  --build-arg VERSION=dev \
  --build-arg COMMIT="$(git rev-parse HEAD)" \
  --build-arg BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -t simple-task-manager-api .
```

The Dockerfile uses Go 1.26.8 in a multi-stage build, produces stripped API, migration, event-worker, and event-replay binaries, includes SQL migrations, and runs the final container as a non-root user. Docker Compose starts PostgreSQL and Redis, runs migrations, then starts both the API and background worker.

## PostgreSQL migrations

For Docker Compose, the dedicated `migrate` service applies pending migrations before the API starts.

Run migrations manually:

```bash
DATABASE_URL="$DATABASE_URL" MIGRATIONS_DIR=migrations go run ./cmd/migrate
```

The migration runner:

- creates `schema_migrations`
- serializes concurrent migration runs with a PostgreSQL advisory lock
- applies migration files in lexical order
- records each successfully committed migration
- safely skips versions that were already applied

## API documentation

Swagger UI supports the Bearer JWT security scheme. Register/login, copy the returned access token, select **Authorize**, and call protected task endpoints from the browser.

The public contract is compatibility line `v1`. Responses advertise `X-API-Version: v1` and `API-Supported-Versions: v1`. OpenAPI governance, backward-compatibility checks, consumer expectations, generated TypeScript SDK validation, and deprecation policy are documented in [`docs/api-versioning.md`](docs/api-versioning.md). Public contract changes are tracked in [`docs/api-changelog.md`](docs/api-changelog.md), with examples in [`docs/api-examples.md`](docs/api-examples.md).

Workspace tenancy, RBAC permissions, tenant-selection headers, rollout compatibility, and audit behavior are documented in [`docs/authorization-multitenancy.md`](docs/authorization-multitenancy.md).

Transactional outbox semantics, webhook signatures, retries/dead letters, replay, SSRF controls, and task idempotency are documented in [`docs/event-driven-processing.md`](docs/event-driven-processing.md).

## Public endpoints

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/health` | Liveness health check |
| `GET` | `/ready` | PostgreSQL/Redis readiness check |
| `GET` | `/metrics` | Prometheus text metrics |
| `GET` | `/version` | Build version, commit, build time, and Go runtime version |
| `GET` | `/docs` | Swagger UI |
| `GET` | `/openapi.yaml` | OpenAPI specification |
| `POST` | `/api/auth/register` | Register |
| `POST` | `/api/auth/login` | Login |
| `POST` | `/api/auth/refresh` | Rotate refresh token and issue a new token pair |
| `POST` | `/api/auth/logout` | Revoke a refresh token |
| `POST` | `/api/auth/forgot-password` | Request password-reset instructions |
| `POST` | `/api/auth/reset-password` | Reset password with one-time token |
| `POST` | `/api/auth/email-verification/confirm` | Confirm email with one-time token |

## Refresh token flow

Register and login return both an access token and a refresh token. Access tokens default to 15 minutes; refresh tokens default to 30 days.

```json
{
  "access_token": "<jwt>",
  "refresh_token": "<opaque-token>",
  "token_type": "Bearer",
  "access_token_expires_in": 900,
  "refresh_token_expires_in": 2592000
}
```

Rotate a refresh token:

```bash
curl -X POST http://localhost:8080/api/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"<refresh_token>"}'
```

Every successful refresh invalidates the previous refresh token. Reusing an old token returns `401`.

Logout:

```bash
curl -X POST http://localhost:8080/api/auth/logout \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"<refresh_token>"}'
```

Logout revokes the refresh session. An access JWT already issued remains valid only until its short access TTL expires.

## Account security endpoints

These endpoints require a valid access JWT:

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/api/auth/change-password` | Change password and revoke all refresh sessions |
| `POST` | `/api/auth/logout-all` | Revoke all refresh sessions |
| `GET` | `/api/auth/sessions` | List active sessions/devices |
| `DELETE` | `/api/auth/sessions/{id}` | Revoke one session |
| `POST` | `/api/auth/email-verification/request` | Create email verification instructions |

Session responses expose the session ID, user agent, IP address, created time, last-used time, and expiry to the authenticated owner only. Refresh-token hashes are never returned.

### Password reset

`POST /api/auth/forgot-password` deliberately returns the same generic message whether or not an account exists, reducing email enumeration risk. Reset tokens are random opaque values; only SHA-256 hashes are stored, and tokens are single-use.

For local/CI testing only:

```bash
export EXPOSE_AUTH_TOKENS=true
```

This adds `development_token` to password-reset and email-verification request responses. Keep this disabled in production. A production deployment should deliver those tokens through an email provider.

### Email verification

Email verification is implemented as an optional account state. Current task endpoints do not require a verified email. The flow is:

```text
Authenticated verification request
        |
        v
One-time verification token
        |
        v
POST /api/auth/email-verification/confirm
        |
        v
email_verified_at is set
```

### Authentication rate limiting

Sensitive authentication routes use a fixed-window limiter keyed by the direct client IP. Without `REDIS_URL`, the limiter is process-local. With `REDIS_URL`, counters are stored atomically in Redis so limits are shared across API replicas.

The server intentionally does not trust `X-Forwarded-For` by default. If deployed behind a trusted reverse proxy, proxy-aware client IP handling should be added explicitly rather than trusting forwarded headers globally.

`RATE_LIMIT_FAIL_OPEN=true` keeps auth endpoints available if Redis fails after the API has started. The readiness probe still fails while Redis is unavailable, allowing an orchestrator to stop routing new traffic to the instance.

## Protected task endpoints

All task endpoints require:

```text
Authorization: Bearer <access_token>
```

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/api/tasks` | List current user's tasks |
| `POST` | `/api/tasks` | Create task |
| `GET` | `/api/tasks/{id}` | Get task |
| `PUT` | `/api/tasks/{id}` | Update task |
| `PATCH` | `/api/tasks/{id}/complete` | Mark task completed |
| `DELETE` | `/api/tasks/{id}` | Delete task |

## Task list query parameters

| Parameter | Default | Allowed values |
| --- | --- | --- |
| `page` | `1` | Positive integer |
| `limit` | `10` | `1` to `100` |
| `search` | empty | Title/description search |
| `completed` | empty | `true`, `false` |
| `sort` | `created_at` | `id`, `title`, `created_at`, `updated_at`, `completed` |
| `order` | `desc` | `asc`, `desc` |

## Observability and operations

Every request receives an `X-Request-ID`. A valid incoming ID is preserved; otherwise the API generates a random 128-bit ID. Structured JSON request logs include the request ID, method, path, status, response bytes, duration, and direct remote IP.

```text
GET /health   -> process liveness only
GET /ready    -> PostgreSQL + Redis readiness when configured
GET /metrics  -> Prometheus text exposition
```

The metrics endpoint exports bounded route/method/status-class request counters and latency histograms, in-flight requests, recovered panics, rate-limit rejections, readiness/dependency gauges, trace exporter failures, PostgreSQL pool statistics, and Redis pool statistics. Dynamic task/session IDs are normalized to route templates to prevent high-cardinality metrics. Production requests also propagate W3C trace context and export OTLP/HTTP traces when the collector endpoint is configured.

Panic recovery is centralized in HTTP middleware. Recovered panics return HTTP 500, increment the panic metric, and emit a structured error log with the request ID and stack trace.

## Kubernetes deployment

Production manifests are organized with Kustomize:

```text
deploy/k8s/
├── base/
│   ├── namespace.yaml
│   ├── serviceaccount.yaml
│   ├── configmap.yaml
│   ├── deployment.yaml
│   ├── worker-deployment.yaml
│   ├── service.yaml
│   ├── hpa.yaml
│   ├── pdb.yaml
│   ├── networkpolicy.yaml
│   ├── migration-job.yaml
│   ├── secret.example.yaml
│   └── kustomization.yaml
├── optional/
│   ├── external-secret.example.yaml
│   └── cert-manager-clusterissuer.example.yaml
└── overlays/
    └── production/
        ├── ingress.yaml
        ├── clusterissuer.yaml
        └── kustomization.yaml
```

The application deployment starts with three replicas and uses a rolling update with `maxUnavailable: 0`. Startup and liveness probes call `/health`; readiness calls `/ready`. Resource requests are used by the HPA, which scales from 3 to 10 replicas using CPU and memory utilization. Production pods are spread across availability zones so a single-zone failure does not concentrate all API replicas in one failure domain.

The cluster must have a Metrics Server (or another implementation of the resource metrics API) for CPU/memory HPA metrics. The production Ingress assumes an `nginx` IngressClass. The NetworkPolicy allows same-namespace traffic plus namespaces named `ingress-nginx` and `monitoring`; adjust these selectors when your cluster uses different namespace names.

Create application secrets before deployment:

```bash
kubectl apply -f deploy/k8s/base/namespace.yaml

kubectl -n task-manager create secret generic task-api-secrets \
  --from-literal=DATABASE_URL='postgres://...' \
  --from-literal=REDIS_URL='redis://...' \
  --from-literal=JWT_SECRET='replace-with-a-random-secret-at-least-32-characters'
```

The production overlay now includes the cert-manager `letsencrypt-prod` ClusterIssuer and an Ingress that requests the `task-api-tls` certificate. Install cert-manager before applying the production overlay and set `CERT_MANAGER_EMAIL` in the GitHub production Environment so the deployment workflow replaces the placeholder ACME email. If cert-manager is intentionally not used, remove `clusterissuer.yaml` and the cert-manager annotation from the overlay and create the TLS secret manually:

```bash
kubectl -n task-manager create secret tls task-api-tls \
  --cert=./tls.crt \
  --key=./tls.key
```

The Kubernetes workloads reference `ghcr-pull-secret`. If the GHCR package is private, create a long-lived pull credential:

```bash
kubectl -n task-manager create secret docker-registry ghcr-pull-secret \
  --docker-server=ghcr.io \
  --docker-username='<github-user>' \
  --docker-password='<github-pat-with-read-packages>'
```

If the GHCR image is public, Kubernetes can pull it anonymously; a missing pull secret may still produce a warning event, so removing the `imagePullSecrets` entry is cleaner for a fully public deployment.

For production secret management, `deploy/k8s/optional/external-secret.example.yaml` shows an External Secrets Operator `ExternalSecret` that materializes the existing `task-api-secrets` Secret from a provider-backed `ClusterSecretStore`. The optional cert-manager issuer example uses `cert-manager.io/v1`.

Render the production manifests without applying them:

```bash
kubectl kustomize deploy/k8s/overlays/production
```

The migration Job is intentionally separate from the base Kustomization so database migrations can complete before the application rollout. The deployment workflow deletes any previous migration Job, runs the migration image for the new commit, waits for completion, then applies the Kustomize overlay and waits for the Deployment rollout.

### GitHub production deployment

`.github/workflows/deploy.yml` builds and pushes:

```text
ghcr.io/cyberyugun/simple-task-manager-api-golang:<commit-sha>
ghcr.io/cyberyugun/simple-task-manager-api-golang:latest
```

It runs on version tags matching `v*` and can also be started with `workflow_dispatch`.

Configure a GitHub Environment named `production` with `KUBE_INGRESS_HOST`, `CERT_MANAGER_EMAIL`, and a cluster authentication mode.

```text
Variables:
  KUBE_AUTH_MODE      aws-eks | azure-aks | gke | kubeconfig
  KUBE_INGRESS_HOST   production DNS host, for example api.example.com
  CERT_MANAGER_EMAIL  ACME account email used by cert-manager

AWS EKS OIDC:
  Secret:   AWS_ROLE_ARN
  Variables: AWS_REGION, EKS_CLUSTER_NAME

Azure AKS OIDC:
  Secrets:  AZURE_CLIENT_ID, AZURE_TENANT_ID, AZURE_SUBSCRIPTION_ID
  Variables: AZURE_RESOURCE_GROUP, AKS_CLUSTER_NAME

Google GKE OIDC:
  Secrets:  GCP_WORKLOAD_IDENTITY_PROVIDER, GCP_SERVICE_ACCOUNT
  Variables: GCP_PROJECT_ID, GKE_CLUSTER_NAME, GKE_LOCATION

Provider-neutral fallback:
  Secret:   KUBE_CONFIG_B64
  Variable: KUBE_AUTH_MODE=kubeconfig
```

The workflow builds the release, blocks HIGH/CRITICAL image vulnerabilities with available fixes, generates a CycloneDX SBOM, creates GitHub build-provenance and SBOM attestations, signs the image keylessly with Cosign using GitHub OIDC, verifies that signature, and deploys the immutable image digest. Before migration and rollout, the deploy job verifies the signature again. Finally, it port-forwards the Service and checks that `GET /version` returns the exact `GITHUB_SHA` embedded in the image.

The deployment workflow natively supports GitHub OIDC for AWS EKS, Azure AKS, and Google GKE. These modes exchange GitHub's short-lived OIDC token for provider credentials and avoid storing a long-lived kubeconfig in GitHub. `KUBE_CONFIG_B64` remains an explicit provider-neutral fallback for clusters that cannot use workload identity federation.

Before the first production rollout, follow the full readiness runbook in [`docs/production-readiness.md`](docs/production-readiness.md). The deployment workflow runs the same cluster preflight automatically before migrations or rollout.

Production infrastructure can be provisioned from the Terraform stacks under `infra/terraform/`. See [`docs/infrastructure-as-code.md`](docs/infrastructure-as-code.md) for AWS, Azure, GCP, remote-state, private-runner, and platform bootstrap guidance.

Terraform plan/apply promotion, encrypted plan artifacts, remote-state bootstrap, OIDC-only infrastructure auth, and drift detection are documented in [`docs/terraform-delivery.md`](docs/terraform-delivery.md).

Production metrics, traces, logs, SLOs, synthetic probes, restore drills, and incident response are documented in [`docs/observability-sre.md`](docs/observability-sre.md) and [`docs/sre-incident-runbook.md`](docs/sre-incident-runbook.md).

High availability, RPO/RTO targets, regional rebuild/restore procedures, automated dependency-failure exercises, and DR game-day acceptance criteria are documented in [`docs/disaster-recovery.md`](docs/disaster-recovery.md) and [`docs/dr-game-day-checklist.md`](docs/dr-game-day-checklist.md).

Load/spike/soak testing, connection-pool tuning, query-plan validation, HPA capacity guidance, and performance regression policy are documented in [`docs/performance-scalability.md`](docs/performance-scalability.md).

Progressive canary delivery, deployment freezes, expand/contract migration safety, automatic image rollback, immutable release evidence, and release/rollback procedures are documented in [`docs/release-engineering.md`](docs/release-engineering.md) and [`docs/production-release-checklist.md`](docs/production-release-checklist.md).

Mandatory staging validation, isolated staging PostgreSQL/Redis guidance, exact-digest promotion evidence, and the staging→production gate are documented in [`docs/staging-promotion.md`](docs/staging-promotion.md).

## Security and software supply chain

Security automation lives in `.github/workflows/security.yml` and runs on pushes to `main`, pull requests, manual dispatches, and a weekly schedule.

```text
Source
  |
  +-- Gitleaks secret scan
  +-- Dependency Review on PRs
  +-- Trivy dependency + secret scan
  +-- Trivy Kubernetes/Docker misconfiguration scan
  |
  v
Container build
  |
  +-- Trivy HIGH/CRITICAL image scan
  +-- CycloneDX SBOM
  |
  v
Release
  |
  +-- GitHub build provenance attestation
  +-- GitHub SBOM attestation
  +-- Cosign keyless OIDC signature
  +-- Cosign verification
  |
  v
Kubernetes deployment by sha256 digest
```

All external GitHub Actions are pinned to full 40-character commit SHAs. CI scans every workflow and fails if a future `uses:` entry is added with a mutable tag such as `@v4`. Version comments are retained beside the SHA so Dependabot can keep those pins current.

Dependabot is configured for weekly Go module, GitHub Actions, and Docker base-image updates. Security reporting guidance is in `SECURITY.md`, and `.github/CODEOWNERS` marks workflows, Kubernetes manifests, dependency files, and the Dockerfile as security-sensitive.

A signed release image can be verified with Cosign using the GitHub Actions identity:

```bash
cosign verify ghcr.io/cyberyugun/simple-task-manager-api-golang@sha256:<digest> \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/cyberyugun/Simple-Task-Manager-API-Golang/.github/workflows/deploy.yml@refs/(heads/main|tags/v.*)
`GET /version` reports the image build identity:

```json
{
  "success": true,
  "data": {
    "version": "v1.2.0",
    "commit": "<git-sha>",
    "build_time": "2026-10-03T00:00:00Z",
    "go_version": "go1.26.x"
  }
}
```

The production image sets these values through Docker build arguments and Go linker flags, so a running pod can be tied back to an immutable source commit.

## Tests

Unit tests:

```bash
go test ./...
```

PostgreSQL integration tests:

```bash
TEST_DATABASE_URL="postgres://task_user:task_password@localhost:5432/task_manager_test?sslmode=disable"   go test -tags=integration ./internal/repository -run Integration -v
```

## CI pipeline

The main CI workflow validates unit/build quality, immutable GitHub Action pins, PostgreSQL behavior, Kubernetes manifests, optional security CRD examples, and the full Docker Compose stack. A separate Security workflow performs Gitleaks, Dependency Review, Trivy source/IaC/image scans, and SBOM generation. Kubernetes validation renders the production Kustomize overlay and parses the rendered resources plus migration and Secret templates client-side.

It validates formatting, `go vet`, race-enabled unit tests, both binaries, migration-runner idempotency, real PostgreSQL repository behavior, refresh-token rotation/replay protection, one-time reset/verification tokens, session revocation, Redis-backed rate limiting, readiness/metrics/version endpoints, structured request logs, Kubernetes manifests, Docker image construction, OpenAPI, and Swagger UI.

## Graceful shutdown

The API handles `SIGINT` and `SIGTERM`. On shutdown it stops accepting new requests and gives active requests up to `SHUTDOWN_TIMEOUT` to finish before forcing the server closed.


## Existing databases

Use the migration runner for existing databases instead of applying individual files manually:

```bash
DATABASE_URL="$DATABASE_URL" MIGRATIONS_DIR=migrations go run ./cmd/migrate
```

Docker Compose uses the same runner automatically before starting the API.

```

## Build metadata

`GET /version` reports the image build identity:

```json
{
  "success": true,
  "data": {
    "version": "v1.2.0",
    "commit": "<git-sha>",
    "build_time": "2026-10-03T00:00:00Z",
    "go_version": "go1.26.x"
  }
}
```

The production image sets these values through Docker build arguments and Go linker flags, so a running pod can be tied back to an immutable source commit.

## Tests

Unit tests:

```bash
go test ./...
```

PostgreSQL integration tests:

```bash
TEST_DATABASE_URL="postgres://task_user:task_password@localhost:5432/task_manager_test?sslmode=disable"   go test -tags=integration ./internal/repository -run Integration -v
```

## CI pipeline

The main CI workflow validates unit/build quality, PostgreSQL behavior, Kubernetes manifests, and the full Docker Compose stack. Kubernetes validation renders the production Kustomize overlay and parses the rendered resources plus migration and Secret templates client-side.

It validates formatting, `go vet`, race-enabled unit tests, both binaries, migration-runner idempotency, real PostgreSQL repository behavior, refresh-token rotation/replay protection, one-time reset/verification tokens, session revocation, Redis-backed rate limiting, readiness/metrics/version endpoints, structured request logs, Kubernetes manifests, Docker image construction, OpenAPI, and Swagger UI.

## Graceful shutdown

The API handles `SIGINT` and `SIGTERM`. On shutdown it stops accepting new requests and gives active requests up to `SHUTDOWN_TIMEOUT` to finish before forcing the server closed.


## Existing databases

Use the migration runner for existing databases instead of applying individual files manually:

```bash
DATABASE_URL="$DATABASE_URL" MIGRATIONS_DIR=migrations go run ./cmd/migrate
```

Docker Compose uses the same runner automatically before starting the API.
