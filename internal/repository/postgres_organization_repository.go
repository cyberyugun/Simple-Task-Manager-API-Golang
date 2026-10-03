package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresOrganizationRepository struct {
	db *sql.DB
}

func NewPostgresOrganizationRepository(db *sql.DB) *PostgresOrganizationRepository {
	return &PostgresOrganizationRepository{db: db}
}

func (r *PostgresOrganizationRepository) CreateOrganization(org model.Organization) (model.Organization, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return model.Organization{}, err
	}
	defer tx.Rollback()

	var created model.Organization
	err = tx.QueryRow(`
		INSERT INTO organizations (
			parent_id, name, status, owner_user_id, max_workspaces, max_members,
			created_by_user_id, created_at, updated_at, deactivated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, parent_id, name, status, owner_user_id, max_workspaces, max_members,
			created_by_user_id, created_at, updated_at, deactivated_at
	`, org.ParentID, org.Name, org.Status, org.OwnerUserID, org.MaxWorkspaces, org.MaxMembers,
		org.CreatedByUserID, org.CreatedAt, org.UpdatedAt, org.DeactivatedAt).
		Scan(&created.ID, &created.ParentID, &created.Name, &created.Status, &created.OwnerUserID,
			&created.MaxWorkspaces, &created.MaxMembers, &created.CreatedByUserID,
			&created.CreatedAt, &created.UpdatedAt, &created.DeactivatedAt)
	if err != nil {
		return model.Organization{}, err
	}
	if _, err := tx.Exec(`
		INSERT INTO organization_members (organization_id, user_id, role, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$4)
	`, created.ID, created.OwnerUserID, model.OrganizationRoleOwner, created.CreatedAt); err != nil {
		return model.Organization{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Organization{}, err
	}
	return created, nil
}

func (r *PostgresOrganizationRepository) GetOrganization(id int64) (model.Organization, error) {
	var item model.Organization
	err := r.db.QueryRow(`
		SELECT id, parent_id, name, status, owner_user_id, max_workspaces, max_members,
			created_by_user_id, created_at, updated_at, deactivated_at
		FROM organizations WHERE id = $1
	`, id).Scan(&item.ID, &item.ParentID, &item.Name, &item.Status, &item.OwnerUserID,
		&item.MaxWorkspaces, &item.MaxMembers, &item.CreatedByUserID,
		&item.CreatedAt, &item.UpdatedAt, &item.DeactivatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Organization{}, ErrOrganizationNotFound
	}
	return item, err
}

func (r *PostgresOrganizationRepository) ListOrganizationsForUser(userID int64) ([]model.Organization, error) {
	rows, err := r.db.Query(`
		SELECT o.id, o.parent_id, o.name, o.status, o.owner_user_id, o.max_workspaces, o.max_members,
			o.created_by_user_id, o.created_at, o.updated_at, o.deactivated_at
		FROM organizations o
		JOIN organization_members m ON m.organization_id = o.id
		WHERE m.user_id = $1
		ORDER BY o.id
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.Organization, 0)
	for rows.Next() {
		var item model.Organization
		if err := rows.Scan(&item.ID, &item.ParentID, &item.Name, &item.Status, &item.OwnerUserID,
			&item.MaxWorkspaces, &item.MaxMembers, &item.CreatedByUserID,
			&item.CreatedAt, &item.UpdatedAt, &item.DeactivatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresOrganizationRepository) UpdateOrganizationStatus(id int64, status string, deactivatedAt *time.Time, now time.Time) (model.Organization, error) {
	var item model.Organization
	err := r.db.QueryRow(`
		UPDATE organizations
		SET status = $2, deactivated_at = $3, updated_at = $4
		WHERE id = $1
		RETURNING id, parent_id, name, status, owner_user_id, max_workspaces, max_members,
			created_by_user_id, created_at, updated_at, deactivated_at
	`, id, status, deactivatedAt, now).
		Scan(&item.ID, &item.ParentID, &item.Name, &item.Status, &item.OwnerUserID,
			&item.MaxWorkspaces, &item.MaxMembers, &item.CreatedByUserID,
			&item.CreatedAt, &item.UpdatedAt, &item.DeactivatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Organization{}, ErrOrganizationNotFound
	}
	return item, err
}

func (r *PostgresOrganizationRepository) UpdateOrganizationQuota(id int64, maxWorkspaces, maxMembers int, now time.Time) (model.Organization, error) {
	var item model.Organization
	err := r.db.QueryRow(`
		UPDATE organizations
		SET max_workspaces = $2, max_members = $3, updated_at = $4
		WHERE id = $1
		RETURNING id, parent_id, name, status, owner_user_id, max_workspaces, max_members,
			created_by_user_id, created_at, updated_at, deactivated_at
	`, id, maxWorkspaces, maxMembers, now).
		Scan(&item.ID, &item.ParentID, &item.Name, &item.Status, &item.OwnerUserID,
			&item.MaxWorkspaces, &item.MaxMembers, &item.CreatedByUserID,
			&item.CreatedAt, &item.UpdatedAt, &item.DeactivatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Organization{}, ErrOrganizationNotFound
	}
	return item, err
}

func (r *PostgresOrganizationRepository) TransferOwnership(id, currentOwnerID, newOwnerID int64, now time.Time) (model.Organization, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return model.Organization{}, err
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM organization_members
			WHERE organization_id = $1 AND user_id = $2
		)
	`, id, newOwnerID).Scan(&exists); err != nil {
		return model.Organization{}, err
	}
	if !exists {
		return model.Organization{}, ErrOrganizationMemberNotFound
	}

	result, err := tx.Exec(`
		UPDATE organizations SET owner_user_id = $3, updated_at = $4
		WHERE id = $1 AND owner_user_id = $2
	`, id, currentOwnerID, newOwnerID, now)
	if err != nil {
		return model.Organization{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return model.Organization{}, err
	}
	if count == 0 {
		return model.Organization{}, ErrOrganizationNotFound
	}
	if _, err := tx.Exec(`
		UPDATE organization_members SET role = $3, updated_at = $4
		WHERE organization_id = $1 AND user_id = $2
	`, id, currentOwnerID, model.OrganizationRoleAdmin, now); err != nil {
		return model.Organization{}, err
	}
	if _, err := tx.Exec(`
		UPDATE organization_members SET role = $3, updated_at = $4
		WHERE organization_id = $1 AND user_id = $2
	`, id, newOwnerID, model.OrganizationRoleOwner, now); err != nil {
		return model.Organization{}, err
	}
	var item model.Organization
	if err := tx.QueryRow(`
		SELECT id, parent_id, name, status, owner_user_id, max_workspaces, max_members,
			created_by_user_id, created_at, updated_at, deactivated_at
		FROM organizations WHERE id = $1
	`, id).Scan(&item.ID, &item.ParentID, &item.Name, &item.Status, &item.OwnerUserID,
		&item.MaxWorkspaces, &item.MaxMembers, &item.CreatedByUserID,
		&item.CreatedAt, &item.UpdatedAt, &item.DeactivatedAt); err != nil {
		return model.Organization{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Organization{}, err
	}
	return item, nil
}

func (r *PostgresOrganizationRepository) GetMember(organizationID, userID int64) (model.OrganizationMember, error) {
	var item model.OrganizationMember
	err := r.db.QueryRow(`
		SELECT organization_id, user_id, role, created_at, updated_at
		FROM organization_members WHERE organization_id = $1 AND user_id = $2
	`, organizationID, userID).
		Scan(&item.OrganizationID, &item.UserID, &item.Role, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.OrganizationMember{}, ErrOrganizationMemberNotFound
	}
	return item, err
}

func (r *PostgresOrganizationRepository) ListMembers(organizationID int64) ([]model.OrganizationMember, error) {
	rows, err := r.db.Query(`
		SELECT organization_id, user_id, role, created_at, updated_at
		FROM organization_members WHERE organization_id = $1 ORDER BY user_id
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.OrganizationMember, 0)
	for rows.Next() {
		var item model.OrganizationMember
		if err := rows.Scan(&item.OrganizationID, &item.UserID, &item.Role, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresOrganizationRepository) UpsertMember(member model.OrganizationMember) (model.OrganizationMember, error) {
	var item model.OrganizationMember
	err := r.db.QueryRow(`
		INSERT INTO organization_members (organization_id, user_id, role, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (organization_id, user_id) DO UPDATE SET
			role = EXCLUDED.role,
			updated_at = EXCLUDED.updated_at
		RETURNING organization_id, user_id, role, created_at, updated_at
	`, member.OrganizationID, member.UserID, member.Role, member.CreatedAt, member.UpdatedAt).
		Scan(&item.OrganizationID, &item.UserID, &item.Role, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PostgresOrganizationRepository) RemoveMember(organizationID, userID int64) error {
	result, err := r.db.Exec(`
		DELETE FROM organization_members
		WHERE organization_id = $1 AND user_id = $2 AND role <> 'owner'
	`, organizationID, userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrOrganizationMemberNotFound
	}
	return nil
}

func (r *PostgresOrganizationRepository) CountMembers(organizationID int64) (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM organization_members WHERE organization_id = $1`, organizationID).Scan(&count)
	return count, err
}

func (r *PostgresOrganizationRepository) AttachWorkspace(link model.OrganizationWorkspace) (model.OrganizationWorkspace, error) {
	var item model.OrganizationWorkspace
	err := r.db.QueryRow(`
		INSERT INTO organization_workspaces (organization_id, workspace_id, attached_by_user_id, attached_at)
		VALUES ($1,$2,$3,$4)
		RETURNING organization_id, workspace_id, attached_by_user_id, attached_at
	`, link.OrganizationID, link.WorkspaceID, link.AttachedByID, link.AttachedAt).
		Scan(&item.OrganizationID, &item.WorkspaceID, &item.AttachedByID, &item.AttachedAt)
	if err != nil {
		return model.OrganizationWorkspace{}, err
	}
	return item, nil
}

func (r *PostgresOrganizationRepository) DetachWorkspace(organizationID, workspaceID int64) error {
	result, err := r.db.Exec(`
		DELETE FROM organization_workspaces WHERE organization_id = $1 AND workspace_id = $2
	`, organizationID, workspaceID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrOrganizationNotFound
	}
	return nil
}

func (r *PostgresOrganizationRepository) ListWorkspaces(organizationID int64) ([]model.OrganizationWorkspace, error) {
	rows, err := r.db.Query(`
		SELECT organization_id, workspace_id, attached_by_user_id, attached_at
		FROM organization_workspaces WHERE organization_id = $1 ORDER BY workspace_id
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.OrganizationWorkspace, 0)
	for rows.Next() {
		var item model.OrganizationWorkspace
		if err := rows.Scan(&item.OrganizationID, &item.WorkspaceID, &item.AttachedByID, &item.AttachedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresOrganizationRepository) CountWorkspaces(organizationID int64) (int, error) {
	var count int
	err := r.db.QueryRow(`SELECT COUNT(*) FROM organization_workspaces WHERE organization_id = $1`, organizationID).Scan(&count)
	return count, err
}

func (r *PostgresOrganizationRepository) CreateInvitation(invitation model.OrganizationInvitation) (model.OrganizationInvitation, error) {
	var item model.OrganizationInvitation
	err := r.db.QueryRow(`
		INSERT INTO organization_invitations (
			organization_id, email, role, token_hash, status, invited_by_user_id,
			expires_at, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, organization_id, email, role, token_hash, status, invited_by_user_id,
			expires_at, accepted_by_user_id, created_at, accepted_at, revoked_at
	`, invitation.OrganizationID, invitation.Email, invitation.Role, invitation.TokenHash,
		invitation.Status, invitation.InvitedByUserID, invitation.ExpiresAt, invitation.CreatedAt).
		Scan(&item.ID, &item.OrganizationID, &item.Email, &item.Role, &item.TokenHash,
			&item.Status, &item.InvitedByUserID, &item.ExpiresAt, &item.AcceptedByUserID,
			&item.CreatedAt, &item.AcceptedAt, &item.RevokedAt)
	return item, err
}

func (r *PostgresOrganizationRepository) FindInvitationByHash(tokenHash string) (model.OrganizationInvitation, error) {
	var item model.OrganizationInvitation
	err := r.db.QueryRow(`
		SELECT id, organization_id, email, role, token_hash, status, invited_by_user_id,
			expires_at, accepted_by_user_id, created_at, accepted_at, revoked_at
		FROM organization_invitations WHERE token_hash = $1
	`, tokenHash).
		Scan(&item.ID, &item.OrganizationID, &item.Email, &item.Role, &item.TokenHash,
			&item.Status, &item.InvitedByUserID, &item.ExpiresAt, &item.AcceptedByUserID,
			&item.CreatedAt, &item.AcceptedAt, &item.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.OrganizationInvitation{}, ErrOrganizationInvitationNotFound
	}
	return item, err
}

func (r *PostgresOrganizationRepository) ListInvitations(organizationID int64) ([]model.OrganizationInvitation, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, email, role, token_hash, status, invited_by_user_id,
			expires_at, accepted_by_user_id, created_at, accepted_at, revoked_at
		FROM organization_invitations
		WHERE organization_id = $1 ORDER BY created_at DESC, id DESC
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.OrganizationInvitation, 0)
	for rows.Next() {
		var item model.OrganizationInvitation
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.Email, &item.Role,
			&item.TokenHash, &item.Status, &item.InvitedByUserID, &item.ExpiresAt,
			&item.AcceptedByUserID, &item.CreatedAt, &item.AcceptedAt, &item.RevokedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresOrganizationRepository) AcceptInvitation(id, userID int64, now time.Time) (model.OrganizationInvitation, error) {
	var item model.OrganizationInvitation
	err := r.db.QueryRow(`
		UPDATE organization_invitations
		SET status = 'accepted', accepted_by_user_id = $2, accepted_at = $3
		WHERE id = $1 AND status = 'pending' AND expires_at > $3
		RETURNING id, organization_id, email, role, token_hash, status, invited_by_user_id,
			expires_at, accepted_by_user_id, created_at, accepted_at, revoked_at
	`, id, userID, now).
		Scan(&item.ID, &item.OrganizationID, &item.Email, &item.Role, &item.TokenHash,
			&item.Status, &item.InvitedByUserID, &item.ExpiresAt, &item.AcceptedByUserID,
			&item.CreatedAt, &item.AcceptedAt, &item.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.OrganizationInvitation{}, ErrOrganizationInvitationNotFound
	}
	return item, err
}

func (r *PostgresOrganizationRepository) RevokeInvitation(organizationID, invitationID int64, now time.Time) error {
	result, err := r.db.Exec(`
		UPDATE organization_invitations SET status = 'revoked', revoked_at = $3
		WHERE organization_id = $1 AND id = $2 AND status = 'pending'
	`, organizationID, invitationID, now)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrOrganizationInvitationNotFound
	}
	return nil
}

func (r *PostgresOrganizationRepository) CreateTeam(team model.OrganizationTeam) (model.OrganizationTeam, error) {
	var item model.OrganizationTeam
	err := r.db.QueryRow(`
		INSERT INTO organization_teams (
			organization_id, name, description, created_by_user_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, organization_id, name, description, created_by_user_id, created_at, updated_at
	`, team.OrganizationID, team.Name, team.Description, team.CreatedByID, team.CreatedAt, team.UpdatedAt).
		Scan(&item.ID, &item.OrganizationID, &item.Name, &item.Description,
			&item.CreatedByID, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *PostgresOrganizationRepository) ListTeams(organizationID int64) ([]model.OrganizationTeam, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, name, description, created_by_user_id, created_at, updated_at
		FROM organization_teams WHERE organization_id = $1 ORDER BY id
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.OrganizationTeam, 0)
	for rows.Next() {
		var item model.OrganizationTeam
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.Name, &item.Description,
			&item.CreatedByID, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresOrganizationRepository) AddTeamMember(member model.OrganizationTeamMember) (model.OrganizationTeamMember, error) {
	var item model.OrganizationTeamMember
	err := r.db.QueryRow(`
		INSERT INTO organization_team_members (team_id, user_id, created_at)
		VALUES ($1,$2,$3)
		ON CONFLICT (team_id, user_id) DO UPDATE SET created_at = organization_team_members.created_at
		RETURNING team_id, user_id, created_at
	`, member.TeamID, member.UserID, member.CreatedAt).
		Scan(&item.TeamID, &item.UserID, &item.CreatedAt)
	return item, err
}

func (r *PostgresOrganizationRepository) ListTeamMembers(teamID int64) ([]model.OrganizationTeamMember, error) {
	rows, err := r.db.Query(`
		SELECT team_id, user_id, created_at
		FROM organization_team_members WHERE team_id = $1 ORDER BY user_id
	`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.OrganizationTeamMember, 0)
	for rows.Next() {
		var item model.OrganizationTeamMember
		if err := rows.Scan(&item.TeamID, &item.UserID, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresOrganizationRepository) CreateDomain(domain model.OrganizationDomain) (model.OrganizationDomain, error) {
	var item model.OrganizationDomain
	err := r.db.QueryRow(`
		INSERT INTO organization_domains (
			organization_id, domain, verification_hash, created_by_user_id, created_at
		) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (organization_id, domain) DO UPDATE SET
			verification_hash = EXCLUDED.verification_hash,
			created_by_user_id = EXCLUDED.created_by_user_id,
			created_at = EXCLUDED.created_at,
			verified_at = NULL,
			verified_by_user_id = NULL
		RETURNING id, organization_id, domain, verification_hash, created_by_user_id,
			created_at, verified_at, verified_by_user_id
	`, domain.OrganizationID, domain.Domain, domain.VerificationHash, domain.CreatedByUserID, domain.CreatedAt).
		Scan(&item.ID, &item.OrganizationID, &item.Domain, &item.VerificationHash,
			&item.CreatedByUserID, &item.CreatedAt, &item.VerifiedAt, &item.VerifiedByUserID)
	return item, err
}

func (r *PostgresOrganizationRepository) ListDomains(organizationID int64) ([]model.OrganizationDomain, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, domain, verification_hash, created_by_user_id,
			created_at, verified_at, verified_by_user_id
		FROM organization_domains WHERE organization_id = $1 ORDER BY id
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.OrganizationDomain, 0)
	for rows.Next() {
		var item model.OrganizationDomain
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.Domain, &item.VerificationHash,
			&item.CreatedByUserID, &item.CreatedAt, &item.VerifiedAt, &item.VerifiedByUserID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresOrganizationRepository) VerifyDomain(organizationID, domainID, actorUserID int64, verificationHash string, now time.Time) (model.OrganizationDomain, error) {
	var item model.OrganizationDomain
	err := r.db.QueryRow(`
		UPDATE organization_domains
		SET verified_at = $5, verified_by_user_id = $3
		WHERE organization_id = $1 AND id = $2 AND verification_hash = $4
		RETURNING id, organization_id, domain, verification_hash, created_by_user_id,
			created_at, verified_at, verified_by_user_id
	`, organizationID, domainID, actorUserID, verificationHash, now).
		Scan(&item.ID, &item.OrganizationID, &item.Domain, &item.VerificationHash,
			&item.CreatedByUserID, &item.CreatedAt, &item.VerifiedAt, &item.VerifiedByUserID)
	if errors.Is(err, sql.ErrNoRows) {
		return model.OrganizationDomain{}, ErrOrganizationDomainNotFound
	}
	return item, err
}

func (r *PostgresOrganizationRepository) RecordAudit(event model.OrganizationAuditEvent) error {
	raw, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO organization_audit_events (
			organization_id, actor_user_id, action, resource_type, resource_id, metadata, created_at
		) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7)
	`, event.OrganizationID, event.ActorUserID, event.Action, event.ResourceType,
		event.ResourceID, string(raw), event.CreatedAt)
	return err
}

func (r *PostgresOrganizationRepository) ListAudit(organizationID int64, limit int) ([]model.OrganizationAuditEvent, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, actor_user_id, action, resource_type, resource_id, metadata, created_at
		FROM organization_audit_events
		WHERE organization_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, organizationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.OrganizationAuditEvent, 0)
	for rows.Next() {
		var item model.OrganizationAuditEvent
		var raw []byte
		if err := rows.Scan(&item.ID, &item.OrganizationID, &item.ActorUserID,
			&item.Action, &item.ResourceType, &item.ResourceID, &raw, &item.CreatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &item.Metadata); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
