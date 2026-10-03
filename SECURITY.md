# Security Policy

## Supported version

Security fixes are applied to the latest code on the `main` branch and the newest published release.

## Reporting a vulnerability

Please do not open a public issue for an unpatched vulnerability.

Use GitHub's private vulnerability reporting flow from the repository **Security** tab when it is enabled. Include:

- affected endpoint, component, dependency, or deployment resource
- reproduction steps or a minimal proof of concept
- expected security impact
- any known prerequisites or required privileges
- suggested remediation, when available

Do not include production credentials, access tokens, refresh tokens, database URLs, private keys, or customer data in a report.

## Response expectations

A report should first be reproduced and triaged before a severity is assigned. Confirmed issues should be fixed on `main`, covered by regression tests where practical, and included in the next release.

## Supply-chain controls

The repository uses automated secret scanning, dependency review, filesystem/container vulnerability scanning, Kubernetes configuration scanning, SBOM generation, immutable GitHub Action pins, image attestations, and keyless container signing.

## Webhook security

Webhook credentials use a separate `WEBHOOK_SIGNING_KEY`; do not reuse or expose the JWT signing secret as a receiver credential.

Webhook requests are HMAC-signed over timestamp plus raw request body. Receivers should verify the timestamp, signature, and event ID before processing.

Production webhook delivery blocks private, loopback, link-local, and unspecified network targets by default to reduce SSRF risk. Keep `WEBHOOK_ALLOW_PRIVATE_NETWORKS=false` unless a reviewed private-network integration explicitly requires otherwise.

Webhook signing secrets, production endpoint credentials, event payloads containing private customer data, and dead-letter payloads must not be posted in public vulnerability reports.
