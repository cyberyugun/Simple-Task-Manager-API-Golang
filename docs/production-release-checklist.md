# Production Release Checklist

## Before release

- [ ] CI is green
- [ ] Security workflow is green
- [ ] image digest is signed and attested
- [ ] migration compatibility gate passes
- [ ] migration uses expand/contract semantics
- [ ] production freeze status reviewed
- [ ] canary weight reviewed
- [ ] canary observation window reviewed
- [ ] production Environment approval completed
- [ ] incident responder available for material releases

## Canary

- [ ] migration completed
- [ ] canary Deployment Ready
- [ ] direct canary health passes
- [ ] direct canary readiness passes
- [ ] direct canary version matches release commit
- [ ] public forced-canary HTTPS route passes
- [ ] canary remains healthy for observation window

## Promotion

- [ ] stable rollout completes
- [ ] unavailable replicas = 0
- [ ] available replicas >= desired replicas
- [ ] public stable version reports release commit
- [ ] metrics endpoint exposes expected metrics
- [ ] observability resources remain present
- [ ] canary resources removed
- [ ] `:latest` moved only after promotion
- [ ] release evidence artifact uploaded
- [ ] `release-attestation-<commit>` artifact uploaded after successful telemetry verification
- [ ] `release-attestation.json` verifies against the production commit, image digest, workflow run, and component hashes
- [ ] provider registry cells include `evidence_sha256` for selected live-provider evidence when the provider gate is active
- [ ] release attestation root digest recorded in the change/release record

## If release fails

- [ ] canary resources removed
- [ ] previous stable image restored when promotion had started
- [ ] rollback rollout reaches Ready
- [ ] database compatibility reviewed before any manual DB intervention
- [ ] failed release evidence preserved
- [ ] incident/release notes record cause and corrective action
