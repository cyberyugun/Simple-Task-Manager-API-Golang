package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"go-simple-task-api/internal/model"
)

type PostgresWorkspaceRepository struct {
	db *sql.DB
}

func NewPostgresWorkspaceRepository(db *sql.DB) *PostgresWorkspaceRepository {
	return &PostgresWorkspaceRepository{db: db}
}

func (r *PostgresWorkspaceRepository) ResolveDefault(userID int64, now time.Time) (model.WorkspaceAccess, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return model.WorkspaceAccess{}, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO workspaces (name, created_by_user_id, is_personal, created_at, updated_at)
		VALUES ('Personal Workspace', $1, TRUE, $2, $2)
		ON CONFLICT (created_by_user_id) WHERE is_personal = TRUE DO NOTHING
	`, userID, now); err != nil {
		return model.WorkspaceAccess{}, err
	}
	if _, err := tx.Exec(`
		INSERT INTO workspace_members (workspace_id, user_id, role, created_at, updated_at)
		SELECT id, $1, 'owner', $2, $2
		FROM workspaces
		WHERE created_by_user_id = $1 AND is_personal = TRUE
		ON CONFLICT (workspace_id, user_id) DO NOTHING
	`, userID, now); err != nil {
		return model.WorkspaceAccess{}, err
	}

	access, err := scanWorkspaceAccess(tx.QueryRow(`
		SELECT w.id, w.name, w.created_by_user_id, w.is_personal, w.created_at, w.updated_at, m.role
		FROM workspaces w
		JOIN workspace_members m ON m.workspace_id = w.id
		WHERE w.created_by_user_id = $1 AND w.is_personal = TRUE AND m.user_id = $1
	`, userID))
	if err != nil {
		return model.WorkspaceAccess{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.WorkspaceAccess{}, err
	}
	return access, nil
}

func (r *PostgresWorkspaceRepository) ResolveAccess(userID, workspaceID int64, now time.Time) (model.WorkspaceAccess, error) {
	if workspaceID <= 0 {
		return r.ResolveDefault(userID, now)
	}
	access, err := scanWorkspaceAccess(r.db.QueryRow(`
		SELECT w.id, w.name, w.created_by_user_id, w.is_personal, w.created_at, w.updated_at, m.role
		FROM workspaces w
		JOIN workspace_members m ON m.workspace_id = w.id
		WHERE w.id = $1 AND m.user_id = $2
	`, workspaceID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkspaceAccess{}, ErrWorkspaceNotFound
	}
	return access, err
}

func (r *PostgresWorkspaceRepository) ListForUser(userID int64, now time.Time) ([]model.WorkspaceAccess, error) {
	if _, err := r.ResolveDefault(userID, now); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(`
		SELECT w.id, w.name, w.created_by_user_id, w.is_personal, w.created_at, w.updated_at, m.role
		FROM workspaces w
		JOIN workspace_members m ON m.workspace_id = w.id
		WHERE m.user_id = $1
		ORDER BY w.is_personal DESC, w.id ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.WorkspaceAccess, 0)
	for rows.Next() {
		item, err := scanWorkspaceAccess(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresWorkspaceRepository) Create(userID int64, name string, now time.Time) (model.WorkspaceAccess, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return model.WorkspaceAccess{}, err
	}
	defer tx.Rollback()

	workspace, err := scanWorkspace(tx.QueryRow(`
		INSERT INTO workspaces (name, created_by_user_id, is_personal, created_at, updated_at)
		VALUES ($1, $2, FALSE, $3, $3)
		RETURNING id, name, created_by_user_id, is_personal, created_at, updated_at
	`, name, userID, now))
	if err != nil {
		return model.WorkspaceAccess{}, err
	}
	if _, err := tx.Exec(`
		INSERT INTO workspace_members (workspace_id, user_id, role, created_at, updated_at)
		VALUES ($1, $2, 'owner', $3, $3)
	`, workspace.ID, userID, now); err != nil {
		return model.WorkspaceAccess{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.WorkspaceAccess{}, err
	}
	return model.WorkspaceAccess{Workspace: workspace, Role: model.WorkspaceRoleOwner}, nil
}

func (r *PostgresWorkspaceRepository) Rename(workspaceID int64, name string, now time.Time) (model.Workspace, error) {
	workspace, err := scanWorkspace(r.db.QueryRow(`
		UPDATE workspaces SET name = $2, updated_at = $3
		WHERE id = $1
		RETURNING id, name, created_by_user_id, is_personal, created_at, updated_at
	`, workspaceID, name, now))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Workspace{}, ErrWorkspaceNotFound
	}
	return workspace, err
}

func (r *PostgresWorkspaceRepository) Delete(workspaceID int64) error {
	result, err := r.db.Exec(`DELETE FROM workspaces WHERE id = $1`, workspaceID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrWorkspaceNotFound
	}
	return nil
}

func (r *PostgresWorkspaceRepository) ListMembers(workspaceID int64) ([]model.WorkspaceMember, error) {
	rows, err := r.db.Query(`
		SELECT m.workspace_id, m.user_id, u.name, u.email, m.role, m.created_at, m.updated_at
		FROM workspace_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id = $1
		ORDER BY m.user_id
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.WorkspaceMember, 0)
	for rows.Next() {
		var item model.WorkspaceMember
		if err := rows.Scan(&item.WorkspaceID, &item.UserID, &item.Name, &item.Email, &item.Role, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresWorkspaceRepository) AddMember(workspaceID, userID int64, role string, now time.Time) (model.WorkspaceMember, error) {
	var member model.WorkspaceMember
	err := r.db.QueryRow(`
		INSERT INTO workspace_members (workspace_id, user_id, role, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $4)
		RETURNING workspace_id, user_id, role, created_at, updated_at
	`, workspaceID, userID, role, now).Scan(
		&member.WorkspaceID, &member.UserID, &member.Role, &member.CreatedAt, &member.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.WorkspaceMember{}, ErrWorkspaceMemberExists
		}
	}
	return member, err
}

func (r *PostgresWorkspaceRepository) UpdateMemberRole(workspaceID, userID int64, role string, now time.Time) (model.WorkspaceMember, error) {
	var member model.WorkspaceMember
	err := r.db.QueryRow(`
		UPDATE workspace_members SET role = $3, updated_at = $4
		WHERE workspace_id = $1 AND user_id = $2
		RETURNING workspace_id, user_id, role, created_at, updated_at
	`, workspaceID, userID, role, now).Scan(
		&member.WorkspaceID, &member.UserID, &member.Role, &member.CreatedAt, &member.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WorkspaceMember{}, ErrWorkspaceMemberNotFound
	}
	return member, err
}

func (r *PostgresWorkspaceRepository) RemoveMember(workspaceID, userID int64) error {
	result, err := r.db.Exec(`DELETE FROM workspace_members WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrWorkspaceMemberNotFound
	}
	return nil
}

func (r *PostgresWorkspaceRepository) RecordAudit(event model.AuditEvent) error {
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO audit_events (workspace_id, actor_user_id, action, resource_type, resource_id, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
	`, event.WorkspaceID, event.ActorUserID, event.Action, event.ResourceType, event.ResourceID, string(metadata), event.CreatedAt)
	return err
}

func (r *PostgresWorkspaceRepository) ListAudit(workspaceID int64, limit int) ([]model.AuditEvent, error) {
	rows, err := r.db.Query(`
		SELECT id, workspace_id, actor_user_id, action, resource_type, resource_id, metadata, created_at
		FROM audit_events
		WHERE workspace_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AuditEvent, 0)
	for rows.Next() {
		var item model.AuditEvent
		var raw []byte
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.ActorUserID, &item.Action, &item.ResourceType, &item.ResourceID, &raw, &item.CreatedAt); err != nil {
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

type workspaceAccessScanner interface {
	Scan(dest ...any) error
}

func scanWorkspaceAccess(scanner workspaceAccessScanner) (model.WorkspaceAccess, error) {
	var access model.WorkspaceAccess
	err := scanner.Scan(
		&access.ID, &access.Name, &access.CreatedByUserID, &access.IsPersonal,
		&access.CreatedAt, &access.UpdatedAt, &access.Role,
	)
	return access, err
}

func scanWorkspace(scanner workspaceAccessScanner) (model.Workspace, error) {
	var workspace model.Workspace
	err := scanner.Scan(
		&workspace.ID, &workspace.Name, &workspace.CreatedByUserID, &workspace.IsPersonal,
		&workspace.CreatedAt, &workspace.UpdatedAt,
	)
	return workspace, err
}
