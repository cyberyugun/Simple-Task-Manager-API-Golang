//go:build integration

package repository_test

import (
	"testing"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestIntegrationPostgresConnectorSecurity(t *testing.T) {
	databaseURL := getenvRequired(t, "TEST_DATABASE_URL")
	db, err := appdb.OpenPostgres(databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgres() error = %v", err)
	}
	if err := resetDatabase(db); err != nil {
		_ = db.Close()
		t.Fatalf("reset database: %v", err)
	}
	t.Cleanup(func() {
		if err := resetDatabase(db); err != nil {
			t.Errorf("cleanup database: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	applyMigrations(t, db)

	users := repository.NewPostgresUserRepository(db)
	orgs := repository.NewPostgresOrganizationRepository(db)
	integrations := repository.NewPostgresIntegrationRepository(db)
	security := repository.NewPostgresConnectorSecurityRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	user, err := users.Create(model.User{
		Name: "Connector Owner", Email: "connector-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Connector Org", Status: model.OrganizationStatusActive, OwnerUserID: user.ID,
		MaxMembers: 20, MaxWorkspaces: 20, CreatedByUserID: user.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := integrations.CreateIntegrationConnection(model.IntegrationConnection{
		OrganizationID: org.ID, Provider: model.IntegrationProviderGitHub, Name: "GitHub",
		Status: model.IntegrationConnectionActive, AuthType: model.IntegrationAuthOAuth2,
		Config: map[string]any{"client_id": "client"}, HealthStatus: model.IntegrationHealthUnknown,
		CreatedByUserID: user.ID, UpdatedByUserID: user.ID, CreatedAt: now, UpdatedAt: now,
	}, "encrypted")
	if err != nil {
		t.Fatal(err)
	}

	session, err := security.CreateOAuthSession(model.ConnectorOAuthSession{
		OrganizationID: org.ID, ConnectionID: connection.ID, StateHash: "state-hash",
		EncryptedVerifier: "encrypted-verifier", RedirectURI: "https://example.test/callback",
		RequestedScopes: []string{"tasks:read"}, CreatedByUserID: user.ID,
		ExpiresAt: now.Add(10 * time.Minute), CreatedAt: now,
	})
	if err != nil || session.ID == 0 {
		t.Fatalf("session=%+v err=%v", session, err)
	}
	consumed, err := security.ConsumeOAuthSession("state-hash", now.Add(time.Minute))
	if err != nil || consumed.ConsumedAt == nil || consumed.ConnectionID != connection.ID {
		t.Fatalf("consumed=%+v err=%v", consumed, err)
	}
	if _, err := security.ConsumeOAuthSession("state-hash", now.Add(2*time.Minute)); err != repository.ErrConnectorOAuthSessionNotFound {
		t.Fatalf("expected consumed session rejection, got %v", err)
	}

	expires := now.Add(4 * time.Minute)
	meta, err := security.UpsertCredentialMetadata(model.ConnectorCredentialMetadata{
		ConnectionID: connection.ID, OrganizationID: org.ID,
		SecretBackend: model.ConnectorSecretBackendDatabase, SecretRef: "org/ref",
		KeyVersion: 1, CredentialVersion: 1, Status: model.ConnectorCredentialActive,
		GrantedScopes: []string{"tasks:read"}, ExpiresAt: &expires, UpdatedAt: now,
	})
	if err != nil || meta.ConnectionID != connection.ID {
		t.Fatalf("metadata=%+v err=%v", meta, err)
	}
	due, err := security.ListCredentialsDue(now.Add(5*time.Minute), 10)
	if err != nil || len(due) != 1 || due[0].ConnectionID != connection.ID {
		t.Fatalf("due=%+v err=%v", due, err)
	}

	actor := user.ID
	if err := security.RecordCredentialAccess(model.ConnectorCredentialAccess{
		OrganizationID: org.ID, ConnectionID: connection.ID, ActorUserID: &actor,
		Action: "rotate", SecretBackend: model.ConnectorSecretBackendDatabase,
		SecretRef: "org/ref", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	audit, err := security.ListCredentialAccess(org.ID, connection.ID, 10)
	if err != nil || len(audit) != 1 || audit[0].Action != "rotate" {
		t.Fatalf("audit=%+v err=%v", audit, err)
	}
}
