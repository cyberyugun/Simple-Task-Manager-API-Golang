package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresExtensionRepository struct {
	db *sql.DB
}

func NewPostgresExtensionRepository(db *sql.DB) *PostgresExtensionRepository {
	return &PostgresExtensionRepository{db: db}
}

func (r *PostgresExtensionRepository) CreatePublisher(item model.ExtensionPublisher) (model.ExtensionPublisher, error) {
	err := scanExtensionPublisher(r.db.QueryRow(`
		INSERT INTO extension_publishers (
			workspace_id,name,slug,website_url,status,created_by_user_id,reviewed_by_user_id,review_note,
			created_at,updated_at,submitted_at,reviewed_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id,workspace_id,name,slug,website_url,status,created_by_user_id,reviewed_by_user_id,review_note,
		          created_at,updated_at,submitted_at,reviewed_at
	`, item.WorkspaceID, item.Name, item.Slug, item.WebsiteURL, item.Status, item.CreatedByUserID,
		item.ReviewedByUserID, item.ReviewNote, item.CreatedAt, item.UpdatedAt, item.SubmittedAt, item.ReviewedAt), &item)
	return item, err
}

func (r *PostgresExtensionRepository) ListPublishers(workspaceID int64) ([]model.ExtensionPublisher, error) {
	rows, err := r.db.Query(`
		SELECT id,workspace_id,name,slug,website_url,status,created_by_user_id,reviewed_by_user_id,review_note,
		       created_at,updated_at,submitted_at,reviewed_at
		FROM extension_publishers WHERE workspace_id=$1 ORDER BY id
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.ExtensionPublisher{}
	for rows.Next() {
		var item model.ExtensionPublisher
		if err := scanExtensionPublisher(rows, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresExtensionRepository) GetPublisher(workspaceID, publisherID int64) (model.ExtensionPublisher, error) {
	var item model.ExtensionPublisher
	err := scanExtensionPublisher(r.db.QueryRow(`
		SELECT id,workspace_id,name,slug,website_url,status,created_by_user_id,reviewed_by_user_id,review_note,
		       created_at,updated_at,submitted_at,reviewed_at
		FROM extension_publishers WHERE workspace_id=$1 AND id=$2
	`, workspaceID, publisherID), &item)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ExtensionPublisher{}, ErrExtensionPublisherNotFound
	}
	return item, err
}

func (r *PostgresExtensionRepository) UpdatePublisherLifecycle(workspaceID, publisherID int64, status string, reviewerID *int64, note string, submittedAt, reviewedAt *time.Time, now time.Time) (model.ExtensionPublisher, error) {
	var item model.ExtensionPublisher
	err := scanExtensionPublisher(r.db.QueryRow(`
		UPDATE extension_publishers
		SET status=$3,reviewed_by_user_id=$4,review_note=$5,submitted_at=$6,reviewed_at=$7,updated_at=$8
		WHERE workspace_id=$1 AND id=$2
		RETURNING id,workspace_id,name,slug,website_url,status,created_by_user_id,reviewed_by_user_id,review_note,
		          created_at,updated_at,submitted_at,reviewed_at
	`, workspaceID, publisherID, status, reviewerID, note, submittedAt, reviewedAt, now), &item)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ExtensionPublisher{}, ErrExtensionPublisherNotFound
	}
	return item, err
}

type extensionScanner interface {
	Scan(dest ...any) error
}

func scanExtensionPublisher(scanner extensionScanner, item *model.ExtensionPublisher) error {
	return scanner.Scan(
		&item.ID, &item.WorkspaceID, &item.Name, &item.Slug, &item.WebsiteURL, &item.Status,
		&item.CreatedByUserID, &item.ReviewedByUserID, &item.ReviewNote, &item.CreatedAt, &item.UpdatedAt,
		&item.SubmittedAt, &item.ReviewedAt,
	)
}

func (r *PostgresExtensionRepository) CreateApplication(item model.MarketplaceApplication) (model.MarketplaceApplication, error) {
	categories, _ := json.Marshal(item.Categories)
	scopes, _ := json.Marshal(item.RequestedScopes)
	eventTypes, _ := json.Marshal(item.EventTypes)
	configSchema, _ := json.Marshal(item.ConfigSchema)
	packs, _ := json.Marshal(item.Packs)
	var rawCategories, rawScopes, rawEvents, rawConfig, rawPacks []byte
	err := r.db.QueryRow(`
		INSERT INTO marketplace_applications (
			publisher_id,publisher_workspace_id,slug,name,summary,description,version,manifest_version,execution_model,
			homepage_url,privacy_url,categories,requested_scopes,event_types,config_schema,packs,daily_request_limit,
			monthly_request_limit,status,created_by_user_id,reviewed_by_user_id,review_note,created_at,updated_at,
			submitted_at,reviewed_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13::jsonb,$14::jsonb,$15::jsonb,$16::jsonb,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)
		RETURNING id,publisher_id,publisher_workspace_id,slug,name,summary,description,version,manifest_version,execution_model,
		          homepage_url,privacy_url,categories,requested_scopes,event_types,config_schema,packs,daily_request_limit,
		          monthly_request_limit,status,created_by_user_id,reviewed_by_user_id,review_note,created_at,updated_at,
		          submitted_at,reviewed_at
	`, item.PublisherID, item.PublisherWorkspaceID, item.Slug, item.Name, item.Summary, item.Description,
		item.Version, item.ManifestVersion, item.ExecutionModel, item.HomepageURL, item.PrivacyURL,
		string(categories), string(scopes), string(eventTypes), string(configSchema), string(packs),
		item.DailyRequestLimit, item.MonthlyRequestLimit, item.Status, item.CreatedByUserID,
		item.ReviewedByUserID, item.ReviewNote, item.CreatedAt, item.UpdatedAt, item.SubmittedAt, item.ReviewedAt,
	).Scan(
		&item.ID, &item.PublisherID, &item.PublisherWorkspaceID, &item.Slug, &item.Name, &item.Summary,
		&item.Description, &item.Version, &item.ManifestVersion, &item.ExecutionModel, &item.HomepageURL, &item.PrivacyURL,
		&rawCategories, &rawScopes, &rawEvents, &rawConfig, &rawPacks, &item.DailyRequestLimit, &item.MonthlyRequestLimit,
		&item.Status, &item.CreatedByUserID, &item.ReviewedByUserID, &item.ReviewNote, &item.CreatedAt, &item.UpdatedAt,
		&item.SubmittedAt, &item.ReviewedAt,
	)
	if err != nil {
		return model.MarketplaceApplication{}, err
	}
	if err := decodeMarketplaceJSON(&item, rawCategories, rawScopes, rawEvents, rawConfig, rawPacks); err != nil {
		return model.MarketplaceApplication{}, err
	}
	return item, nil
}

func (r *PostgresExtensionRepository) ListWorkspaceApplications(workspaceID int64) ([]model.MarketplaceApplication, error) {
	return r.listApplications(`
		SELECT id,publisher_id,publisher_workspace_id,slug,name,summary,description,version,manifest_version,execution_model,
		       homepage_url,privacy_url,categories,requested_scopes,event_types,config_schema,packs,daily_request_limit,
		       monthly_request_limit,status,created_by_user_id,reviewed_by_user_id,review_note,created_at,updated_at,
		       submitted_at,reviewed_at
		FROM marketplace_applications WHERE publisher_workspace_id=$1 ORDER BY id
	`, workspaceID)
}

func (r *PostgresExtensionRepository) GetWorkspaceApplication(workspaceID, appID int64) (model.MarketplaceApplication, error) {
	item, err := r.scanApplication(r.db.QueryRow(`
		SELECT id,publisher_id,publisher_workspace_id,slug,name,summary,description,version,manifest_version,execution_model,
		       homepage_url,privacy_url,categories,requested_scopes,event_types,config_schema,packs,daily_request_limit,
		       monthly_request_limit,status,created_by_user_id,reviewed_by_user_id,review_note,created_at,updated_at,
		       submitted_at,reviewed_at
		FROM marketplace_applications WHERE publisher_workspace_id=$1 AND id=$2
	`, workspaceID, appID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.MarketplaceApplication{}, ErrMarketplaceApplicationNotFound
	}
	return item, err
}

func (r *PostgresExtensionRepository) GetApplication(appID int64) (model.MarketplaceApplication, error) {
	item, err := r.scanApplication(r.db.QueryRow(`
		SELECT id,publisher_id,publisher_workspace_id,slug,name,summary,description,version,manifest_version,execution_model,
		       homepage_url,privacy_url,categories,requested_scopes,event_types,config_schema,packs,daily_request_limit,
		       monthly_request_limit,status,created_by_user_id,reviewed_by_user_id,review_note,created_at,updated_at,
		       submitted_at,reviewed_at
		FROM marketplace_applications WHERE id=$1
	`, appID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.MarketplaceApplication{}, ErrMarketplaceApplicationNotFound
	}
	return item, err
}

func (r *PostgresExtensionRepository) ListMarketplaceApplications(category string) ([]model.MarketplaceApplication, error) {
	category = strings.ToLower(strings.TrimSpace(category))
	if category == "" {
		return r.listApplications(`
			SELECT id,publisher_id,publisher_workspace_id,slug,name,summary,description,version,manifest_version,execution_model,
			       homepage_url,privacy_url,categories,requested_scopes,event_types,config_schema,packs,daily_request_limit,
			       monthly_request_limit,status,created_by_user_id,reviewed_by_user_id,review_note,created_at,updated_at,
			       submitted_at,reviewed_at
			FROM marketplace_applications WHERE status='approved' ORDER BY lower(name),id
		`)
	}
	return r.listApplications(`
		SELECT id,publisher_id,publisher_workspace_id,slug,name,summary,description,version,manifest_version,execution_model,
		       homepage_url,privacy_url,categories,requested_scopes,event_types,config_schema,packs,daily_request_limit,
		       monthly_request_limit,status,created_by_user_id,reviewed_by_user_id,review_note,created_at,updated_at,
		       submitted_at,reviewed_at
		FROM marketplace_applications WHERE status='approved' AND categories ? $1 ORDER BY lower(name),id
	`, category)
}

func (r *PostgresExtensionRepository) UpdateApplicationLifecycle(workspaceID, appID int64, status string, reviewerID *int64, note string, submittedAt, reviewedAt *time.Time, now time.Time) (model.MarketplaceApplication, error) {
	item, err := r.scanApplication(r.db.QueryRow(`
		UPDATE marketplace_applications
		SET status=$3,reviewed_by_user_id=$4,review_note=$5,submitted_at=$6,reviewed_at=$7,updated_at=$8
		WHERE publisher_workspace_id=$1 AND id=$2
		RETURNING id,publisher_id,publisher_workspace_id,slug,name,summary,description,version,manifest_version,execution_model,
		          homepage_url,privacy_url,categories,requested_scopes,event_types,config_schema,packs,daily_request_limit,
		          monthly_request_limit,status,created_by_user_id,reviewed_by_user_id,review_note,created_at,updated_at,
		          submitted_at,reviewed_at
	`, workspaceID, appID, status, reviewerID, note, submittedAt, reviewedAt, now))
	if errors.Is(err, sql.ErrNoRows) {
		return model.MarketplaceApplication{}, ErrMarketplaceApplicationNotFound
	}
	return item, err
}

func (r *PostgresExtensionRepository) listApplications(query string, args ...any) ([]model.MarketplaceApplication, error) {
	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.MarketplaceApplication{}
	for rows.Next() {
		item, err := r.scanApplication(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresExtensionRepository) scanApplication(scanner extensionScanner) (model.MarketplaceApplication, error) {
	var item model.MarketplaceApplication
	var categories, scopes, eventTypes, configSchema, packs []byte
	err := scanner.Scan(
		&item.ID, &item.PublisherID, &item.PublisherWorkspaceID, &item.Slug, &item.Name, &item.Summary,
		&item.Description, &item.Version, &item.ManifestVersion, &item.ExecutionModel, &item.HomepageURL, &item.PrivacyURL,
		&categories, &scopes, &eventTypes, &configSchema, &packs, &item.DailyRequestLimit, &item.MonthlyRequestLimit,
		&item.Status, &item.CreatedByUserID, &item.ReviewedByUserID, &item.ReviewNote, &item.CreatedAt, &item.UpdatedAt,
		&item.SubmittedAt, &item.ReviewedAt,
	)
	if err != nil {
		return item, err
	}
	if err := decodeMarketplaceJSON(&item, categories, scopes, eventTypes, configSchema, packs); err != nil {
		return model.MarketplaceApplication{}, err
	}
	return item, nil
}

func decodeMarketplaceJSON(item *model.MarketplaceApplication, categories, scopes, eventTypes, configSchema, packs []byte) error {
	if err := json.Unmarshal(categories, &item.Categories); err != nil {
		return err
	}
	if err := json.Unmarshal(scopes, &item.RequestedScopes); err != nil {
		return err
	}
	if err := json.Unmarshal(eventTypes, &item.EventTypes); err != nil {
		return err
	}
	if err := json.Unmarshal(configSchema, &item.ConfigSchema); err != nil {
		return err
	}
	return json.Unmarshal(packs, &item.Packs)
}

func (r *PostgresExtensionRepository) CreateInstallation(item model.ExtensionInstallation, secretHash string) (model.ExtensionInstallation, error) {
	scopes, _ := json.Marshal(item.GrantedScopes)
	config, _ := json.Marshal(item.Config)
	secretRefs, _ := json.Marshal(item.SecretRefs)
	var rawScopes, rawConfig, rawSecretRefs []byte
	err := r.db.QueryRow(`
		INSERT INTO extension_installations (
			organization_id,application_id,workspace_id,status,granted_scopes,config,secret_refs,install_secret_hash,
			install_secret_prefix,daily_request_limit,monthly_request_limit,installed_by_user_id,installed_at,updated_at,
			uninstalled_by_user_id,uninstalled_at
		) VALUES ($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7::jsonb,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		RETURNING id,organization_id,application_id,workspace_id,status,granted_scopes,config,secret_refs,
		          install_secret_prefix,daily_request_limit,monthly_request_limit,installed_by_user_id,installed_at,
		          updated_at,uninstalled_by_user_id,uninstalled_at
	`, item.OrganizationID, item.ApplicationID, item.WorkspaceID, item.Status, string(scopes), string(config),
		string(secretRefs), secretHash, item.InstallSecretPrefix, item.DailyRequestLimit, item.MonthlyRequestLimit,
		item.InstalledByUserID, item.InstalledAt, item.UpdatedAt, item.UninstalledByUserID, item.UninstalledAt,
	).Scan(
		&item.ID, &item.OrganizationID, &item.ApplicationID, &item.WorkspaceID, &item.Status, &rawScopes, &rawConfig,
		&rawSecretRefs, &item.InstallSecretPrefix, &item.DailyRequestLimit, &item.MonthlyRequestLimit,
		&item.InstalledByUserID, &item.InstalledAt, &item.UpdatedAt, &item.UninstalledByUserID, &item.UninstalledAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return model.ExtensionInstallation{}, ErrExtensionInstallationExists
		}
		return model.ExtensionInstallation{}, err
	}
	if err := decodeInstallationJSON(&item, rawScopes, rawConfig, rawSecretRefs); err != nil {
		return model.ExtensionInstallation{}, err
	}
	return item, nil
}

func (r *PostgresExtensionRepository) ListInstallations(organizationID int64) ([]model.ExtensionInstallation, error) {
	rows, err := r.db.Query(`
		SELECT id,organization_id,application_id,workspace_id,status,granted_scopes,config,secret_refs,
		       install_secret_prefix,daily_request_limit,monthly_request_limit,installed_by_user_id,installed_at,
		       updated_at,uninstalled_by_user_id,uninstalled_at
		FROM extension_installations WHERE organization_id=$1 ORDER BY id
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.ExtensionInstallation{}
	for rows.Next() {
		item, err := scanExtensionInstallation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresExtensionRepository) GetInstallation(organizationID, installationID int64) (model.ExtensionInstallation, error) {
	item, err := scanExtensionInstallation(r.db.QueryRow(`
		SELECT id,organization_id,application_id,workspace_id,status,granted_scopes,config,secret_refs,
		       install_secret_prefix,daily_request_limit,monthly_request_limit,installed_by_user_id,installed_at,
		       updated_at,uninstalled_by_user_id,uninstalled_at
		FROM extension_installations WHERE organization_id=$1 AND id=$2
	`, organizationID, installationID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.ExtensionInstallation{}, ErrExtensionInstallationNotFound
	}
	return item, err
}

func (r *PostgresExtensionRepository) GetRuntimeInstallation(installationID, workspaceID int64) (model.ExtensionInstallation, error) {
	item, err := scanExtensionInstallation(r.db.QueryRow(`
		SELECT id,organization_id,application_id,workspace_id,status,granted_scopes,config,secret_refs,
		       install_secret_prefix,daily_request_limit,monthly_request_limit,installed_by_user_id,installed_at,
		       updated_at,uninstalled_by_user_id,uninstalled_at
		FROM extension_installations WHERE id=$1 AND workspace_id=$2 AND status='active'
	`, installationID, workspaceID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.ExtensionInstallation{}, ErrExtensionInstallationNotFound
	}
	return item, err
}

func (r *PostgresExtensionRepository) FindInstallationBySecretHash(secretHash string, now time.Time) (model.ExtensionInstallation, error) {
	item, err := scanExtensionInstallation(r.db.QueryRow(`
		SELECT id,organization_id,application_id,workspace_id,status,granted_scopes,config,secret_refs,
		       install_secret_prefix,daily_request_limit,monthly_request_limit,installed_by_user_id,installed_at,
		       updated_at,uninstalled_by_user_id,uninstalled_at
		FROM extension_installations
		WHERE install_secret_hash=$1 AND status='active'
	`, secretHash))
	if errors.Is(err, sql.ErrNoRows) {
		return model.ExtensionInstallation{}, ErrExtensionInstallationNotFound
	}
	return item, err
}

func (r *PostgresExtensionRepository) RotateInstallationSecret(organizationID, installationID int64, prefix, secretHash string, now time.Time) (model.ExtensionInstallation, error) {
	item, err := scanExtensionInstallation(r.db.QueryRow(`
		UPDATE extension_installations
		SET install_secret_prefix=$3,install_secret_hash=$4,updated_at=$5
		WHERE organization_id=$1 AND id=$2 AND status='active'
		RETURNING id,organization_id,application_id,workspace_id,status,granted_scopes,config,secret_refs,
		          install_secret_prefix,daily_request_limit,monthly_request_limit,installed_by_user_id,installed_at,
		          updated_at,uninstalled_by_user_id,uninstalled_at
	`, organizationID, installationID, prefix, secretHash, now))
	if errors.Is(err, sql.ErrNoRows) {
		return model.ExtensionInstallation{}, ErrExtensionInstallationNotFound
	}
	return item, err
}

func (r *PostgresExtensionRepository) Uninstall(organizationID, installationID, actorUserID int64, now time.Time) (model.ExtensionInstallation, error) {
	item, err := scanExtensionInstallation(r.db.QueryRow(`
		UPDATE extension_installations
		SET status='uninstalled',uninstalled_by_user_id=COALESCE(uninstalled_by_user_id,$3),
		    uninstalled_at=COALESCE(uninstalled_at,$4),updated_at=$4,
		    install_secret_hash='revoked:' || id::text || ':' || EXTRACT(EPOCH FROM $4)::bigint::text
		WHERE organization_id=$1 AND id=$2
		RETURNING id,organization_id,application_id,workspace_id,status,granted_scopes,config,secret_refs,
		          install_secret_prefix,daily_request_limit,monthly_request_limit,installed_by_user_id,installed_at,
		          updated_at,uninstalled_by_user_id,uninstalled_at
	`, organizationID, installationID, actorUserID, now))
	if errors.Is(err, sql.ErrNoRows) {
		return model.ExtensionInstallation{}, ErrExtensionInstallationNotFound
	}
	return item, err
}

func scanExtensionInstallation(scanner extensionScanner) (model.ExtensionInstallation, error) {
	var item model.ExtensionInstallation
	var scopes, config, secretRefs []byte
	err := scanner.Scan(
		&item.ID, &item.OrganizationID, &item.ApplicationID, &item.WorkspaceID, &item.Status, &scopes, &config,
		&secretRefs, &item.InstallSecretPrefix, &item.DailyRequestLimit, &item.MonthlyRequestLimit,
		&item.InstalledByUserID, &item.InstalledAt, &item.UpdatedAt, &item.UninstalledByUserID, &item.UninstalledAt,
	)
	if err != nil {
		return item, err
	}
	if err := decodeInstallationJSON(&item, scopes, config, secretRefs); err != nil {
		return model.ExtensionInstallation{}, err
	}
	return item, nil
}

func decodeInstallationJSON(item *model.ExtensionInstallation, scopes, config, secretRefs []byte) error {
	if err := json.Unmarshal(scopes, &item.GrantedScopes); err != nil {
		return err
	}
	if err := json.Unmarshal(config, &item.Config); err != nil {
		return err
	}
	return json.Unmarshal(secretRefs, &item.SecretRefs)
}

func (r *PostgresExtensionRepository) CreateSubscription(item model.ExtensionEventSubscription) (model.ExtensionEventSubscription, error) {
	eventTypes, _ := json.Marshal(item.EventTypes)
	var rawEvents []byte
	err := r.db.QueryRow(`
		INSERT INTO extension_event_subscriptions (
			installation_id,organization_id,workspace_id,webhook_subscription_id,url,event_types,created_by_user_id,created_at
		) VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8)
		RETURNING id,installation_id,organization_id,workspace_id,webhook_subscription_id,url,event_types,created_by_user_id,created_at
	`, item.InstallationID, item.OrganizationID, item.WorkspaceID, item.WebhookSubscriptionID, item.URL,
		string(eventTypes), item.CreatedByUserID, item.CreatedAt,
	).Scan(&item.ID, &item.InstallationID, &item.OrganizationID, &item.WorkspaceID, &item.WebhookSubscriptionID,
		&item.URL, &rawEvents, &item.CreatedByUserID, &item.CreatedAt)
	if err != nil {
		return model.ExtensionEventSubscription{}, err
	}
	if err := json.Unmarshal(rawEvents, &item.EventTypes); err != nil {
		return model.ExtensionEventSubscription{}, err
	}
	return item, nil
}

func (r *PostgresExtensionRepository) ListSubscriptions(organizationID, installationID int64) ([]model.ExtensionEventSubscription, error) {
	rows, err := r.db.Query(`
		SELECT id,installation_id,organization_id,workspace_id,webhook_subscription_id,url,event_types,created_by_user_id,created_at
		FROM extension_event_subscriptions WHERE organization_id=$1 AND installation_id=$2 ORDER BY id
	`, organizationID, installationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.ExtensionEventSubscription{}
	for rows.Next() {
		item, err := scanExtensionSubscription(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresExtensionRepository) GetSubscription(organizationID, installationID, subscriptionID int64) (model.ExtensionEventSubscription, error) {
	item, err := scanExtensionSubscription(r.db.QueryRow(`
		SELECT id,installation_id,organization_id,workspace_id,webhook_subscription_id,url,event_types,created_by_user_id,created_at
		FROM extension_event_subscriptions WHERE organization_id=$1 AND installation_id=$2 AND id=$3
	`, organizationID, installationID, subscriptionID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.ExtensionEventSubscription{}, ErrExtensionSubscriptionNotFound
	}
	return item, err
}

func (r *PostgresExtensionRepository) DeleteSubscription(organizationID, installationID, subscriptionID int64) error {
	result, err := r.db.Exec(`
		DELETE FROM extension_event_subscriptions WHERE organization_id=$1 AND installation_id=$2 AND id=$3
	`, organizationID, installationID, subscriptionID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrExtensionSubscriptionNotFound
	}
	return nil
}

func scanExtensionSubscription(scanner extensionScanner) (model.ExtensionEventSubscription, error) {
	var item model.ExtensionEventSubscription
	var eventTypes []byte
	err := scanner.Scan(
		&item.ID, &item.InstallationID, &item.OrganizationID, &item.WorkspaceID, &item.WebhookSubscriptionID,
		&item.URL, &eventTypes, &item.CreatedByUserID, &item.CreatedAt,
	)
	if err != nil {
		return item, err
	}
	if err := json.Unmarshal(eventTypes, &item.EventTypes); err != nil {
		return model.ExtensionEventSubscription{}, err
	}
	return item, nil
}

func (r *PostgresExtensionRepository) ConsumeRequest(installationID, workspaceID int64, now time.Time) (model.ExtensionInstallation, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return model.ExtensionInstallation{}, err
	}
	defer tx.Rollback()

	item, err := scanExtensionInstallation(tx.QueryRow(`
		SELECT id,organization_id,application_id,workspace_id,status,granted_scopes,config,secret_refs,
		       install_secret_prefix,daily_request_limit,monthly_request_limit,installed_by_user_id,installed_at,
		       updated_at,uninstalled_by_user_id,uninstalled_at
		FROM extension_installations
		WHERE id=$1 AND workspace_id=$2 AND status='active'
		FOR UPDATE
	`, installationID, workspaceID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.ExtensionInstallation{}, ErrExtensionInstallationNotFound
	}
	if err != nil {
		return model.ExtensionInstallation{}, err
	}

	day := now.UTC().Format("2006-01-02")
	monthStart := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	var daily, monthly int64
	if err := tx.QueryRow(`
		SELECT
		  COALESCE(MAX(requests) FILTER (WHERE usage_date=$2::date),0),
		  COALESCE(SUM(requests) FILTER (WHERE usage_date >= $3::date),0)
		FROM extension_usage_daily WHERE installation_id=$1
	`, installationID, day, monthStart).Scan(&daily, &monthly); err != nil {
		return model.ExtensionInstallation{}, err
	}
	if daily >= item.DailyRequestLimit || monthly >= item.MonthlyRequestLimit {
		return model.ExtensionInstallation{}, ErrExtensionQuotaExceeded
	}
	_, err = tx.Exec(`
		INSERT INTO extension_usage_daily (
			installation_id,organization_id,workspace_id,usage_date,requests,errors,total_latency_ms,last_request_at
		) VALUES ($1,$2,$3,$4::date,1,0,0,$5)
		ON CONFLICT(installation_id,usage_date) DO UPDATE
		SET requests=extension_usage_daily.requests+1,last_request_at=EXCLUDED.last_request_at
	`, item.ID, item.OrganizationID, item.WorkspaceID, day, now.UTC())
	if err != nil {
		return model.ExtensionInstallation{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.ExtensionInstallation{}, err
	}
	return item, nil
}

func (r *PostgresExtensionRepository) RecordResponse(installationID, organizationID, workspaceID int64, statusCode int, latencyMS int64, now time.Time) error {
	errorsDelta := 0
	if statusCode >= 400 {
		errorsDelta = 1
	}
	_, err := r.db.Exec(`
		INSERT INTO extension_usage_daily (
			installation_id,organization_id,workspace_id,usage_date,requests,errors,total_latency_ms,last_request_at
		) VALUES ($1,$2,$3,$4::date,0,$5,$6,$7)
		ON CONFLICT(installation_id,usage_date) DO UPDATE
		SET errors=extension_usage_daily.errors+EXCLUDED.errors,
		    total_latency_ms=extension_usage_daily.total_latency_ms+EXCLUDED.total_latency_ms,
		    last_request_at=GREATEST(extension_usage_daily.last_request_at,EXCLUDED.last_request_at)
	`, installationID, organizationID, workspaceID, now.UTC().Format("2006-01-02"), errorsDelta, maxExtensionInt64(latencyMS, 0), now.UTC())
	return err
}

func (r *PostgresExtensionRepository) Usage(organizationID, installationID int64, from, to time.Time) ([]model.ExtensionUsageDaily, error) {
	if _, err := r.GetInstallation(organizationID, installationID); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(`
		SELECT installation_id,organization_id,workspace_id,usage_date::text,requests,errors,total_latency_ms,last_request_at
		FROM extension_usage_daily
		WHERE organization_id=$1 AND installation_id=$2 AND usage_date BETWEEN $3::date AND $4::date
		ORDER BY usage_date
	`, organizationID, installationID, from.UTC().Format("2006-01-02"), to.UTC().Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.ExtensionUsageDaily{}
	for rows.Next() {
		var item model.ExtensionUsageDaily
		if err := rows.Scan(&item.InstallationID, &item.OrganizationID, &item.WorkspaceID, &item.Date, &item.Requests,
			&item.Errors, &item.TotalLatencyMS, &item.LastRequestAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresExtensionRepository) RecordDomainEvent(event model.DomainEvent) error {
	if strings.TrimSpace(event.EventKey) == "" {
		key, err := randomEventKey()
		if err != nil {
			return err
		}
		event.EventKey = key
	}
	if event.SchemaVersion <= 0 {
		event.SchemaVersion = 1
	}
	if strings.TrimSpace(event.CorrelationID) == "" {
		event.CorrelationID = event.EventKey
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if len(event.Data) == 0 {
		event.Data = []byte("{}")
	}
	_, err := r.db.Exec(`
		INSERT INTO outbox_events (
			event_key,workspace_id,event_type,aggregate_type,aggregate_id,schema_version,
			correlation_id,causation_id,payload,occurred_at,available_at,created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$10,$10)
	`, event.EventKey, event.WorkspaceID, event.EventType, event.AggregateType, event.AggregateID,
		event.SchemaVersion, event.CorrelationID, event.CausationID, string(event.Data), event.OccurredAt)
	return err
}

func maxExtensionInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
