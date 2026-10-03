package repository

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrOrganizationNotFound           = errors.New("organization not found")
	ErrOrganizationMemberNotFound     = errors.New("organization member not found")
	ErrOrganizationWorkspaceExists    = errors.New("workspace is already attached to an organization")
	ErrOrganizationInvitationNotFound = errors.New("organization invitation not found")
	ErrOrganizationTeamNotFound       = errors.New("organization team not found")
	ErrOrganizationDomainNotFound     = errors.New("organization domain not found")
)

type OrganizationRepository interface {
	CreateOrganization(org model.Organization) (model.Organization, error)
	GetOrganization(id int64) (model.Organization, error)
	ListOrganizationsForUser(userID int64) ([]model.Organization, error)
	UpdateOrganizationStatus(id int64, status string, deactivatedAt *time.Time, now time.Time) (model.Organization, error)
	UpdateOrganizationQuota(id int64, maxWorkspaces, maxMembers int, now time.Time) (model.Organization, error)
	TransferOwnership(id, currentOwnerID, newOwnerID int64, now time.Time) (model.Organization, error)

	GetMember(organizationID, userID int64) (model.OrganizationMember, error)
	ListMembers(organizationID int64) ([]model.OrganizationMember, error)
	UpsertMember(member model.OrganizationMember) (model.OrganizationMember, error)
	RemoveMember(organizationID, userID int64) error
	CountMembers(organizationID int64) (int, error)

	AttachWorkspace(link model.OrganizationWorkspace) (model.OrganizationWorkspace, error)
	DetachWorkspace(organizationID, workspaceID int64) error
	ListWorkspaces(organizationID int64) ([]model.OrganizationWorkspace, error)
	CountWorkspaces(organizationID int64) (int, error)

	CreateInvitation(invitation model.OrganizationInvitation) (model.OrganizationInvitation, error)
	FindInvitationByHash(tokenHash string) (model.OrganizationInvitation, error)
	ListInvitations(organizationID int64) ([]model.OrganizationInvitation, error)
	AcceptInvitation(id, userID int64, now time.Time) (model.OrganizationInvitation, error)
	RevokeInvitation(organizationID, invitationID int64, now time.Time) error

	CreateTeam(team model.OrganizationTeam) (model.OrganizationTeam, error)
	ListTeams(organizationID int64) ([]model.OrganizationTeam, error)
	AddTeamMember(member model.OrganizationTeamMember) (model.OrganizationTeamMember, error)
	ListTeamMembers(teamID int64) ([]model.OrganizationTeamMember, error)

	CreateDomain(domain model.OrganizationDomain) (model.OrganizationDomain, error)
	ListDomains(organizationID int64) ([]model.OrganizationDomain, error)
	VerifyDomain(organizationID, domainID, actorUserID int64, verificationHash string, now time.Time) (model.OrganizationDomain, error)

	RecordAudit(event model.OrganizationAuditEvent) error
	ListAudit(organizationID int64, limit int) ([]model.OrganizationAuditEvent, error)
}

type InMemoryOrganizationRepository struct {
	mu          sync.Mutex
	orgs        map[int64]model.Organization
	members     map[int64]map[int64]model.OrganizationMember
	workspaces  map[int64]map[int64]model.OrganizationWorkspace
	invites     map[int64]model.OrganizationInvitation
	inviteHash  map[string]int64
	teams       map[int64]model.OrganizationTeam
	teamMembers map[int64]map[int64]model.OrganizationTeamMember
	domains     map[int64]model.OrganizationDomain
	audits      []model.OrganizationAuditEvent
	nextOrg     int64
	nextInvite  int64
	nextTeam    int64
	nextDomain  int64
	nextAudit   int64
}

func NewInMemoryOrganizationRepository() *InMemoryOrganizationRepository {
	return &InMemoryOrganizationRepository{
		orgs:        make(map[int64]model.Organization),
		members:     make(map[int64]map[int64]model.OrganizationMember),
		workspaces:  make(map[int64]map[int64]model.OrganizationWorkspace),
		invites:     make(map[int64]model.OrganizationInvitation),
		inviteHash:  make(map[string]int64),
		teams:       make(map[int64]model.OrganizationTeam),
		teamMembers: make(map[int64]map[int64]model.OrganizationTeamMember),
		domains:     make(map[int64]model.OrganizationDomain),
		nextOrg:     1,
		nextInvite:  1,
		nextTeam:    1,
		nextDomain:  1,
		nextAudit:   1,
	}
}

func (r *InMemoryOrganizationRepository) CreateOrganization(org model.Organization) (model.Organization, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	org.ID = r.nextOrg
	r.nextOrg++
	r.orgs[org.ID] = org
	r.members[org.ID] = map[int64]model.OrganizationMember{
		org.OwnerUserID: {
			OrganizationID: org.ID,
			UserID:         org.OwnerUserID,
			Role:           model.OrganizationRoleOwner,
			CreatedAt:      org.CreatedAt,
			UpdatedAt:      org.UpdatedAt,
		},
	}
	return org, nil
}

func (r *InMemoryOrganizationRepository) GetOrganization(id int64) (model.Organization, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	org, ok := r.orgs[id]
	if !ok {
		return model.Organization{}, ErrOrganizationNotFound
	}
	return org, nil
}

func (r *InMemoryOrganizationRepository) ListOrganizationsForUser(userID int64) ([]model.Organization, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.Organization, 0)
	for orgID, members := range r.members {
		if _, ok := members[userID]; ok {
			items = append(items, r.orgs[orgID])
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryOrganizationRepository) UpdateOrganizationStatus(id int64, status string, deactivatedAt *time.Time, now time.Time) (model.Organization, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	org, ok := r.orgs[id]
	if !ok {
		return model.Organization{}, ErrOrganizationNotFound
	}
	org.Status = status
	org.DeactivatedAt = deactivatedAt
	org.UpdatedAt = now
	r.orgs[id] = org
	return org, nil
}

func (r *InMemoryOrganizationRepository) UpdateOrganizationQuota(id int64, maxWorkspaces, maxMembers int, now time.Time) (model.Organization, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	org, ok := r.orgs[id]
	if !ok {
		return model.Organization{}, ErrOrganizationNotFound
	}
	org.MaxWorkspaces = maxWorkspaces
	org.MaxMembers = maxMembers
	org.UpdatedAt = now
	r.orgs[id] = org
	return org, nil
}

func (r *InMemoryOrganizationRepository) TransferOwnership(id, currentOwnerID, newOwnerID int64, now time.Time) (model.Organization, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	org, ok := r.orgs[id]
	if !ok || org.OwnerUserID != currentOwnerID {
		return model.Organization{}, ErrOrganizationNotFound
	}
	newMember, ok := r.members[id][newOwnerID]
	if !ok {
		return model.Organization{}, ErrOrganizationMemberNotFound
	}
	oldMember := r.members[id][currentOwnerID]
	oldMember.Role = model.OrganizationRoleAdmin
	oldMember.UpdatedAt = now
	newMember.Role = model.OrganizationRoleOwner
	newMember.UpdatedAt = now
	r.members[id][currentOwnerID] = oldMember
	r.members[id][newOwnerID] = newMember
	org.OwnerUserID = newOwnerID
	org.UpdatedAt = now
	r.orgs[id] = org
	return org, nil
}

func (r *InMemoryOrganizationRepository) GetMember(organizationID, userID int64) (model.OrganizationMember, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	member, ok := r.members[organizationID][userID]
	if !ok {
		return model.OrganizationMember{}, ErrOrganizationMemberNotFound
	}
	return member, nil
}

func (r *InMemoryOrganizationRepository) ListMembers(organizationID int64) ([]model.OrganizationMember, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.orgs[organizationID]; !ok {
		return nil, ErrOrganizationNotFound
	}
	items := make([]model.OrganizationMember, 0, len(r.members[organizationID]))
	for _, member := range r.members[organizationID] {
		items = append(items, member)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UserID < items[j].UserID })
	return items, nil
}

func (r *InMemoryOrganizationRepository) UpsertMember(member model.OrganizationMember) (model.OrganizationMember, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.orgs[member.OrganizationID]; !ok {
		return model.OrganizationMember{}, ErrOrganizationNotFound
	}
	if r.members[member.OrganizationID] == nil {
		r.members[member.OrganizationID] = make(map[int64]model.OrganizationMember)
	}
	if old, ok := r.members[member.OrganizationID][member.UserID]; ok {
		member.CreatedAt = old.CreatedAt
	}
	r.members[member.OrganizationID][member.UserID] = member
	return member, nil
}

func (r *InMemoryOrganizationRepository) RemoveMember(organizationID, userID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.members[organizationID][userID]; !ok {
		return ErrOrganizationMemberNotFound
	}
	delete(r.members[organizationID], userID)
	return nil
}

func (r *InMemoryOrganizationRepository) CountMembers(organizationID int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.orgs[organizationID]; !ok {
		return 0, ErrOrganizationNotFound
	}
	return len(r.members[organizationID]), nil
}

func (r *InMemoryOrganizationRepository) AttachWorkspace(link model.OrganizationWorkspace) (model.OrganizationWorkspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for orgID, links := range r.workspaces {
		if _, ok := links[link.WorkspaceID]; ok && orgID != link.OrganizationID {
			return model.OrganizationWorkspace{}, ErrOrganizationWorkspaceExists
		}
	}
	if r.workspaces[link.OrganizationID] == nil {
		r.workspaces[link.OrganizationID] = make(map[int64]model.OrganizationWorkspace)
	}
	if _, ok := r.workspaces[link.OrganizationID][link.WorkspaceID]; ok {
		return model.OrganizationWorkspace{}, ErrOrganizationWorkspaceExists
	}
	r.workspaces[link.OrganizationID][link.WorkspaceID] = link
	return link, nil
}

func (r *InMemoryOrganizationRepository) DetachWorkspace(organizationID, workspaceID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.workspaces[organizationID][workspaceID]; !ok {
		return ErrOrganizationNotFound
	}
	delete(r.workspaces[organizationID], workspaceID)
	return nil
}

func (r *InMemoryOrganizationRepository) ListWorkspaces(organizationID int64) ([]model.OrganizationWorkspace, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.OrganizationWorkspace, 0)
	for _, link := range r.workspaces[organizationID] {
		items = append(items, link)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].WorkspaceID < items[j].WorkspaceID })
	return items, nil
}

func (r *InMemoryOrganizationRepository) CountWorkspaces(organizationID int64) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.orgs[organizationID]; !ok {
		return 0, ErrOrganizationNotFound
	}
	return len(r.workspaces[organizationID]), nil
}

func (r *InMemoryOrganizationRepository) CreateInvitation(invitation model.OrganizationInvitation) (model.OrganizationInvitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	invitation.ID = r.nextInvite
	r.nextInvite++
	r.invites[invitation.ID] = invitation
	r.inviteHash[invitation.TokenHash] = invitation.ID
	return invitation, nil
}

func (r *InMemoryOrganizationRepository) FindInvitationByHash(tokenHash string) (model.OrganizationInvitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.inviteHash[tokenHash]
	if !ok {
		return model.OrganizationInvitation{}, ErrOrganizationInvitationNotFound
	}
	return r.invites[id], nil
}

func (r *InMemoryOrganizationRepository) ListInvitations(organizationID int64) ([]model.OrganizationInvitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.OrganizationInvitation, 0)
	for _, invitation := range r.invites {
		if invitation.OrganizationID == organizationID {
			items = append(items, invitation)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return items, nil
}

func (r *InMemoryOrganizationRepository) AcceptInvitation(id, userID int64, now time.Time) (model.OrganizationInvitation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	invitation, ok := r.invites[id]
	if !ok || invitation.Status != model.InvitationStatusPending {
		return model.OrganizationInvitation{}, ErrOrganizationInvitationNotFound
	}
	invitation.Status = model.InvitationStatusAccepted
	invitation.AcceptedByUserID = &userID
	invitation.AcceptedAt = &now
	r.invites[id] = invitation
	return invitation, nil
}

func (r *InMemoryOrganizationRepository) RevokeInvitation(organizationID, invitationID int64, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	invitation, ok := r.invites[invitationID]
	if !ok || invitation.OrganizationID != organizationID || invitation.Status != model.InvitationStatusPending {
		return ErrOrganizationInvitationNotFound
	}
	invitation.Status = model.InvitationStatusRevoked
	invitation.RevokedAt = &now
	r.invites[invitationID] = invitation
	return nil
}

func (r *InMemoryOrganizationRepository) CreateTeam(team model.OrganizationTeam) (model.OrganizationTeam, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	team.ID = r.nextTeam
	r.nextTeam++
	r.teams[team.ID] = team
	return team, nil
}

func (r *InMemoryOrganizationRepository) ListTeams(organizationID int64) ([]model.OrganizationTeam, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.OrganizationTeam, 0)
	for _, team := range r.teams {
		if team.OrganizationID == organizationID {
			items = append(items, team)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryOrganizationRepository) AddTeamMember(member model.OrganizationTeamMember) (model.OrganizationTeamMember, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.teams[member.TeamID]; !ok {
		return model.OrganizationTeamMember{}, ErrOrganizationTeamNotFound
	}
	if r.teamMembers[member.TeamID] == nil {
		r.teamMembers[member.TeamID] = make(map[int64]model.OrganizationTeamMember)
	}
	r.teamMembers[member.TeamID][member.UserID] = member
	return member, nil
}

func (r *InMemoryOrganizationRepository) ListTeamMembers(teamID int64) ([]model.OrganizationTeamMember, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.teams[teamID]; !ok {
		return nil, ErrOrganizationTeamNotFound
	}
	items := make([]model.OrganizationTeamMember, 0)
	for _, member := range r.teamMembers[teamID] {
		items = append(items, member)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UserID < items[j].UserID })
	return items, nil
}

func (r *InMemoryOrganizationRepository) CreateDomain(domain model.OrganizationDomain) (model.OrganizationDomain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.domains {
		if existing.OrganizationID == domain.OrganizationID && strings.EqualFold(existing.Domain, domain.Domain) {
			return existing, nil
		}
	}
	domain.ID = r.nextDomain
	r.nextDomain++
	r.domains[domain.ID] = domain
	return domain, nil
}

func (r *InMemoryOrganizationRepository) ListDomains(organizationID int64) ([]model.OrganizationDomain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.OrganizationDomain, 0)
	for _, domain := range r.domains {
		if domain.OrganizationID == organizationID {
			items = append(items, domain)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryOrganizationRepository) VerifyDomain(organizationID, domainID, actorUserID int64, verificationHash string, now time.Time) (model.OrganizationDomain, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	domain, ok := r.domains[domainID]
	if !ok || domain.OrganizationID != organizationID || domain.VerificationHash != verificationHash {
		return model.OrganizationDomain{}, ErrOrganizationDomainNotFound
	}
	domain.VerifiedAt = &now
	domain.VerifiedByUserID = &actorUserID
	r.domains[domainID] = domain
	return domain, nil
}

func (r *InMemoryOrganizationRepository) RecordAudit(event model.OrganizationAuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event.ID = r.nextAudit
	r.nextAudit++
	r.audits = append(r.audits, event)
	return nil
}

func (r *InMemoryOrganizationRepository) ListAudit(organizationID int64, limit int) ([]model.OrganizationAuditEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.OrganizationAuditEvent, 0)
	for i := len(r.audits) - 1; i >= 0 && len(items) < limit; i-- {
		if r.audits[i].OrganizationID == organizationID {
			items = append(items, r.audits[i])
		}
	}
	return items, nil
}
