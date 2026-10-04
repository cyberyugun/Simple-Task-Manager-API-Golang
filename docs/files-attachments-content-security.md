# Phase 36 — Files, Attachments & Content Security

Phase 36 adds secure file collaboration for tasks and comments while keeping object storage behind a provider abstraction.

## Core flow

\`\`\`text
REQUEST UPLOAD
  -> VALIDATE MIME / SIZE / QUOTA
  -> PRESIGNED DIRECT UPLOAD
  -> COMPLETE + SHA-256 CHECK
  -> MALWARE SCAN HOOK
  -> CLEAN / INFECTED / QUARANTINED
  -> ATTACH TO TASK / COMMENT
  -> PRESIGNED DOWNLOAD
  -> RETENTION / LEGAL HOLD / PRIVACY DELETE
\`\`\`

## Storage abstraction

The application uses the `ObjectStore` interface and keeps raw file bytes out of the API process.

Two storage modes are available:

- provider-neutral signed gateway mode, which keeps the original HMAC-signed upload/download URL contract for `s3`, `s3_compatible`, `azure_blob`, `gcs`, and `development`;
- native object-store mode, enabled with `ATTACHMENT_STORAGE_NATIVE=true`.

Native mode implements AWS S3/S3-compatible and Azure Blob Storage providers.

S3 generates AWS SigV4 presigned PUT/GET URLs, supports static credentials for local/S3-compatible environments and AWS web-identity credentials for EKS/IRSA deployments, verifies object size plus server-side SHA-256 metadata with a signed HEAD request before marking an upload complete, and verifies deletion with a post-delete HEAD request. Upload presigning includes `x-amz-meta-sha256`; optional server-side encryption supports `AES256` and `aws:kms`.

Azure Blob generates service SAS URLs when an account key is explicitly configured, or user-delegation SAS URLs when AKS workload identity / Azure Managed Identity is used. Workload-identity token exchange requests the `https://storage.azure.com/.default` scope and user-delegation keys are cached within their provider expiry. Uploads require `BlockBlob`, persist `x-ms-meta-sha256`, and can bind a signed encryption scope via `ATTACHMENT_ENCRYPTION_KEY_ID`. Upload completion and deletion are verified through authenticated HEAD/DELETE calls.

GCS remains on the signed-gateway contract until its native adapter is added. Setting native mode for an unimplemented provider fails startup rather than silently falling back.

The core API never accepts raw file bytes.

## Upload safety

Upload requests require:

- a valid task in the current workspace;
- optional comment attachment validated against the task;
- allowlisted MIME shape and explicit rejection of executable/shell MIME types;
- positive size under the configured per-file maximum;
- a 64-character SHA-256 digest;
- enough remaining workspace quota.

The object key is generated server-side from workspace, digest prefix, timestamp, and a sanitized file name. Client-supplied paths are never used directly.

After the direct upload, the client calls the completion endpoint with the observed size and SHA-256. Metadata must match the upload request before the object enters the scan queue.

## Integrity and deduplication

Each attachment stores SHA-256 and size metadata. Optional workspace-local deduplication can create a new attachment reference that reuses a clean object with the same digest and size.

## Malware scanning

\`AttachmentScanner\` is a pluggable scanning hook. Development/test environments may use \`NoopAttachmentScanner\`. When \`ATTACHMENT_SCANNER_URL\` is configured, the API and worker use \`HTTPAttachmentScanner\`, an authenticated remote scanning-gateway adapter.

The remote scanner receives attachment/object metadata rather than raw file bytes. It can use its own storage identity to fetch the object from S3, Azure Blob, GCS, MinIO/R2 or another configured backend and invoke ClamAV or another security engine.

Security controls on the scanner client:

- HTTPS is required unless the existing insecure-development flag is explicitly enabled;
- redirects are not followed;
- response bodies are bounded;
- optional bearer authentication is supported;
- optional HMAC-SHA256 request signing covers the Unix timestamp plus exact JSON request body;
- scanner transport/protocol failures remain fail-closed and are quarantined by the existing scan state machine;
- \`ATTACHMENT_SCANNER_REQUIRED=true\` makes startup fail if no scanner endpoint is configured.

The worker transitions objects:

\`\`\`text
uploaded -> scanning -> clean
                    -> infected
                    -> quarantined (scanner error)
\`\`\`

Only \`clean\` attachments can receive download URLs.

## Governance

Attachment metadata includes:

- encryption mode and encryption key identifier;
- legal-hold flag;
- optional \`retain_until\`;
- scan status/engine/message;
- upload, scan, deletion timestamps.

Legal hold blocks deletion. A future retain date also blocks manual deletion. The retention worker deletes expired objects only when legal hold is not active.

Workspace owners/admins can update attachment governance metadata. Workspace audit records are emitted for upload request/completion, deduplication, scan completion, download request, governance change, manual deletion, and retention deletion.

## API

\`\`\`text
POST   /api/attachments/uploads
GET    /api/attachments?task_id=&comment_id=
GET    /api/attachments/usage
POST   /api/attachments/{attachment_id}/complete
POST   /api/attachments/{attachment_id}/download
PATCH  /api/attachments/{attachment_id}/governance
DELETE /api/attachments/{attachment_id}
\`\`\`

All endpoints are workspace-scoped through the existing workspace middleware and task scope.

## Worker

The worker processes malware scan and retention queues.

Environment:

\`\`\`text
ATTACHMENT_STORAGE_PROVIDER=s3_compatible
ATTACHMENT_STORAGE_BUCKET=task-attachments
ATTACHMENT_STORAGE_BASE_URL=https://storage.example.com
ATTACHMENT_ENCRYPTION=AES256
ATTACHMENT_ENCRYPTION_KEY_ID=<optional-key-id>
ATTACHMENT_DEDUPLICATE=true
ATTACHMENT_SCANNER_URL=https://scanner.internal.example/v1/scan
ATTACHMENT_SCANNER_REQUIRED=true
ATTACHMENT_SCANNER_BEARER_TOKEN=<optional>
ATTACHMENT_SCANNER_SIGNING_SECRET=<recommended>
ATTACHMENT_SCAN_POLL_INTERVAL=5s
ATTACHMENT_RETENTION_POLL_INTERVAL=1h
ATTACHMENT_BATCH_SIZE=50
\`\`\`

If \`ATTACHMENT_SIGNING_SECRET\` is omitted, the API/worker uses \`JWT_SECRET\` as the signing fallback. Production should inject a separate signing value at deployment time from the platform secret manager; do not commit it to source control.

\`ATTACHMENT_SIGNING_SECRET\` and \`ATTACHMENT_STORAGE_BASE_URL\` apply to signed-gateway mode. Native S3 mode instead signs provider URLs and verification/deletion requests with AWS SigV4 credentials obtained from workload identity or explicit local/test credentials.

For production content security, set \`ATTACHMENT_SCANNER_REQUIRED=true\`. Store scanner bearer/signing credentials in the deployment secret manager. The scanner response contract is:

\`\`\`json
{"clean": true, "engine": "clamav-gateway", "message": "OK"}
\`\`\`

A response with \`clean:false\` marks the attachment infected. HTTP errors, timeouts, malformed JSON, or a missing engine identifier quarantine the attachment rather than making it downloadable.

## Persistence

Migration \`021_file_attachment_platform.sql\` adds the \`attachments\` table with indexes for:

- workspace/task listing;
- workspace/comment listing;
- scan queue processing;
- retention processing;
- SHA-256 deduplication.

## Definition of done

Phase 36 is considered complete when direct-upload metadata and presigning, provider abstraction, task/comment attachment links, MIME/size/hash validation, quota accounting, scanning states, download gating, legal hold/retention integration, PostgreSQL parity, OpenAPI, unit/integration coverage, and Docker runtime smoke are green.
