package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrInvalidWorkspaceName = errors.New("workspace name is required")
	ErrInvalidWorkspaceRole = errors.New("workspace role must be admin or member")
	ErrWorkspaceForbidden   = errors.New("workspace action is forbidden")
	ErrPersonalWorkspace    = errors.New("personal workspace cannot be deleted")
	ErrOwnerMembership      = errors.New("owner membership cannot be changed or removed")
	ErrWorkspaceLegalHold   = errors.New("workspace cannot be deleted while a legal hold is active")
)

type WorkspaceDeletionGuard interface {
	HasActiveLegalHold(workspaceID int64) (bool, error)
}

type WorkspaceService struct {
	workspaces    repository.WorkspaceRepository
	users         repository.UserRepository
	deletionGuard WorkspaceDeletionGuard
}

func NewWorkspaceService(workspaces repository.WorkspaceRepository, users repository.UserRepository) *WorkspaceService {
	return &WorkspaceService{workspaces: workspaces, users: users}
}

func (s *WorkspaceService) SetDeletionGuard(guard WorkspaceDeletionGuard) {
	s.deletionGuard = guard
}

func (s *WorkspaceService) List(userID int64) ([]model.WorkspaceAccess, error) {
	return s.workspaces.ListForUser(userID, time.Now())
}

func (s *WorkspaceService) Access(userID, workspaceID int64) (model.WorkspaceAccess, error) {
	return s.workspaces.ResolveAccess(userID, workspaceID, time.Now())
}

func (s *WorkspaceService) Create(userID int64, req model.CreateWorkspaceRequest) (model.WorkspaceAccess, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return model.WorkspaceAccess{}, ErrInvalidWorkspaceName
	}
	now := time.Now()
	access, err := s.workspaces.Create(userID, name, now)
	if err != nil {
		return model.WorkspaceAccess{}, err
	}
	if err := s.audit(access.ID, userID, "workspace.created", "workspace", access.ID, map[string]any{"name": name}, now); err != nil {
		return model.WorkspaceAccess{}, err
	}
	return access, nil
}

func (s *WorkspaceService) Rename(actorID, workspaceID int64, req model.RenameWorkspaceRequest) (model.Workspace, error) {
	access, err := s.requireAdmin(actorID, workspaceID)
	if err != nil {
		return model.Workspace{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return model.Workspace{}, ErrInvalidWorkspaceName
	}
	now := time.Now()
	workspace, err := s.workspaces.Rename(workspaceID, name, now)
	if err != nil {
		return model.Workspace{}, err
	}
	if err := s.audit(workspaceID, actorID, "workspace.renamed", "workspace", workspaceID, map[string]any{
		"previous_name": access.Name,
		"name":          name,
	}, now); err != nil {
		return model.Workspace{}, err
	}
	return workspace, nil
}

func (s *WorkspaceService) Delete(actorID, workspaceID int64) error {
	access, err := s.workspaces.ResolveAccess(actorID, workspaceID, time.Now())
	if err != nil {
		return err
	}
	if access.Role != model.WorkspaceRoleOwner {
		return ErrWorkspaceForbidden
	}
	if access.IsPersonal {
		return ErrPersonalWorkspace
	}
	if s.deletionGuard != nil {
		held, err := s.deletionGuard.HasActiveLegalHold(workspaceID)
		if err != nil {
			return err
		}
		if held {
			return ErrWorkspaceLegalHold
		}
	}
	now := time.Now()
	if err := s.audit(workspaceID, actorID, "workspace.deleted", "workspace", workspaceID, map[string]any{"name": access.Name}, now); err != nil {
		return err
	}
	return s.workspaces.Delete(workspaceID)
}

func (s *WorkspaceService) ListMembers(actorID, workspaceID int64) ([]model.WorkspaceMember, error) {
	if _, err := s.workspaces.ResolveAccess(actorID, workspaceID, time.Now()); err != nil {
		return nil, err
	}
	return s.workspaces.ListMembers(workspaceID)
}

func (s *WorkspaceService) AddMember(actorID, workspaceID int64, req model.AddWorkspaceMemberRequest) (model.WorkspaceMember, error) {
	access, err := s.requireAdmin(actorID, workspaceID)
	if err != nil {
		return model.WorkspaceMember{}, err
	}
	role, err := normalizedAssignableRole(req.Role)
	if err != nil {
		return model.WorkspaceMember{}, err
	}
	if access.Role == model.WorkspaceRoleAdmin && role != model.WorkspaceRoleMember {
		return model.WorkspaceMember{}, ErrWorkspaceForbidden
	}
	user, err := s.users.FindByEmail(strings.ToLower(strings.TrimSpace(req.Email)))
	if err != nil {
		return model.WorkspaceMember{}, err
	}
	now := time.Now()
	member, err := s.workspaces.AddMember(workspaceID, user.ID, role, now)
	if err != nil {
		return model.WorkspaceMember{}, err
	}
	member.Name = user.Name
	member.Email = user.Email
	if err := s.audit(workspaceID, actorID, "workspace.member_added", "user", user.ID, map[string]any{"role": role}, now); err != nil {
		return model.WorkspaceMember{}, err
	}
	return member, nil
}

func (s *WorkspaceService) UpdateMemberRole(actorID, workspaceID, targetUserID int64, req model.UpdateWorkspaceMemberRequest) (model.WorkspaceMember, error) {
	access, err := s.requireAdmin(actorID, workspaceID)
	if err != nil {
		return model.WorkspaceMember{}, err
	}
	role, err := normalizedAssignableRole(req.Role)
	if err != nil {
		return model.WorkspaceMember{}, err
	}
	current, err := s.member(workspaceID, targetUserID)
	if err != nil {
		return model.WorkspaceMember{}, err
	}
	if current.Role == model.WorkspaceRoleOwner {
		return model.WorkspaceMember{}, ErrOwnerMembership
	}
	if access.Role == model.WorkspaceRoleAdmin && (current.Role != model.WorkspaceRoleMember || role != model.WorkspaceRoleMember) {
		return model.WorkspaceMember{}, ErrWorkspaceForbidden
	}

	now := time.Now()
	member, err := s.workspaces.UpdateMemberRole(workspaceID, targetUserID, role, now)
	if err != nil {
		return model.WorkspaceMember{}, err
	}
	if err := s.audit(workspaceID, actorID, "workspace.member_role_updated", "user", targetUserID, map[string]any{
		"previous_role": current.Role,
		"role":          role,
	}, now); err != nil {
		return model.WorkspaceMember{}, err
	}
	return member, nil
}

func (s *WorkspaceService) RemoveMember(actorID, workspaceID, targetUserID int64) error {
	access, err := s.requireAdmin(actorID, workspaceID)
	if err != nil {
		return err
	}
	current, err := s.member(workspaceID, targetUserID)
	if err != nil {
		return err
	}
	if current.Role == model.WorkspaceRoleOwner {
		return ErrOwnerMembership
	}
	if access.Role == model.WorkspaceRoleAdmin && current.Role != model.WorkspaceRoleMember {
		return ErrWorkspaceForbidden
	}
	if err := s.workspaces.RemoveMember(workspaceID, targetUserID); err != nil {
		return err
	}
	return s.audit(workspaceID, actorID, "workspace.member_removed", "user", targetUserID, map[string]any{
		"previous_role": current.Role,
	}, time.Now())
}

func (s *WorkspaceService) Audit(actorID, workspaceID int64, limit int) ([]model.AuditEvent, error) {
	access, err := s.requireAdmin(actorID, workspaceID)
	if err != nil {
		return nil, err
	}
	if access.Role != model.WorkspaceRoleOwner && access.Role != model.WorkspaceRoleAdmin {
		return nil, ErrWorkspaceForbidden
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	return s.workspaces.ListAudit(workspaceID, limit)
}

func (s *WorkspaceService) requireAdmin(userID, workspaceID int64) (model.WorkspaceAccess, error) {
	access, err := s.workspaces.ResolveAccess(userID, workspaceID, time.Now())
	if err != nil {
		return model.WorkspaceAccess{}, err
	}
	if access.Role != model.WorkspaceRoleOwner && access.Role != model.WorkspaceRoleAdmin {
		return model.WorkspaceAccess{}, ErrWorkspaceForbidden
	}
	return access, nil
}

func (s *WorkspaceService) member(workspaceID, userID int64) (model.WorkspaceMember, error) {
	members, err := s.workspaces.ListMembers(workspaceID)
	if err != nil {
		return model.WorkspaceMember{}, err
	}
	for _, member := range members {
		if member.UserID == userID {
			return member, nil
		}
	}
	return model.WorkspaceMember{}, repository.ErrWorkspaceMemberNotFound
}

func normalizedAssignableRole(role string) (string, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	if role != model.WorkspaceRoleAdmin && role != model.WorkspaceRoleMember {
		return "", ErrInvalidWorkspaceRole
	}
	return role, nil
}

func (s *WorkspaceService) audit(workspaceID, actorID int64, action, resourceType string, resourceID int64, metadata map[string]any, now time.Time) error {
	wid := workspaceID
	uid := actorID
	return s.workspaces.RecordAudit(model.AuditEvent{
		WorkspaceID:  &wid,
		ActorUserID:  &uid,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   fmt.Sprintf("%d", resourceID),
		Metadata:     metadata,
		CreatedAt:    now,
	})
}
