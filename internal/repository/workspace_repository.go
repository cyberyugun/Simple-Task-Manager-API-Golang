package repository

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrWorkspaceNotFound   = errors.New("workspace not found")
	ErrWorkspaceMemberExists = errors.New("workspace member already exists")
	ErrWorkspaceMemberNotFound = errors.New("workspace member not found")
)

type WorkspaceRepository interface {
	ResolveDefault(userID int64, now time.Time) (model.WorkspaceAccess, error)
	ResolveAccess(userID, workspaceID int64, now time.Time) (model.WorkspaceAccess, error)
	ListForUser(userID int64, now time.Time) ([]model.WorkspaceAccess, error)
	Create(userID int64, name string, now time.Time) (model.WorkspaceAccess, error)
	Rename(workspaceID int64, name string, now time.Time) (model.Workspace, error)
	Delete(workspaceID int64) error
	ListMembers(workspaceID int64) ([]model.WorkspaceMember, error)
	AddMember(workspaceID, userID int64, role string, now time.Time) (model.WorkspaceMember, error)
	UpdateMemberRole(workspaceID, userID int64, role string, now time.Time) (model.WorkspaceMember, error)
	RemoveMember(workspaceID, userID int64) error
	RecordAudit(event model.AuditEvent) error
	ListAudit(workspaceID int64, limit int) ([]model.AuditEvent, error)
}

type InMemoryWorkspaceRepository struct {
	mu         sync.RWMutex
	workspaces map[int64]model.Workspace
	members    map[int64]map[int64]model.WorkspaceMember
	audits     []model.AuditEvent
	nextID     int64
	nextAudit  int64
}

func NewInMemoryWorkspaceRepository() *InMemoryWorkspaceRepository {
	return &InMemoryWorkspaceRepository{
		workspaces: make(map[int64]model.Workspace),
		members:    make(map[int64]map[int64]model.WorkspaceMember),
		nextID:     1,
		nextAudit:  1,
	}
}

func (r *InMemoryWorkspaceRepository) ensurePersonalLocked(userID int64, now time.Time) model.WorkspaceAccess {
	for id, workspace := range r.workspaces {
		if workspace.IsPersonal && workspace.CreatedByUserID == userID {
			member := r.members[id][userID]
			return model.WorkspaceAccess{Workspace: workspace, Role: member.Role}
		}
	}
	workspace := model.Workspace{
		ID: r.nextID, Name: "Personal Workspace", CreatedByUserID: userID,
		IsPersonal: true, CreatedAt: now, UpdatedAt: now,
	}
	r.nextID++
	r.workspaces[workspace.ID] = workspace
	r.members[workspace.ID] = map[int64]model.WorkspaceMember{
		userID: {
			WorkspaceID: workspace.ID, UserID: userID, Role: model.WorkspaceRoleOwner,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	return model.WorkspaceAccess{Workspace: workspace, Role: model.WorkspaceRoleOwner}
}

func (r *InMemoryWorkspaceRepository) ResolveDefault(userID int64, now time.Time) (model.WorkspaceAccess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ensurePersonalLocked(userID, now), nil
}

func (r *InMemoryWorkspaceRepository) ResolveAccess(userID, workspaceID int64, now time.Time) (model.WorkspaceAccess, error) {
	if workspaceID <= 0 {
		return r.ResolveDefault(userID, now)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	workspace, ok := r.workspaces[workspaceID]
	if !ok {
		return model.WorkspaceAccess{}, ErrWorkspaceNotFound
	}
	member, ok := r.members[workspaceID][userID]
	if !ok {
		return model.WorkspaceAccess{}, ErrWorkspaceNotFound
	}
	return model.WorkspaceAccess{Workspace: workspace, Role: member.Role}, nil
}

func (r *InMemoryWorkspaceRepository) ListForUser(userID int64, now time.Time) ([]model.WorkspaceAccess, error) {
	if _, err := r.ResolveDefault(userID, now); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]model.WorkspaceAccess, 0)
	for workspaceID, members := range r.members {
		member, ok := members[userID]
		if !ok {
			continue
		}
		items = append(items, model.WorkspaceAccess{Workspace: r.workspaces[workspaceID], Role: member.Role})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsPersonal != items[j].IsPersonal {
			return items[i].IsPersonal
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (r *InMemoryWorkspaceRepository) Create(userID int64, name string, now time.Time) (model.WorkspaceAccess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	workspace := model.Workspace{
		ID: r.nextID, Name: strings.TrimSpace(name), CreatedByUserID: userID,
		CreatedAt: now, UpdatedAt: now,
	}
	r.nextID++
	r.workspaces[workspace.ID] = workspace
	r.members[workspace.ID] = map[int64]model.WorkspaceMember{
		userID: {
			WorkspaceID: workspace.ID, UserID: userID, Role: model.WorkspaceRoleOwner,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	return model.WorkspaceAccess{Workspace: workspace, Role: model.WorkspaceRoleOwner}, nil
}

func (r *InMemoryWorkspaceRepository) Rename(workspaceID int64, name string, now time.Time) (model.Workspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	workspace, ok := r.workspaces[workspaceID]
	if !ok {
		return model.Workspace{}, ErrWorkspaceNotFound
	}
	workspace.Name = strings.TrimSpace(name)
	workspace.UpdatedAt = now
	r.workspaces[workspaceID] = workspace
	return workspace, nil
}

func (r *InMemoryWorkspaceRepository) Delete(workspaceID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.workspaces[workspaceID]; !ok {
		return ErrWorkspaceNotFound
	}
	delete(r.workspaces, workspaceID)
	delete(r.members, workspaceID)
	return nil
}

func (r *InMemoryWorkspaceRepository) ListMembers(workspaceID int64) ([]model.WorkspaceMember, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.workspaces[workspaceID]; !ok {
		return nil, ErrWorkspaceNotFound
	}
	items := make([]model.WorkspaceMember, 0, len(r.members[workspaceID]))
	for _, member := range r.members[workspaceID] {
		items = append(items, member)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UserID < items[j].UserID })
	return items, nil
}

func (r *InMemoryWorkspaceRepository) AddMember(workspaceID, userID int64, role string, now time.Time) (model.WorkspaceMember, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.workspaces[workspaceID]; !ok {
		return model.WorkspaceMember{}, ErrWorkspaceNotFound
	}
	if _, ok := r.members[workspaceID][userID]; ok {
		return model.WorkspaceMember{}, ErrWorkspaceMemberExists
	}
	member := model.WorkspaceMember{WorkspaceID: workspaceID, UserID: userID, Role: role, CreatedAt: now, UpdatedAt: now}
	r.members[workspaceID][userID] = member
	return member, nil
}

func (r *InMemoryWorkspaceRepository) UpdateMemberRole(workspaceID, userID int64, role string, now time.Time) (model.WorkspaceMember, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	member, ok := r.members[workspaceID][userID]
	if !ok {
		return model.WorkspaceMember{}, ErrWorkspaceMemberNotFound
	}
	member.Role = role
	member.UpdatedAt = now
	r.members[workspaceID][userID] = member
	return member, nil
}

func (r *InMemoryWorkspaceRepository) RemoveMember(workspaceID, userID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.members[workspaceID][userID]; !ok {
		return ErrWorkspaceMemberNotFound
	}
	delete(r.members[workspaceID], userID)
	return nil
}

func (r *InMemoryWorkspaceRepository) RecordAudit(event model.AuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event.ID = r.nextAudit
	r.nextAudit++
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	if event.Metadata != nil {
		if _, err := json.Marshal(event.Metadata); err != nil {
			return err
		}
	}
	r.audits = append(r.audits, event)
	return nil
}

func (r *InMemoryWorkspaceRepository) ListAudit(workspaceID int64, limit int) ([]model.AuditEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]model.AuditEvent, 0)
	for i := len(r.audits) - 1; i >= 0 && len(items) < limit; i-- {
		event := r.audits[i]
		if event.WorkspaceID != nil && *event.WorkspaceID == workspaceID {
			items = append(items, event)
		}
	}
	return items, nil
}
