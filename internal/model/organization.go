package model

import "time"

const (
	OrganizationStatusActive      = "active"
	OrganizationStatusSuspended   = "suspended"
	OrganizationStatusDeactivated = "deactivated"

	OrganizationRoleOwner          = "owner"
	OrganizationRoleAdmin          = "admin"
	OrganizationRoleDelegatedAdmin = "delegated_admin"
	OrganizationRoleMember         = "member"

	InvitationStatusPending  = "pending"
	InvitationStatusAccepted = "accepted"
	InvitationStatusRevoked  = "revoked"
	InvitationStatusExpired  = "expired"
)

type Organization struct {
	ID              int64      `json:"id"`
	ParentID        *int64     `json:"parent_id,omitempty"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	OwnerUserID     int64      `json:"owner_user_id"`
	MaxWorkspaces   int        `json:"max_workspaces"`
	MaxMembers      int        `json:"max_members"`
	CreatedByUserID int64      `json:"created_by_user_id"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	DeactivatedAt   *time.Time `json:"deactivated_at,omitempty"`
}

type OrganizationMember struct {
	OrganizationID int64     `json:"organization_id"`
	UserID         int64     `json:"user_id"`
	Role           string    `json:"role"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type OrganizationDirectoryEntry struct {
	OrganizationID int64     `json:"organization_id"`
	UserID         int64     `json:"user_id"`
	Name           string    `json:"name"`
	Email          string    `json:"email"`
	Role           string    `json:"role"`
	CreatedAt      time.Time `json:"created_at"`
}

type OrganizationWorkspace struct {
	OrganizationID int64     `json:"organization_id"`
	WorkspaceID    int64     `json:"workspace_id"`
	AttachedByID   int64     `json:"attached_by_user_id"`
	AttachedAt     time.Time `json:"attached_at"`
}

type OrganizationInvitation struct {
	ID               int64      `json:"id"`
	OrganizationID   int64      `json:"organization_id"`
	Email            string     `json:"email"`
	Role             string     `json:"role"`
	TokenHash        string     `json:"-"`
	Status           string     `json:"status"`
	InvitedByUserID  int64      `json:"invited_by_user_id"`
	ExpiresAt        time.Time  `json:"expires_at"`
	AcceptedByUserID *int64     `json:"accepted_by_user_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	AcceptedAt       *time.Time `json:"accepted_at,omitempty"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
}

type OrganizationInvitationSecret struct {
	Invitation OrganizationInvitation `json:"invitation"`
	Token      string                 `json:"token"`
}

type OrganizationTeam struct {
	ID             int64     `json:"id"`
	OrganizationID int64     `json:"organization_id"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	CreatedByID    int64     `json:"created_by_user_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type OrganizationTeamMember struct {
	TeamID    int64     `json:"team_id"`
	UserID    int64     `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

type OrganizationDomain struct {
	ID               int64      `json:"id"`
	OrganizationID   int64      `json:"organization_id"`
	Domain           string     `json:"domain"`
	VerificationHash string     `json:"-"`
	CreatedByUserID  int64      `json:"created_by_user_id"`
	CreatedAt        time.Time  `json:"created_at"`
	VerifiedAt       *time.Time `json:"verified_at,omitempty"`
	VerifiedByUserID *int64     `json:"verified_by_user_id,omitempty"`
}

type OrganizationDomainSecret struct {
	Domain OrganizationDomain `json:"domain"`
	Token  string             `json:"verification_token"`
}

type OrganizationAuditEvent struct {
	ID             int64          `json:"id"`
	OrganizationID int64          `json:"organization_id"`
	ActorUserID    *int64         `json:"actor_user_id,omitempty"`
	Action         string         `json:"action"`
	ResourceType   string         `json:"resource_type"`
	ResourceID     string         `json:"resource_id,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

type OrganizationDashboard struct {
	Organization      Organization `json:"organization"`
	MemberCount       int          `json:"member_count"`
	WorkspaceCount    int          `json:"workspace_count"`
	PendingInvites    int          `json:"pending_invites"`
	TeamCount         int          `json:"team_count"`
	VerifiedDomains   int          `json:"verified_domains"`
	WorkspaceCapacity int          `json:"workspace_capacity"`
	MemberCapacity    int          `json:"member_capacity"`
	GeneratedAt       time.Time    `json:"generated_at"`
}

type CreateOrganizationRequest struct {
	Name          string `json:"name"`
	ParentID      *int64 `json:"parent_id,omitempty"`
	MaxWorkspaces int    `json:"max_workspaces"`
	MaxMembers    int    `json:"max_members"`
}

type UpdateOrganizationStatusRequest struct {
	Status string `json:"status"`
}

type UpdateOrganizationQuotaRequest struct {
	MaxWorkspaces int `json:"max_workspaces"`
	MaxMembers    int `json:"max_members"`
}

type TransferOrganizationOwnershipRequest struct {
	UserID int64 `json:"user_id"`
}

type AttachOrganizationWorkspaceRequest struct {
	WorkspaceID int64 `json:"workspace_id"`
}

type CreateOrganizationInvitationRequest struct {
	Email          string `json:"email"`
	Role           string `json:"role"`
	ExpiresInHours int    `json:"expires_in_hours,omitempty"`
}

type AcceptOrganizationInvitationRequest struct {
	Token string `json:"token"`
}

type BulkOrganizationMemberRequest struct {
	UserIDs []int64 `json:"user_ids"`
	Role    string  `json:"role"`
}

type CreateOrganizationTeamRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type AddOrganizationTeamMemberRequest struct {
	UserID int64 `json:"user_id"`
}

type CreateOrganizationDomainRequest struct {
	Domain string `json:"domain"`
}

type VerifyOrganizationDomainRequest struct {
	Token string `json:"verification_token"`
}
