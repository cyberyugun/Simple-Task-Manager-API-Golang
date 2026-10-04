package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresConnectorSecurityRepository struct{ db *sql.DB }

func NewPostgresConnectorSecurityRepository(db *sql.DB) *PostgresConnectorSecurityRepository {
	return &PostgresConnectorSecurityRepository{db: db}
}

func (r *PostgresConnectorSecurityRepository) CreateOAuthSession(session model.ConnectorOAuthSession) (model.ConnectorOAuthSession, error) {
	scopes, _ := json.Marshal(session.RequestedScopes)
	err := r.db.QueryRow(`
		INSERT INTO integration_oauth_sessions (
			organization_id,connection_id,state_hash,encrypted_verifier,redirect_uri,requested_scopes,
			created_by_user_id,expires_at,created_at
		) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9)
		RETURNING id
	`, session.OrganizationID, session.ConnectionID, session.StateHash, session.EncryptedVerifier,
		session.RedirectURI, string(scopes), session.CreatedByUserID, session.ExpiresAt, session.CreatedAt).Scan(&session.ID)
	return session, err
}

func (r *PostgresConnectorSecurityRepository) ConsumeOAuthSession(stateHash string, now time.Time) (model.ConnectorOAuthSession, error) {
	tx, err := r.db.Begin()
	if err != nil { return model.ConnectorOAuthSession{}, err }
	defer tx.Rollback()
	var item model.ConnectorOAuthSession
	var scopes []byte
	err = tx.QueryRow(`
		SELECT id,organization_id,connection_id,state_hash,encrypted_verifier,redirect_uri,requested_scopes,
			created_by_user_id,expires_at,consumed_at,created_at
		FROM integration_oauth_sessions
		WHERE state_hash=$1 AND consumed_at IS NULL AND expires_at>$2
		FOR UPDATE
	`, stateHash, now).Scan(&item.ID,&item.OrganizationID,&item.ConnectionID,&item.StateHash,&item.EncryptedVerifier,
		&item.RedirectURI,&scopes,&item.CreatedByUserID,&item.ExpiresAt,&item.ConsumedAt,&item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) { return model.ConnectorOAuthSession{}, ErrConnectorOAuthSessionNotFound }
	if err != nil { return model.ConnectorOAuthSession{}, err }
	if err := json.Unmarshal(scopes, &item.RequestedScopes); err != nil { return model.ConnectorOAuthSession{}, err }
	if _, err := tx.Exec(`UPDATE integration_oauth_sessions SET consumed_at=$2 WHERE id=$1`, item.ID, now); err != nil {
		return model.ConnectorOAuthSession{}, err
	}
	item.ConsumedAt = &now
	if err := tx.Commit(); err != nil { return model.ConnectorOAuthSession{}, err }
	return item, nil
}

func (r *PostgresConnectorSecurityRepository) UpsertCredentialMetadata(item model.ConnectorCredentialMetadata) (model.ConnectorCredentialMetadata, error) {
	scopes, _ := json.Marshal(item.GrantedScopes)
	var raw []byte
	err := r.db.QueryRow(`
		INSERT INTO integration_credential_metadata (
			connection_id,organization_id,secret_backend,secret_ref,key_version,credential_version,status,
			granted_scopes,expires_at,last_refresh_at,last_validated_at,rotated_at,revoked_at,updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (connection_id) DO UPDATE SET
			secret_backend=EXCLUDED.secret_backend,secret_ref=EXCLUDED.secret_ref,key_version=EXCLUDED.key_version,
			credential_version=EXCLUDED.credential_version,status=EXCLUDED.status,granted_scopes=EXCLUDED.granted_scopes,
			expires_at=EXCLUDED.expires_at,last_refresh_at=EXCLUDED.last_refresh_at,last_validated_at=EXCLUDED.last_validated_at,
			rotated_at=EXCLUDED.rotated_at,revoked_at=EXCLUDED.revoked_at,updated_at=EXCLUDED.updated_at
		RETURNING connection_id,organization_id,secret_backend,secret_ref,key_version,credential_version,status,
			granted_scopes,expires_at,last_refresh_at,last_validated_at,rotated_at,revoked_at,updated_at
	`, item.ConnectionID,item.OrganizationID,item.SecretBackend,item.SecretRef,item.KeyVersion,item.CredentialVersion,
		item.Status,string(scopes),item.ExpiresAt,item.LastRefreshAt,item.LastValidatedAt,item.RotatedAt,item.RevokedAt,item.UpdatedAt).
		Scan(&item.ConnectionID,&item.OrganizationID,&item.SecretBackend,&item.SecretRef,&item.KeyVersion,&item.CredentialVersion,
			&item.Status,&raw,&item.ExpiresAt,&item.LastRefreshAt,&item.LastValidatedAt,&item.RotatedAt,&item.RevokedAt,&item.UpdatedAt)
	if err != nil { return model.ConnectorCredentialMetadata{}, err }
	if err := json.Unmarshal(raw,&item.GrantedScopes); err != nil { return model.ConnectorCredentialMetadata{}, err }
	return item,nil
}

func (r *PostgresConnectorSecurityRepository) GetCredentialMetadata(organizationID, connectionID int64) (model.ConnectorCredentialMetadata, error) {
	var item model.ConnectorCredentialMetadata
	var raw []byte
	err := r.db.QueryRow(`
		SELECT connection_id,organization_id,secret_backend,secret_ref,key_version,credential_version,status,
			granted_scopes,expires_at,last_refresh_at,last_validated_at,rotated_at,revoked_at,updated_at
		FROM integration_credential_metadata WHERE organization_id=$1 AND connection_id=$2
	`,organizationID,connectionID).Scan(&item.ConnectionID,&item.OrganizationID,&item.SecretBackend,&item.SecretRef,
		&item.KeyVersion,&item.CredentialVersion,&item.Status,&raw,&item.ExpiresAt,&item.LastRefreshAt,
		&item.LastValidatedAt,&item.RotatedAt,&item.RevokedAt,&item.UpdatedAt)
	if errors.Is(err,sql.ErrNoRows){ return model.ConnectorCredentialMetadata{},ErrConnectorCredentialNotFound }
	if err != nil { return model.ConnectorCredentialMetadata{},err }
	if err:=json.Unmarshal(raw,&item.GrantedScopes);err!=nil{return model.ConnectorCredentialMetadata{},err}
	return item,nil
}

func (r *PostgresConnectorSecurityRepository) ListCredentialsDue(before time.Time, limit int) ([]model.ConnectorCredentialMetadata,error){
	rows,err:=r.db.Query(`
		SELECT connection_id,organization_id,secret_backend,secret_ref,key_version,credential_version,status,
			granted_scopes,expires_at,last_refresh_at,last_validated_at,rotated_at,revoked_at,updated_at
		FROM integration_credential_metadata
		WHERE status='active' AND expires_at IS NOT NULL AND expires_at <= $1
		ORDER BY expires_at, connection_id LIMIT $2
	`,before,limit)
	if err!=nil{return nil,err}
	defer rows.Close()
	items:=[]model.ConnectorCredentialMetadata{}
	for rows.Next(){
		var item model.ConnectorCredentialMetadata; var raw []byte
		if err:=rows.Scan(&item.ConnectionID,&item.OrganizationID,&item.SecretBackend,&item.SecretRef,&item.KeyVersion,
			&item.CredentialVersion,&item.Status,&raw,&item.ExpiresAt,&item.LastRefreshAt,&item.LastValidatedAt,
			&item.RotatedAt,&item.RevokedAt,&item.UpdatedAt);err!=nil{return nil,err}
		if err:=json.Unmarshal(raw,&item.GrantedScopes);err!=nil{return nil,err}
		items=append(items,item)
	}
	return items,rows.Err()
}

func (r *PostgresConnectorSecurityRepository) RecordCredentialAccess(item model.ConnectorCredentialAccess) error {
	_,err:=r.db.Exec(`
		INSERT INTO integration_credential_access_audit
			(organization_id,connection_id,actor_user_id,action,secret_backend,secret_ref,created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`,item.OrganizationID,item.ConnectionID,item.ActorUserID,item.Action,item.SecretBackend,item.SecretRef,item.CreatedAt)
	return err
}

func (r *PostgresConnectorSecurityRepository) ListCredentialAccess(organizationID, connectionID int64, limit int)([]model.ConnectorCredentialAccess,error){
	rows,err:=r.db.Query(`
		SELECT id,organization_id,connection_id,actor_user_id,action,secret_backend,secret_ref,created_at
		FROM integration_credential_access_audit
		WHERE organization_id=$1 AND ($2=0 OR connection_id=$2)
		ORDER BY created_at DESC,id DESC LIMIT $3
	`,organizationID,connectionID,limit)
	if err!=nil{return nil,err}; defer rows.Close()
	items:=[]model.ConnectorCredentialAccess{}
	for rows.Next(){var item model.ConnectorCredentialAccess
		if err:=rows.Scan(&item.ID,&item.OrganizationID,&item.ConnectionID,&item.ActorUserID,&item.Action,&item.SecretBackend,&item.SecretRef,&item.CreatedAt);err!=nil{return nil,err}
		items=append(items,item)
	}
	return items,rows.Err()
}
