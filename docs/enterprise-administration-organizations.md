# Phase 28 — Enterprise Administration & Organization Management

Phase 28 adds an organization control plane above workspaces.

## Organization hierarchy and lifecycle

Organizations can optionally reference a parent organization. Child organizations can be created by an administrator of the parent. Organization owners can move the organization between `active`, `suspended`, and `deactivated` states. Suspended/deactivated organizations remain readable but administrative mutations are blocked until reactivated.

## Roles and ownership

Organization roles are:

- `owner`: lifecycle, quota, ownership, and all administration.
- `admin`: organization administration.
- `delegated_admin`: directory/invitation/team/workspace/domain administration without lifecycle or ownership authority.
- `member`: directory/read access.

Ownership transfer is explicit and atomic at repository level: the new owner must already be an organization member, the previous owner becomes an admin, and the target becomes owner.

## Quotas

Every organization has member and workspace limits. Quotas are enforced before bulk member insertion, invitation acceptance, and workspace attachment. Owners cannot lower quotas below current utilization.

## Invitations

Organization invitations use 256-bit opaque tokens. Only SHA-256 token hashes are stored. The raw token is returned only in the create-invitation response. Acceptance requires an authenticated user whose account email matches the invitation email.

## Enterprise directory and teams

Organization members are exposed as an enterprise directory enriched from the user repository. Administrators can bulk-upsert members and create teams containing existing organization members.

## Workspace administration

Workspaces can be attached to one organization. The actor must be an organization administrator and also have owner/admin access to the workspace being attached.

## Domain verification

Administrators can register a domain and receive a one-time verification token. The stored value is only the SHA-256 hash. The current implementation exposes an explicit proof-token confirmation API; a DNS TXT resolver can be added later without changing the persisted verification model.

## Dashboard and audit

The administration dashboard reports current status, quotas/capacity, member/workspace counts, pending invitations, teams, and verified domains. Administrative mutations write organization-scoped audit events.

## Main endpoints

- `GET|POST /api/organizations`
- `GET /api/organizations/{id}`
- `PUT /api/organizations/{id}/status`
- `PUT /api/organizations/{id}/quota`
- `PUT /api/organizations/{id}/ownership`
- `GET /api/organizations/{id}/directory`
- `POST /api/organizations/{id}/members/bulk`
- `GET|POST /api/organizations/{id}/workspaces`
- `GET|POST /api/organizations/{id}/invitations`
- `POST /api/organizations/invitations/accept`
- `GET|POST /api/organizations/{id}/teams`
- `GET|POST /api/organizations/{id}/domains`
- `GET /api/organizations/{id}/dashboard`
- `GET /api/organizations/{id}/audit`
