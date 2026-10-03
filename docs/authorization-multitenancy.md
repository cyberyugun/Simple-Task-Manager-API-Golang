# Authorization, RBAC, and Multi-Tenant Isolation

Phase 23 adds workspace tenancy while preserving the existing API v1 task routes.

## Tenant model

Every authenticated user has a personal workspace. Existing clients do not need to send a workspace selector:

```text
Authorization: Bearer <token>
GET /api/tasks
```

The server resolves the user's personal workspace automatically.

Shared workspace clients select a tenant with:

```text
X-Workspace-ID: <workspace-id>
```

The middleware resolves membership before the task handler runs. A workspace that does not exist and a workspace the user cannot access both return `404`, reducing tenant enumeration leakage.

Responses from workspace-scoped task routes include:

```text
X-Workspace-ID: <resolved-id>
X-Workspace-Role: owner | admin | member
```

## Roles

| Capability | Owner | Admin | Member |
| --- | --- | --- | --- |
| Read/write workspace tasks | Yes | Yes | Yes |
| View workspace members | Yes | Yes | Yes |
| Rename workspace | Yes | Yes | No |
| Add ordinary member | Yes | Yes | No |
| Add admin | Yes | No | No |
| Change member role | Yes | Limited | No |
| Remove ordinary member | Yes | Yes | No |
| Change/remove owner | No | No | No |
| Delete shared workspace | Yes | No | No |
| Delete personal workspace | No | No | No |
| View audit trail | Yes | Yes | No |

Ownership transfer is intentionally not implemented in this phase. This avoids accidental privilege escalation or orphaned workspaces.

## Workspace API

Protected endpoints:

```text
GET    /api/workspaces
POST   /api/workspaces
GET    /api/workspaces/{id}
PUT    /api/workspaces/{id}
DELETE /api/workspaces/{id}

GET    /api/workspaces/{id}/members
POST   /api/workspaces/{id}/members
PUT    /api/workspaces/{id}/members/{user_id}
DELETE /api/workspaces/{id}/members/{user_id}

GET    /api/workspaces/{id}/audit
```

Members are added by email. Assignable roles are `admin` and `member`; `owner` cannot be granted through the membership API.

## Task isolation

Task repository queries are scoped by `workspace_id`, not only by the authenticated user.

The authorization sequence is:

```text
JWT
 |
 v
authenticated user_id
 |
 v
X-Workspace-ID or personal default
 |
 v
workspace membership lookup
 |
 +-- not member --> 404
 |
 v
resolved WorkspaceAccess
 |
 v
task query WHERE workspace_id = resolved tenant
```

Handlers cannot accept an arbitrary workspace ID directly from a URL/query parameter and bypass the middleware context.

## Rolling-deployment compatibility

The previous API version stored task ownership only in `tasks.user_id`.

Phase 23 uses an expand-compatible migration:

- adds nullable `workspace_id`
- adds nullable `created_by_user_id`
- creates a personal workspace for every existing user
- creates owner memberships
- backfills existing tasks into personal workspaces
- keeps the old `user_id` column during the compatibility window

New **personal** tasks continue to populate legacy `user_id`, so old pods can still serve them during rolling deployment.

New **shared-workspace** tasks store the creator in `created_by_user_id` but deliberately leave legacy `user_id` null. This prevents an old pod, which only filters by `user_id`, from accidentally exposing a shared tenant task inside a creator's personal task list.

New repository queries also recognize old tasks that may be inserted during the coexistence window with a null `workspace_id`.

A later contract/cleanup phase can remove this legacy bridge only after old application versions are no longer supported.

## Audit trail

Security-relevant workspace changes are written to `audit_events`, including:

- workspace creation
- workspace rename
- workspace deletion
- member added
- member role changed
- member removed

Events include workspace, actor user, action, resource type/id, metadata, and timestamp.

The audit endpoint is restricted to owners and admins and is capped at 500 events per request.

## PostgreSQL constraints and indexes

The schema enforces:

- workspace foreign keys
- one personal workspace per creating user
- unique workspace membership
- role values limited to `owner`, `admin`, `member`
- task workspace foreign key
- audit actor/workspace references

Indexes cover membership lookup, workspace task filtering, task completion/time ordering, creator lookup, and audit history.

## Security tests

Phase 23 tests verify:

- different personal workspaces are isolated
- task lookup/delete cannot cross workspace IDs
- non-members cannot resolve another workspace
- shared members can use the shared tenant
- shared tasks do not appear in personal workspace lists
- members cannot view admin audit data
- members cannot mutate roles
- admins cannot mutate/remove owners
- live HTTP requests preserve the same rules
- OpenAPI remains backward compatible with v1 consumers
