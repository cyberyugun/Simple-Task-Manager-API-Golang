package model

import "time"

const (
	WorkspaceRoleOwner  = "owner"
	WorkspaceRoleAdmin  = "admin"
	WorkspaceRoleMember = "member"
)

type Workspace struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	CreatedByUserID int64     `json:"created_by_user_id"`
	IsPersonal      bool      `json:"is_personal"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type WorkspaceAccess struct {
	Workspace
	Role string `json:"role"`
}

type WorkspaceMember struct {
	WorkspaceID int64     `json:"workspace_id"`
	UserID      int64     `json:"user_id"`
	Name        string    `json:"name,omitempty"`
	Email       string    `json:"email,omitempty"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateWorkspaceRequest struct {
	Name string `json:"name"`
}

type RenameWorkspaceRequest struct {
	Name string `json:"name"`
}

type AddWorkspaceMemberRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type UpdateWorkspaceMemberRequest struct {
	Role string `json:"role"`
}

type AuditEvent struct {
	ID           int64          `json:"id"`
	WorkspaceID  *int64         `json:"workspace_id,omitempty"`
	ActorUserID  *int64         `json:"actor_user_id,omitempty"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}
