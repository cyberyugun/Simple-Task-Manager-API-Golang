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

The application uses the \`ObjectStore\` interface. The current signed URL adapter is provider-neutral and records one of:

- \`s3\`
- \`s3_compatible\`
- \`azure_blob\`
- \`gcs\`
- \`development\`

It generates short-lived HMAC-signed upload/download URLs against a configured storage gateway/base URL. This keeps cloud SDKs out of the core domain and allows a production adapter or gateway to translate the contract to AWS S3, Azure Blob, GCS, MinIO, Cloudflare R2, or another S3-compatible backend.

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

\`AttachmentScanner\` is a pluggable scanning hook. The initial default implementation is \`NoopAttachmentScanner\`, which is intended for development/test environments. Production deployments should replace it with an antivirus/malware scanner adapter such as a ClamAV service or security scanning gateway.

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
ATTACHMENT_SIGNING_SECRET=<at-least-16-char-secret>
ATTACHMENT_ENCRYPTION=AES256
ATTACHMENT_ENCRYPTION_KEY_ID=<optional-key-id>
ATTACHMENT_DEDUPLICATE=true
ATTACHMENT_SCAN_POLL_INTERVAL=5s
ATTACHMENT_RETENTION_POLL_INTERVAL=1h
ATTACHMENT_BATCH_SIZE=50
\`\`\`

If \`ATTACHMENT_SIGNING_SECRET\` is omitted, the API/worker uses \`JWT_SECRET\` as the signing fallback. Production should use a separate secret.

## Persistence

Migration \`021_file_attachment_platform.sql\` adds the \`attachments\` table with indexes for:

- workspace/task listing;
- workspace/comment listing;
- scan queue processing;
- retention processing;
- SHA-256 deduplication.

## Definition of done

Phase 36 is considered complete when direct-upload metadata and presigning, provider abstraction, task/comment attachment links, MIME/size/hash validation, quota accounting, scanning states, download gating, legal hold/retention integration, PostgreSQL parity, OpenAPI, unit/integration coverage, and Docker runtime smoke are green.
