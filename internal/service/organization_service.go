package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrOrganizationForbidden       = errors.New("organization action is forbidden")
	ErrOrganizationInactive        = errors.New("organization is not active")
	ErrInvalidOrganization         = errors.New("invalid organization")
	ErrInvalidOrganizationRole     = errors.New("invalid organization role")
	ErrOrganizationMemberQuota     = errors.New("organization member quota exceeded")
	ErrOrganizationWorkspaceQuota  = errors.New("organization workspace quota exceeded")
	ErrOrganizationOwnerMembership = errors.New("organization owner membership cannot be removed or changed directly")
	ErrInvalidOrganizationInvite   = errors.New("invalid or expired organization invitation")
	ErrInvalidOrganizationDomain   = errors.New("invalid organization domain verification")
	ErrInvalidOrganizationTeam     = errors.New("invalid organization team")
	ErrOrganizationPlanLimit       = errors.New("organization quota exceeds active billing plan limits")
)

type OrganizationEntitlementProvider interface {
	EffectiveLimits(organizationID int64, at time.Time) (model.BillingLimits, error)
}

type OrganizationService struct {
	repo         repository.OrganizationRepository
	users        repository.UserRepository
	workspaces   repository.WorkspaceRepository
	entitlements OrganizationEntitlementProvider
}

func NewOrganizationService(
	repo repository.OrganizationRepository,
	users repository.UserRepository,
	workspaces repository.WorkspaceRepository,
) *OrganizationService {
	return &OrganizationService{repo: repo, users: users, workspaces: workspaces}
}

func (s *OrganizationService) SetEntitlementProvider(provider OrganizationEntitlementProvider) {
	s.entitlements = provider
}

func (s *OrganizationService) Create(actorUserID int64, req model.CreateOrganizationRequest) (model.Organization, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return model.Organization{}, ErrInvalidOrganization
	}
	maxWorkspaces := req.MaxWorkspaces
	if maxWorkspaces == 0 {
		maxWorkspaces = 20
	}
	maxMembers := req.MaxMembers
	if maxMembers == 0 {
		maxMembers = 100
	}
	if maxWorkspaces < 1 || maxWorkspaces > 10000 || maxMembers < 1 || maxMembers > 100000 {
		return model.Organization{}, ErrInvalidOrganization
	}
	if req.ParentID != nil {
		if *req.ParentID <= 0 {
			return model.Organization{}, ErrInvalidOrganization
		}
		if _, _, err := s.requireAdmin(actorUserID, *req.ParentID); err != nil {
			return model.Organization{}, err
		}
	}
	now := time.Now()
	item, err := s.repo.CreateOrganization(model.Organization{
		ParentID:        req.ParentID,
		Name:            name,
		Status:          model.OrganizationStatusActive,
		OwnerUserID:     actorUserID,
		MaxWorkspaces:   maxWorkspaces,
		MaxMembers:      maxMembers,
		CreatedByUserID: actorUserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err == nil {
		s.audit(item.ID, &actorUserID, "organization.created", "organization", fmt.Sprint(item.ID), map[string]any{
			"name": item.Name, "parent_id": item.ParentID,
		})
	}
	return item, err
}

func (s *OrganizationService) List(actorUserID int64) ([]model.Organization, error) {
	return s.repo.ListOrganizationsForUser(actorUserID)
}

func (s *OrganizationService) Get(actorUserID, organizationID int64) (model.Organization, error) {
	if _, err := s.repo.GetMember(organizationID, actorUserID); err != nil {
		return model.Organization{}, ErrOrganizationForbidden
	}
	return s.repo.GetOrganization(organizationID)
}

func (s *OrganizationService) UpdateStatus(actorUserID, organizationID int64, req model.UpdateOrganizationStatusRequest) (model.Organization, error) {
	org, err := s.requireOwner(actorUserID, organizationID)
	if err != nil {
		return model.Organization{}, err
	}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status != model.OrganizationStatusActive && status != model.OrganizationStatusSuspended &&
		status != model.OrganizationStatusDeactivated {
		return model.Organization{}, ErrInvalidOrganization
	}
	now := time.Now()
	var deactivatedAt *time.Time
	if status == model.OrganizationStatusDeactivated {
		deactivatedAt = &now
	}
	item, err := s.repo.UpdateOrganizationStatus(org.ID, status, deactivatedAt, now)
	if err == nil {
		s.audit(org.ID, &actorUserID, "organization.status.updated", "organization", fmt.Sprint(org.ID), map[string]any{
			"previous_status": org.Status, "status": status,
		})
	}
	return item, err
}

func (s *OrganizationService) UpdateQuota(actorUserID, organizationID int64, req model.UpdateOrganizationQuotaRequest) (model.Organization, error) {
	if _, err := s.requireOwner(actorUserID, organizationID); err != nil {
		return model.Organization{}, err
	}
	if req.MaxWorkspaces < 1 || req.MaxWorkspaces > 10000 || req.MaxMembers < 1 || req.MaxMembers > 100000 {
		return model.Organization{}, ErrInvalidOrganization
	}
	memberCount, err := s.repo.CountMembers(organizationID)
	if err != nil {
		return model.Organization{}, err
	}
	workspaceCount, err := s.repo.CountWorkspaces(organizationID)
	if err != nil {
		return model.Organization{}, err
	}
	if memberCount > req.MaxMembers || workspaceCount > req.MaxWorkspaces {
		return model.Organization{}, ErrInvalidOrganization
	}
	if s.entitlements != nil {
		limits, err := s.entitlements.EffectiveLimits(organizationID, time.Now().UTC())
		if err != nil {
			return model.Organization{}, err
		}
		if req.MaxMembers > limits.MaxMembers || req.MaxWorkspaces > limits.MaxWorkspaces {
			return model.Organization{}, ErrOrganizationPlanLimit
		}
	}
	item, err := s.repo.UpdateOrganizationQuota(organizationID, req.MaxWorkspaces, req.MaxMembers, time.Now())
	if err == nil {
		s.audit(organizationID, &actorUserID, "organization.quota.updated", "organization", fmt.Sprint(organizationID), map[string]any{
			"max_workspaces": req.MaxWorkspaces, "max_members": req.MaxMembers,
		})
	}
	return item, err
}

func (s *OrganizationService) TransferOwnership(actorUserID, organizationID int64, req model.TransferOrganizationOwnershipRequest) (model.Organization, error) {
	if req.UserID <= 0 || req.UserID == actorUserID {
		return model.Organization{}, ErrInvalidOrganization
	}
	if _, err := s.requireOwner(actorUserID, organizationID); err != nil {
		return model.Organization{}, err
	}
	if _, err := s.repo.GetMember(organizationID, req.UserID); err != nil {
		return model.Organization{}, err
	}
	item, err := s.repo.TransferOwnership(organizationID, actorUserID, req.UserID, time.Now())
	if err == nil {
		s.audit(organizationID, &actorUserID, "organization.ownership.transferred", "user", fmt.Sprint(req.UserID), map[string]any{
			"previous_owner_user_id": actorUserID,
		})
	}
	return item, err
}

func (s *OrganizationService) Directory(actorUserID, organizationID int64) ([]model.OrganizationDirectoryEntry, error) {
	if _, _, err := s.requireMember(actorUserID, organizationID); err != nil {
		return nil, err
	}
	members, err := s.repo.ListMembers(organizationID)
	if err != nil {
		return nil, err
	}
	items := make([]model.OrganizationDirectoryEntry, 0, len(members))
	for _, member := range members {
		user, err := s.users.FindByID(member.UserID)
		if err != nil {
			return nil, err
		}
		items = append(items, model.OrganizationDirectoryEntry{
			OrganizationID: organizationID,
			UserID:         user.ID,
			Name:           user.Name,
			Email:          user.Email,
			Role:           member.Role,
			CreatedAt:      member.CreatedAt,
		})
	}
	return items, nil
}

func (s *OrganizationService) BulkAddMembers(actorUserID, organizationID int64, req model.BulkOrganizationMemberRequest) ([]model.OrganizationMember, error) {
	if _, _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	role, err := normalizeOrganizationRole(req.Role, false)
	if err != nil || len(req.UserIDs) == 0 || len(req.UserIDs) > 200 {
		return nil, ErrInvalidOrganizationRole
	}
	org, err := s.repo.GetOrganization(organizationID)
	if err != nil {
		return nil, err
	}
	count, err := s.repo.CountMembers(organizationID)
	if err != nil {
		return nil, err
	}
	unique := make(map[int64]bool)
	for _, userID := range req.UserIDs {
		if userID <= 0 {
			return nil, ErrInvalidOrganization
		}
		if _, err := s.users.FindByID(userID); err != nil {
			return nil, err
		}
		if _, err := s.repo.GetMember(organizationID, userID); errors.Is(err, repository.ErrOrganizationMemberNotFound) {
			unique[userID] = true
		}
	}
	maxMembers, _, err := s.effectiveOrganizationLimits(org)
	if err != nil {
		return nil, err
	}
	if count+len(unique) > maxMembers {
		return nil, ErrOrganizationMemberQuota
	}
	now := time.Now()
	items := make([]model.OrganizationMember, 0, len(req.UserIDs))
	for _, userID := range req.UserIDs {
		member, err := s.repo.UpsertMember(model.OrganizationMember{
			OrganizationID: organizationID, UserID: userID, Role: role,
			CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return nil, err
		}
		items = append(items, member)
	}
	s.audit(organizationID, &actorUserID, "organization.members.bulk_upserted", "organization", fmt.Sprint(organizationID), map[string]any{
		"count": len(items), "role": role,
	})
	return items, nil
}

func (s *OrganizationService) RemoveMember(actorUserID, organizationID, userID int64) error {
	_, actor, err := s.requireAdmin(actorUserID, organizationID)
	if err != nil {
		return err
	}
	target, err := s.repo.GetMember(organizationID, userID)
	if err != nil {
		return err
	}
	if target.Role == model.OrganizationRoleOwner {
		return ErrOrganizationOwnerMembership
	}
	if actor.Role == model.OrganizationRoleDelegatedAdmin && target.Role != model.OrganizationRoleMember {
		return ErrOrganizationForbidden
	}
	if err := s.repo.RemoveMember(organizationID, userID); err != nil {
		return err
	}
	s.audit(organizationID, &actorUserID, "organization.member.removed", "user", fmt.Sprint(userID), map[string]any{
		"previous_role": target.Role,
	})
	return nil
}

func (s *OrganizationService) AttachWorkspace(actorUserID, organizationID int64, req model.AttachOrganizationWorkspaceRequest) (model.OrganizationWorkspace, error) {
	org, _, err := s.requireAdmin(actorUserID, organizationID)
	if err != nil {
		return model.OrganizationWorkspace{}, err
	}
	if req.WorkspaceID <= 0 {
		return model.OrganizationWorkspace{}, ErrInvalidOrganization
	}
	access, err := s.workspaces.ResolveAccess(actorUserID, req.WorkspaceID, time.Now())
	if err != nil {
		return model.OrganizationWorkspace{}, err
	}
	if access.Role != model.WorkspaceRoleOwner && access.Role != model.WorkspaceRoleAdmin {
		return model.OrganizationWorkspace{}, ErrOrganizationForbidden
	}
	count, err := s.repo.CountWorkspaces(organizationID)
	if err != nil {
		return model.OrganizationWorkspace{}, err
	}
	_, maxWorkspaces, err := s.effectiveOrganizationLimits(org)
	if err != nil {
		return model.OrganizationWorkspace{}, err
	}
	if count >= maxWorkspaces {
		return model.OrganizationWorkspace{}, ErrOrganizationWorkspaceQuota
	}
	item, err := s.repo.AttachWorkspace(model.OrganizationWorkspace{
		OrganizationID: organizationID, WorkspaceID: req.WorkspaceID,
		AttachedByID: actorUserID, AttachedAt: time.Now(),
	})
	if err == nil {
		s.audit(organizationID, &actorUserID, "organization.workspace.attached", "workspace", fmt.Sprint(req.WorkspaceID), nil)
	}
	return item, err
}

func (s *OrganizationService) ListWorkspaces(actorUserID, organizationID int64) ([]model.OrganizationWorkspace, error) {
	if _, _, err := s.requireMember(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListWorkspaces(organizationID)
}

func (s *OrganizationService) DetachWorkspace(actorUserID, organizationID, workspaceID int64) error {
	if _, _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return err
	}
	if err := s.repo.DetachWorkspace(organizationID, workspaceID); err != nil {
		return err
	}
	s.audit(organizationID, &actorUserID, "organization.workspace.detached", "workspace", fmt.Sprint(workspaceID), nil)
	return nil
}

func (s *OrganizationService) CreateInvitation(actorUserID, organizationID int64, req model.CreateOrganizationInvitationRequest) (model.OrganizationInvitationSecret, error) {
	org, _, err := s.requireAdmin(actorUserID, organizationID)
	if err != nil {
		return model.OrganizationInvitationSecret{}, err
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if _, err := mail.ParseAddress(email); err != nil {
		return model.OrganizationInvitationSecret{}, ErrInvalidOrganizationInvite
	}
	role, err := normalizeOrganizationRole(req.Role, false)
	if err != nil {
		return model.OrganizationInvitationSecret{}, err
	}
	count, err := s.repo.CountMembers(organizationID)
	if err != nil {
		return model.OrganizationInvitationSecret{}, err
	}
	maxMembers, _, err := s.effectiveOrganizationLimits(org)
	if err != nil {
		return model.OrganizationInvitationSecret{}, err
	}
	if count >= maxMembers {
		return model.OrganizationInvitationSecret{}, ErrOrganizationMemberQuota
	}
	hours := req.ExpiresInHours
	if hours == 0 {
		hours = 72
	}
	if hours < 1 || hours > 720 {
		return model.OrganizationInvitationSecret{}, ErrInvalidOrganizationInvite
	}
	token, tokenHash, err := randomOpaqueSecret()
	if err != nil {
		return model.OrganizationInvitationSecret{}, err
	}
	now := time.Now()
	invitation, err := s.repo.CreateInvitation(model.OrganizationInvitation{
		OrganizationID: organizationID, Email: email, Role: role, TokenHash: tokenHash,
		Status: model.InvitationStatusPending, InvitedByUserID: actorUserID,
		ExpiresAt: now.Add(time.Duration(hours) * time.Hour), CreatedAt: now,
	})
	if err != nil {
		return model.OrganizationInvitationSecret{}, err
	}
	s.audit(organizationID, &actorUserID, "organization.invitation.created", "invitation", fmt.Sprint(invitation.ID), map[string]any{
		"email": email, "role": role, "expires_at": invitation.ExpiresAt,
	})
	return model.OrganizationInvitationSecret{Invitation: invitation, Token: token}, nil
}

func (s *OrganizationService) ListInvitations(actorUserID, organizationID int64) ([]model.OrganizationInvitation, error) {
	if _, _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListInvitations(organizationID)
}

func (s *OrganizationService) RevokeInvitation(actorUserID, organizationID, invitationID int64) error {
	if _, _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return err
	}
	if err := s.repo.RevokeInvitation(organizationID, invitationID, time.Now()); err != nil {
		return err
	}
	s.audit(organizationID, &actorUserID, "organization.invitation.revoked", "invitation", fmt.Sprint(invitationID), nil)
	return nil
}

func (s *OrganizationService) AcceptInvitation(actorUserID int64, req model.AcceptOrganizationInvitationRequest) (model.OrganizationMember, error) {
	token := strings.TrimSpace(req.Token)
	if token == "" {
		return model.OrganizationMember{}, ErrInvalidOrganizationInvite
	}
	sum := sha256.Sum256([]byte(token))
	invitation, err := s.repo.FindInvitationByHash(hex.EncodeToString(sum[:]))
	if err != nil || invitation.Status != model.InvitationStatusPending || !invitation.ExpiresAt.After(time.Now()) {
		return model.OrganizationMember{}, ErrInvalidOrganizationInvite
	}
	user, err := s.users.FindByID(actorUserID)
	if err != nil {
		return model.OrganizationMember{}, err
	}
	if !strings.EqualFold(user.Email, invitation.Email) {
		return model.OrganizationMember{}, ErrOrganizationForbidden
	}
	org, err := s.repo.GetOrganization(invitation.OrganizationID)
	if err != nil {
		return model.OrganizationMember{}, err
	}
	if org.Status != model.OrganizationStatusActive {
		return model.OrganizationMember{}, ErrOrganizationInactive
	}
	count, err := s.repo.CountMembers(org.ID)
	if err != nil {
		return model.OrganizationMember{}, err
	}
	maxMembers, _, err := s.effectiveOrganizationLimits(org)
	if err != nil {
		return model.OrganizationMember{}, err
	}
	if _, existingErr := s.repo.GetMember(org.ID, actorUserID); errors.Is(existingErr, repository.ErrOrganizationMemberNotFound) && count >= maxMembers {
		return model.OrganizationMember{}, ErrOrganizationMemberQuota
	}
	now := time.Now()
	if _, err := s.repo.AcceptInvitation(invitation.ID, actorUserID, now); err != nil {
		return model.OrganizationMember{}, ErrInvalidOrganizationInvite
	}
	member, err := s.repo.UpsertMember(model.OrganizationMember{
		OrganizationID: org.ID, UserID: actorUserID, Role: invitation.Role,
		CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		s.audit(org.ID, &actorUserID, "organization.invitation.accepted", "invitation", fmt.Sprint(invitation.ID), map[string]any{
			"role": invitation.Role,
		})
	}
	return member, err
}

func (s *OrganizationService) CreateTeam(actorUserID, organizationID int64, req model.CreateOrganizationTeamRequest) (model.OrganizationTeam, error) {
	if _, _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.OrganizationTeam{}, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 120 {
		return model.OrganizationTeam{}, ErrInvalidOrganizationTeam
	}
	now := time.Now()
	item, err := s.repo.CreateTeam(model.OrganizationTeam{
		OrganizationID: organizationID, Name: name, Description: strings.TrimSpace(req.Description),
		CreatedByID: actorUserID, CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		s.audit(organizationID, &actorUserID, "organization.team.created", "team", fmt.Sprint(item.ID), map[string]any{"name": name})
	}
	return item, err
}

func (s *OrganizationService) ListTeams(actorUserID, organizationID int64) ([]model.OrganizationTeam, error) {
	if _, _, err := s.requireMember(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListTeams(organizationID)
}

func (s *OrganizationService) AddTeamMember(actorUserID, organizationID, teamID int64, req model.AddOrganizationTeamMemberRequest) (model.OrganizationTeamMember, error) {
	if _, _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.OrganizationTeamMember{}, err
	}
	if _, err := s.repo.GetMember(organizationID, req.UserID); err != nil {
		return model.OrganizationTeamMember{}, err
	}
	teams, err := s.repo.ListTeams(organizationID)
	if err != nil {
		return model.OrganizationTeamMember{}, err
	}
	found := false
	for _, team := range teams {
		if team.ID == teamID {
			found = true
			break
		}
	}
	if !found {
		return model.OrganizationTeamMember{}, repository.ErrOrganizationTeamNotFound
	}
	item, err := s.repo.AddTeamMember(model.OrganizationTeamMember{TeamID: teamID, UserID: req.UserID, CreatedAt: time.Now()})
	if err == nil {
		s.audit(organizationID, &actorUserID, "organization.team.member_added", "team", fmt.Sprint(teamID), map[string]any{"user_id": req.UserID})
	}
	return item, err
}

func (s *OrganizationService) ListTeamMembers(actorUserID, organizationID, teamID int64) ([]model.OrganizationTeamMember, error) {
	if _, _, err := s.requireMember(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListTeamMembers(teamID)
}

func (s *OrganizationService) CreateDomain(actorUserID, organizationID int64, req model.CreateOrganizationDomainRequest) (model.OrganizationDomainSecret, error) {
	if _, _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.OrganizationDomainSecret{}, err
	}
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if domain == "" || strings.Contains(domain, "://") || strings.ContainsAny(domain, "/ @") || !strings.Contains(domain, ".") {
		return model.OrganizationDomainSecret{}, ErrInvalidOrganizationDomain
	}
	token, tokenHash, err := randomOpaqueSecret()
	if err != nil {
		return model.OrganizationDomainSecret{}, err
	}
	item, err := s.repo.CreateDomain(model.OrganizationDomain{
		OrganizationID: organizationID, Domain: domain, VerificationHash: tokenHash,
		CreatedByUserID: actorUserID, CreatedAt: time.Now(),
	})
	if err != nil {
		return model.OrganizationDomainSecret{}, err
	}
	s.audit(organizationID, &actorUserID, "organization.domain.created", "domain", fmt.Sprint(item.ID), map[string]any{"domain": domain})
	return model.OrganizationDomainSecret{Domain: item, Token: token}, nil
}

func (s *OrganizationService) ListDomains(actorUserID, organizationID int64) ([]model.OrganizationDomain, error) {
	if _, _, err := s.requireMember(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListDomains(organizationID)
}

func (s *OrganizationService) VerifyDomain(actorUserID, organizationID, domainID int64, req model.VerifyOrganizationDomainRequest) (model.OrganizationDomain, error) {
	if _, _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return model.OrganizationDomain{}, err
	}
	token := strings.TrimSpace(req.Token)
	if token == "" {
		return model.OrganizationDomain{}, ErrInvalidOrganizationDomain
	}
	sum := sha256.Sum256([]byte(token))
	item, err := s.repo.VerifyDomain(organizationID, domainID, actorUserID, hex.EncodeToString(sum[:]), time.Now())
	if err != nil {
		return model.OrganizationDomain{}, ErrInvalidOrganizationDomain
	}
	s.audit(organizationID, &actorUserID, "organization.domain.verified", "domain", fmt.Sprint(domainID), map[string]any{"domain": item.Domain})
	return item, nil
}

func (s *OrganizationService) Dashboard(actorUserID, organizationID int64) (model.OrganizationDashboard, error) {
	org, _, err := s.requireMember(actorUserID, organizationID)
	if err != nil {
		return model.OrganizationDashboard{}, err
	}
	memberCount, err := s.repo.CountMembers(organizationID)
	if err != nil {
		return model.OrganizationDashboard{}, err
	}
	workspaceCount, err := s.repo.CountWorkspaces(organizationID)
	if err != nil {
		return model.OrganizationDashboard{}, err
	}
	invitations, err := s.repo.ListInvitations(organizationID)
	if err != nil {
		return model.OrganizationDashboard{}, err
	}
	teams, err := s.repo.ListTeams(organizationID)
	if err != nil {
		return model.OrganizationDashboard{}, err
	}
	domains, err := s.repo.ListDomains(organizationID)
	if err != nil {
		return model.OrganizationDashboard{}, err
	}
	pending := 0
	now := time.Now()
	for _, invitation := range invitations {
		if invitation.Status == model.InvitationStatusPending && invitation.ExpiresAt.After(now) {
			pending++
		}
	}
	verified := 0
	for _, domain := range domains {
		if domain.VerifiedAt != nil {
			verified++
		}
	}
	maxMembers, maxWorkspaces, err := s.effectiveOrganizationLimits(org)
	if err != nil {
		return model.OrganizationDashboard{}, err
	}
	return model.OrganizationDashboard{
		Organization: org, MemberCount: memberCount, WorkspaceCount: workspaceCount,
		PendingInvites: pending, TeamCount: len(teams), VerifiedDomains: verified,
		WorkspaceCapacity: nonNegative(maxWorkspaces - workspaceCount),
		MemberCapacity:    nonNegative(maxMembers - memberCount),
		GeneratedAt:       now,
	}, nil
}

func (s *OrganizationService) Audit(actorUserID, organizationID int64, limit int) ([]model.OrganizationAuditEvent, error) {
	if _, _, err := s.requireAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	return s.repo.ListAudit(organizationID, limit)
}

func (s *OrganizationService) effectiveOrganizationLimits(org model.Organization) (int, int, error) {
	maxMembers := org.MaxMembers
	maxWorkspaces := org.MaxWorkspaces
	if s.entitlements == nil {
		return maxMembers, maxWorkspaces, nil
	}
	limits, err := s.entitlements.EffectiveLimits(org.ID, time.Now().UTC())
	if err != nil {
		return 0, 0, err
	}
	if limits.MaxMembers > 0 && limits.MaxMembers < maxMembers {
		maxMembers = limits.MaxMembers
	}
	if limits.MaxWorkspaces > 0 && limits.MaxWorkspaces < maxWorkspaces {
		maxWorkspaces = limits.MaxWorkspaces
	}
	return maxMembers, maxWorkspaces, nil
}

func (s *OrganizationService) requireOwner(userID, organizationID int64) (model.Organization, error) {
	org, err := s.repo.GetOrganization(organizationID)
	if err != nil {
		return model.Organization{}, err
	}
	if org.OwnerUserID != userID {
		return model.Organization{}, ErrOrganizationForbidden
	}
	return org, nil
}

func (s *OrganizationService) requireAdmin(userID, organizationID int64) (model.Organization, model.OrganizationMember, error) {
	org, member, err := s.requireMember(userID, organizationID)
	if err != nil {
		return model.Organization{}, model.OrganizationMember{}, err
	}
	if org.Status != model.OrganizationStatusActive {
		return model.Organization{}, model.OrganizationMember{}, ErrOrganizationInactive
	}
	if member.Role != model.OrganizationRoleOwner && member.Role != model.OrganizationRoleAdmin &&
		member.Role != model.OrganizationRoleDelegatedAdmin {
		return model.Organization{}, model.OrganizationMember{}, ErrOrganizationForbidden
	}
	return org, member, nil
}

func (s *OrganizationService) requireMember(userID, organizationID int64) (model.Organization, model.OrganizationMember, error) {
	org, err := s.repo.GetOrganization(organizationID)
	if err != nil {
		return model.Organization{}, model.OrganizationMember{}, err
	}
	member, err := s.repo.GetMember(organizationID, userID)
	if err != nil {
		return model.Organization{}, model.OrganizationMember{}, ErrOrganizationForbidden
	}
	return org, member, nil
}

func normalizeOrganizationRole(role string, allowOwner bool) (string, error) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case model.OrganizationRoleOwner:
		if allowOwner {
			return model.OrganizationRoleOwner, nil
		}
	case model.OrganizationRoleAdmin:
		return model.OrganizationRoleAdmin, nil
	case model.OrganizationRoleDelegatedAdmin:
		return model.OrganizationRoleDelegatedAdmin, nil
	case model.OrganizationRoleMember:
		return model.OrganizationRoleMember, nil
	}
	return "", ErrInvalidOrganizationRole
}

func randomOpaqueSecret() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}

func (s *OrganizationService) audit(organizationID int64, actorUserID *int64, action, resourceType, resourceID string, metadata map[string]any) {
	_ = s.repo.RecordAudit(model.OrganizationAuditEvent{
		OrganizationID: organizationID,
		ActorUserID:    actorUserID,
		Action:         action,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Metadata:       metadata,
		CreatedAt:      time.Now(),
	})
}
