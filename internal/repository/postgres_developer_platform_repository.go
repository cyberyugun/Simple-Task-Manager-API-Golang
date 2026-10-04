package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresDeveloperPlatformRepository struct {
	db *sql.DB
}

func NewPostgresDeveloperPlatformRepository(db *sql.DB) *PostgresDeveloperPlatformRepository {
	return &PostgresDeveloperPlatformRepository{db: db}
}

func (r *PostgresDeveloperPlatformRepository) CreateApplication(item model.DeveloperApplication) (model.DeveloperApplication, error) {
	scopes, err := json.Marshal(item.AllowedScopes)
	if err != nil {
		return model.DeveloperApplication{}, err
	}
	var raw []byte
	err = r.db.QueryRow("INSERT INTO developer_applications (workspace_id,name,description,status,allowed_scopes,daily_request_limit,monthly_request_limit,sandbox_enabled,created_by_user_id,reviewed_by_user_id,review_note,created_at,updated_at,submitted_at,reviewed_at) VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id,workspace_id,name,description,status,allowed_scopes,daily_request_limit,monthly_request_limit,sandbox_enabled,created_by_user_id,reviewed_by_user_id,review_note,created_at,updated_at,submitted_at,reviewed_at",
		item.WorkspaceID, item.Name, item.Description, item.Status, string(scopes), item.DailyRequestLimit, item.MonthlyRequestLimit,
		item.SandboxEnabled, item.CreatedByUserID, item.ReviewedByUserID, item.ReviewNote, item.CreatedAt, item.UpdatedAt, item.SubmittedAt, item.ReviewedAt,
	).Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Description, &item.Status, &raw, &item.DailyRequestLimit,
		&item.MonthlyRequestLimit, &item.SandboxEnabled, &item.CreatedByUserID, &item.ReviewedByUserID, &item.ReviewNote,
		&item.CreatedAt, &item.UpdatedAt, &item.SubmittedAt, &item.ReviewedAt)
	if err != nil {
		return model.DeveloperApplication{}, err
	}
	if err := json.Unmarshal(raw, &item.AllowedScopes); err != nil {
		return model.DeveloperApplication{}, err
	}
	return item, nil
}

func (r *PostgresDeveloperPlatformRepository) ListApplications(workspaceID int64) ([]model.DeveloperApplication, error) {
	rows, err := r.db.Query("SELECT id,workspace_id,name,description,status,allowed_scopes,daily_request_limit,monthly_request_limit,sandbox_enabled,created_by_user_id,reviewed_by_user_id,review_note,created_at,updated_at,submitted_at,reviewed_at FROM developer_applications WHERE workspace_id=$1 ORDER BY id", workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.DeveloperApplication, 0)
	for rows.Next() {
		item, err := scanDeveloperApplication(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresDeveloperPlatformRepository) GetApplication(workspaceID, appID int64) (model.DeveloperApplication, error) {
	item, err := scanDeveloperApplication(r.db.QueryRow("SELECT id,workspace_id,name,description,status,allowed_scopes,daily_request_limit,monthly_request_limit,sandbox_enabled,created_by_user_id,reviewed_by_user_id,review_note,created_at,updated_at,submitted_at,reviewed_at FROM developer_applications WHERE workspace_id=$1 AND id=$2", workspaceID, appID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.DeveloperApplication{}, ErrDeveloperAppNotFound
	}
	return item, err
}

func (r *PostgresDeveloperPlatformRepository) UpdateApplicationLifecycle(workspaceID, appID int64, status string, reviewerID *int64, note string, submittedAt, reviewedAt *time.Time, now time.Time) (model.DeveloperApplication, error) {
	item, err := scanDeveloperApplication(r.db.QueryRow("UPDATE developer_applications SET status=$3,reviewed_by_user_id=$4,review_note=$5,submitted_at=$6,reviewed_at=$7,updated_at=$8 WHERE workspace_id=$1 AND id=$2 RETURNING id,workspace_id,name,description,status,allowed_scopes,daily_request_limit,monthly_request_limit,sandbox_enabled,created_by_user_id,reviewed_by_user_id,review_note,created_at,updated_at,submitted_at,reviewed_at", workspaceID, appID, status, reviewerID, note, submittedAt, reviewedAt, now))
	if errors.Is(err, sql.ErrNoRows) {
		return model.DeveloperApplication{}, ErrDeveloperAppNotFound
	}
	return item, err
}

func (r *PostgresDeveloperPlatformRepository) CreateCredential(item model.DeveloperCredential) (model.DeveloperCredential, error) {
	scopes, err := json.Marshal(item.Scopes)
	if err != nil {
		return model.DeveloperCredential{}, err
	}
	redirectValues := item.RedirectURIs
	if redirectValues == nil {
		redirectValues = []string{}
	}
	redirects, err := json.Marshal(redirectValues)
	if err != nil {
		return model.DeveloperCredential{}, err
	}
	var rawScopes, rawRedirects []byte
	err = r.db.QueryRow("INSERT INTO developer_app_credentials (app_id,workspace_id,kind,environment,external_id,key_prefix,scopes,redirect_uris,status,rotated_from_id,created_by_user_id,created_at,revoked_at) SELECT $1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,$10,$11,$12,$13 WHERE EXISTS (SELECT 1 FROM developer_applications WHERE id=$1 AND workspace_id=$2) RETURNING id,app_id,workspace_id,kind,environment,external_id,key_prefix,scopes,redirect_uris,status,rotated_from_id,created_by_user_id,created_at,revoked_at",
		item.AppID, item.WorkspaceID, item.Kind, item.Environment, item.ExternalID, item.KeyPrefix, string(scopes), string(redirects),
		item.Status, item.RotatedFromID, item.CreatedByUserID, item.CreatedAt, item.RevokedAt,
	).Scan(&item.ID, &item.AppID, &item.WorkspaceID, &item.Kind, &item.Environment, &item.ExternalID, &item.KeyPrefix,
		&rawScopes, &rawRedirects, &item.Status, &item.RotatedFromID, &item.CreatedByUserID, &item.CreatedAt, &item.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.DeveloperCredential{}, ErrDeveloperAppNotFound
	}
	if err != nil {
		return model.DeveloperCredential{}, err
	}
	if err := json.Unmarshal(rawScopes, &item.Scopes); err != nil {
		return model.DeveloperCredential{}, err
	}
	if err := json.Unmarshal(rawRedirects, &item.RedirectURIs); err != nil {
		return model.DeveloperCredential{}, err
	}
	return item, nil
}

func (r *PostgresDeveloperPlatformRepository) ListCredentials(workspaceID, appID int64) ([]model.DeveloperCredential, error) {
	if _, err := r.GetApplication(workspaceID, appID); err != nil {
		return nil, err
	}
	rows, err := r.db.Query("SELECT id,app_id,workspace_id,kind,environment,external_id,key_prefix,scopes,redirect_uris,status,rotated_from_id,created_by_user_id,created_at,revoked_at FROM developer_app_credentials WHERE workspace_id=$1 AND app_id=$2 ORDER BY id", workspaceID, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.DeveloperCredential, 0)
	for rows.Next() {
		item, err := scanDeveloperCredential(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresDeveloperPlatformRepository) GetCredential(workspaceID, credentialID int64) (model.DeveloperCredential, error) {
	item, err := scanDeveloperCredential(r.db.QueryRow("SELECT id,app_id,workspace_id,kind,environment,external_id,key_prefix,scopes,redirect_uris,status,rotated_from_id,created_by_user_id,created_at,revoked_at FROM developer_app_credentials WHERE workspace_id=$1 AND id=$2", workspaceID, credentialID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.DeveloperCredential{}, ErrDeveloperCredentialNotFound
	}
	return item, err
}

func (r *PostgresDeveloperPlatformRepository) FindCredentialByExternalID(externalID string) (model.DeveloperCredential, model.DeveloperApplication, error) {
	row := r.db.QueryRow("SELECT c.id,c.app_id,c.workspace_id,c.kind,c.environment,c.external_id,c.key_prefix,c.scopes,c.redirect_uris,c.status,c.rotated_from_id,c.created_by_user_id,c.created_at,c.revoked_at,a.id,a.workspace_id,a.name,a.description,a.status,a.allowed_scopes,a.daily_request_limit,a.monthly_request_limit,a.sandbox_enabled,a.created_by_user_id,a.reviewed_by_user_id,a.review_note,a.created_at,a.updated_at,a.submitted_at,a.reviewed_at FROM developer_app_credentials c JOIN developer_applications a ON a.id=c.app_id WHERE c.external_id=$1 AND c.status='active'", externalID)
	return scanDeveloperCredentialAndApp(row)
}

func (r *PostgresDeveloperPlatformRepository) RevokeCredential(workspaceID, credentialID int64, now time.Time) error {
	res, err := r.db.Exec("UPDATE developer_app_credentials SET status='revoked',revoked_at=COALESCE(revoked_at,$3) WHERE workspace_id=$1 AND id=$2", workspaceID, credentialID, now)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrDeveloperCredentialNotFound
	}
	return nil
}

func (r *PostgresDeveloperPlatformRepository) ConsumeRequest(externalID string, workspaceID int64, now time.Time) (model.DeveloperCredential, model.DeveloperApplication, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return model.DeveloperCredential{}, model.DeveloperApplication{}, err
	}
	defer tx.Rollback()
	row := tx.QueryRow("SELECT c.id,c.app_id,c.workspace_id,c.kind,c.environment,c.external_id,c.key_prefix,c.scopes,c.redirect_uris,c.status,c.rotated_from_id,c.created_by_user_id,c.created_at,c.revoked_at,a.id,a.workspace_id,a.name,a.description,a.status,a.allowed_scopes,a.daily_request_limit,a.monthly_request_limit,a.sandbox_enabled,a.created_by_user_id,a.reviewed_by_user_id,a.review_note,a.created_at,a.updated_at,a.submitted_at,a.reviewed_at FROM developer_app_credentials c JOIN developer_applications a ON a.id=c.app_id WHERE c.external_id=$1 AND c.workspace_id=$2 AND c.status='active' FOR UPDATE OF a", externalID, workspaceID)
	credential, app, err := scanDeveloperCredentialAndApp(row)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrDeveloperCredentialNotFound) {
		return model.DeveloperCredential{}, model.DeveloperApplication{}, ErrDeveloperCredentialNotFound
	}
	if err != nil {
		return model.DeveloperCredential{}, model.DeveloperApplication{}, err
	}
	utc := now.UTC()
	day := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	monthStart := time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
	var daily, monthly int64
	err = tx.QueryRow("SELECT COALESCE(SUM(CASE WHEN usage_date=$2::date THEN requests ELSE 0 END),0),COALESCE(SUM(CASE WHEN usage_date >= $3::date THEN requests ELSE 0 END),0) FROM developer_api_usage_daily WHERE app_id=$1 AND usage_date >= $3::date", app.ID, day, monthStart).Scan(&daily, &monthly)
	if err != nil {
		return model.DeveloperCredential{}, model.DeveloperApplication{}, err
	}
	if daily >= app.DailyRequestLimit || monthly >= app.MonthlyRequestLimit {
		return model.DeveloperCredential{}, model.DeveloperApplication{}, ErrDeveloperQuotaExceeded
	}
	_, err = tx.Exec("INSERT INTO developer_api_usage_daily (app_id,workspace_id,usage_date,requests,errors,total_latency_ms,last_request_at) VALUES ($1,$2,$3::date,1,0,0,$4) ON CONFLICT (app_id,usage_date) DO UPDATE SET requests=developer_api_usage_daily.requests+1,last_request_at=EXCLUDED.last_request_at", app.ID, workspaceID, day, utc)
	if err != nil {
		return model.DeveloperCredential{}, model.DeveloperApplication{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.DeveloperCredential{}, model.DeveloperApplication{}, err
	}
	return credential, app, nil
}

func (r *PostgresDeveloperPlatformRepository) RecordResponse(appID, workspaceID int64, statusCode int, latencyMS int64, now time.Time) error {
	if latencyMS < 0 {
		latencyMS = 0
	}
	errorIncrement := 0
	if statusCode >= 400 {
		errorIncrement = 1
	}
	utc := now.UTC()
	day := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	_, err := r.db.Exec("INSERT INTO developer_api_usage_daily (app_id,workspace_id,usage_date,requests,errors,total_latency_ms,last_request_at) VALUES ($1,$2,$3::date,0,$4,$5,$6) ON CONFLICT (app_id,usage_date) DO UPDATE SET errors=developer_api_usage_daily.errors+EXCLUDED.errors,total_latency_ms=developer_api_usage_daily.total_latency_ms+EXCLUDED.total_latency_ms,last_request_at=GREATEST(developer_api_usage_daily.last_request_at,EXCLUDED.last_request_at)", appID, workspaceID, day, errorIncrement, latencyMS, utc)
	return err
}

func (r *PostgresDeveloperPlatformRepository) Usage(workspaceID, appID int64, from, to time.Time) ([]model.DeveloperUsageDaily, error) {
	if _, err := r.GetApplication(workspaceID, appID); err != nil {
		return nil, err
	}
	rows, err := r.db.Query("SELECT app_id,workspace_id,usage_date::text,requests,errors,total_latency_ms,last_request_at FROM developer_api_usage_daily WHERE workspace_id=$1 AND app_id=$2 AND usage_date BETWEEN $3::date AND $4::date ORDER BY usage_date", workspaceID, appID, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.DeveloperUsageDaily, 0)
	for rows.Next() {
		var item model.DeveloperUsageDaily
		if err := rows.Scan(&item.AppID, &item.WorkspaceID, &item.Date, &item.Requests, &item.Errors, &item.TotalLatencyMS, &item.LastRequestAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresDeveloperPlatformRepository) CreateWebhookTest(item model.DeveloperWebhookTest) (model.DeveloperWebhookTest, error) {
	raw, err := json.Marshal(item.Payload)
	if err != nil {
		return model.DeveloperWebhookTest{}, err
	}
	var payloadRaw []byte
	var httpStatus sql.NullInt64
	if item.HTTPStatus > 0 {
		httpStatus = sql.NullInt64{Int64: int64(item.HTTPStatus), Valid: true}
	}
	err = r.db.QueryRow("INSERT INTO developer_webhook_tests (app_id,workspace_id,url,event_type,payload,dry_run,status,http_status,response_preview,error,created_by_user_id,created_at) SELECT $1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11,$12 WHERE EXISTS (SELECT 1 FROM developer_applications WHERE id=$1 AND workspace_id=$2) RETURNING id,app_id,workspace_id,url,event_type,payload,dry_run,status,http_status,response_preview,error,created_by_user_id,created_at",
		item.AppID, item.WorkspaceID, item.URL, item.EventType, string(raw), item.DryRun, item.Status, httpStatus, item.ResponsePreview, item.Error, item.CreatedByUserID, item.CreatedAt,
	).Scan(&item.ID, &item.AppID, &item.WorkspaceID, &item.URL, &item.EventType, &payloadRaw, &item.DryRun, &item.Status, &httpStatus, &item.ResponsePreview, &item.Error, &item.CreatedByUserID, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.DeveloperWebhookTest{}, ErrDeveloperAppNotFound
	}
	if err != nil {
		return model.DeveloperWebhookTest{}, err
	}
	if err := json.Unmarshal(payloadRaw, &item.Payload); err != nil {
		return model.DeveloperWebhookTest{}, err
	}
	if httpStatus.Valid {
		item.HTTPStatus = int(httpStatus.Int64)
	}
	return item, nil
}

func (r *PostgresDeveloperPlatformRepository) ListWebhookTests(workspaceID, appID int64, limit int) ([]model.DeveloperWebhookTest, error) {
	if _, err := r.GetApplication(workspaceID, appID); err != nil {
		return nil, err
	}
	rows, err := r.db.Query("SELECT id,app_id,workspace_id,url,event_type,payload,dry_run,status,http_status,response_preview,error,created_by_user_id,created_at FROM developer_webhook_tests WHERE workspace_id=$1 AND app_id=$2 ORDER BY created_at DESC,id DESC LIMIT $3", workspaceID, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.DeveloperWebhookTest, 0)
	for rows.Next() {
		item, err := scanDeveloperWebhookTest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type developerScanner interface{ Scan(dest ...any) error }

func scanDeveloperApplication(scanner developerScanner) (model.DeveloperApplication, error) {
	var item model.DeveloperApplication
	var scopes []byte
	var reviewer sql.NullInt64
	var submitted, reviewed sql.NullTime
	err := scanner.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Description, &item.Status, &scopes, &item.DailyRequestLimit, &item.MonthlyRequestLimit, &item.SandboxEnabled, &item.CreatedByUserID, &reviewer, &item.ReviewNote, &item.CreatedAt, &item.UpdatedAt, &submitted, &reviewed)
	if err != nil {
		return model.DeveloperApplication{}, err
	}
	if err := json.Unmarshal(scopes, &item.AllowedScopes); err != nil {
		return model.DeveloperApplication{}, err
	}
	if reviewer.Valid {
		v := reviewer.Int64
		item.ReviewedByUserID = &v
	}
	if submitted.Valid {
		v := submitted.Time
		item.SubmittedAt = &v
	}
	if reviewed.Valid {
		v := reviewed.Time
		item.ReviewedAt = &v
	}
	return item, nil
}

func scanDeveloperCredential(scanner developerScanner) (model.DeveloperCredential, error) {
	var item model.DeveloperCredential
	var scopes, redirects []byte
	var rotated sql.NullInt64
	var revoked sql.NullTime
	err := scanner.Scan(&item.ID, &item.AppID, &item.WorkspaceID, &item.Kind, &item.Environment, &item.ExternalID, &item.KeyPrefix, &scopes, &redirects, &item.Status, &rotated, &item.CreatedByUserID, &item.CreatedAt, &revoked)
	if err != nil {
		return model.DeveloperCredential{}, err
	}
	if err := json.Unmarshal(scopes, &item.Scopes); err != nil {
		return model.DeveloperCredential{}, err
	}
	if err := json.Unmarshal(redirects, &item.RedirectURIs); err != nil {
		return model.DeveloperCredential{}, err
	}
	if rotated.Valid {
		v := rotated.Int64
		item.RotatedFromID = &v
	}
	if revoked.Valid {
		v := revoked.Time
		item.RevokedAt = &v
	}
	return item, nil
}

func scanDeveloperCredentialAndApp(scanner developerScanner) (model.DeveloperCredential, model.DeveloperApplication, error) {
	var c model.DeveloperCredential
	var a model.DeveloperApplication
	var cs, redirects, as []byte
	var rotated, reviewer sql.NullInt64
	var revoked, submitted, reviewed sql.NullTime
	err := scanner.Scan(&c.ID, &c.AppID, &c.WorkspaceID, &c.Kind, &c.Environment, &c.ExternalID, &c.KeyPrefix, &cs, &redirects, &c.Status, &rotated, &c.CreatedByUserID, &c.CreatedAt, &revoked, &a.ID, &a.WorkspaceID, &a.Name, &a.Description, &a.Status, &as, &a.DailyRequestLimit, &a.MonthlyRequestLimit, &a.SandboxEnabled, &a.CreatedByUserID, &reviewer, &a.ReviewNote, &a.CreatedAt, &a.UpdatedAt, &submitted, &reviewed)
	if errors.Is(err, sql.ErrNoRows) {
		return model.DeveloperCredential{}, model.DeveloperApplication{}, ErrDeveloperCredentialNotFound
	}
	if err != nil {
		return model.DeveloperCredential{}, model.DeveloperApplication{}, err
	}
	if err := json.Unmarshal(cs, &c.Scopes); err != nil {
		return model.DeveloperCredential{}, model.DeveloperApplication{}, err
	}
	if err := json.Unmarshal(redirects, &c.RedirectURIs); err != nil {
		return model.DeveloperCredential{}, model.DeveloperApplication{}, err
	}
	if err := json.Unmarshal(as, &a.AllowedScopes); err != nil {
		return model.DeveloperCredential{}, model.DeveloperApplication{}, err
	}
	if rotated.Valid {
		v := rotated.Int64
		c.RotatedFromID = &v
	}
	if revoked.Valid {
		v := revoked.Time
		c.RevokedAt = &v
	}
	if reviewer.Valid {
		v := reviewer.Int64
		a.ReviewedByUserID = &v
	}
	if submitted.Valid {
		v := submitted.Time
		a.SubmittedAt = &v
	}
	if reviewed.Valid {
		v := reviewed.Time
		a.ReviewedAt = &v
	}
	return c, a, nil
}

func scanDeveloperWebhookTest(scanner developerScanner) (model.DeveloperWebhookTest, error) {
	var item model.DeveloperWebhookTest
	var raw []byte
	var status sql.NullInt64
	err := scanner.Scan(&item.ID, &item.AppID, &item.WorkspaceID, &item.URL, &item.EventType, &raw, &item.DryRun, &item.Status, &status, &item.ResponsePreview, &item.Error, &item.CreatedByUserID, &item.CreatedAt)
	if err != nil {
		return model.DeveloperWebhookTest{}, err
	}
	if err := json.Unmarshal(raw, &item.Payload); err != nil {
		return model.DeveloperWebhookTest{}, err
	}
	if status.Valid {
		item.HTTPStatus = int(status.Int64)
	}
	return item, nil
}
