//go:build integration

package repository_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net/url"
	"testing"
	"time"

	"go-simple-task-api/internal/auth"
	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
)

func TestIntegrationPostgresZeroTrustSecurity(t *testing.T) {
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
	workspaces := repository.NewPostgresWorkspaceRepository(db)
	orgs := repository.NewPostgresOrganizationRepository(db)
	refreshes := repository.NewPostgresRefreshTokenRepository(db)
	zeroRepo := repository.NewPostgresZeroTrustRepository(db)
	tokens := auth.NewTokenManager("phase-43-integration-token-secret-1234567890", 15*time.Minute)
	zero := service.NewZeroTrustService(zeroRepo, orgs, refreshes, tokens, "phase-43-integration-checkpoint-secret-1234567890")

	now := time.Now().UTC().Truncate(time.Microsecond)
	owner, err := users.Create(model.User{
		Name: "Zero Trust Owner", Email: "zero-trust-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspaces.Create(owner.ID, "Zero Trust Workspace", now)
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Zero Trust Organization", Status: model.OrganizationStatusActive, OwnerUserID: owner.ID,
		MaxWorkspaces: 20, MaxMembers: 100, CreatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := orgs.AttachWorkspace(model.OrganizationWorkspace{
		OrganizationID: org.ID, WorkspaceID: workspace.ID, AttachedByID: owner.ID, AttachedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := zero.UpdatePolicy(owner.ID, org.ID, model.UpdateZeroTrustPolicyRequest{
		Enabled: true, RequireWorkloadMTLS: true, RequireBoundTokens: true,
		StepUpRiskScore: 40, RevokeRiskScore: 75, ImpossibleTravelKPH: 900, WAFBlockScore: 80,
		AllowedCIDRs: []string{"10.0.0.0/8"}, DeniedCIDRs: []string{"203.0.113.0/24"},
		AutoRevokeHighRisk: true, RequireTrustedDevice: true, SIEMFederationEnabled: true,
		AuditCheckpointInterval: 100,
	}); err != nil {
		t.Fatal(err)
	}

	workload, err := zero.CreateWorkload(owner.ID, org.ID, model.CreateWorkloadIdentityRequest{
		WorkspaceID: workspace.ID, Name: "integration-worker",
		SPIFFEID: "spiffe://simple-task-manager/workload/integration",
		AllowedScopes: []string{model.ScopeTasksRead}, MTLSRequired: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	certPEM, fingerprint := integrationWorkloadCertificate(t, workload.SPIFFEID)
	if _, err := zero.RegisterCertificate(owner.ID, org.ID, workload.ID, model.RegisterWorkloadCertificateRequest{
		CertificatePEM: certPEM, ReplaceExisting: true,
	}); err != nil {
		t.Fatal(err)
	}
	workloadToken, err := zero.ExchangeWorkloadToken(fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := tokens.ParseClaims(workloadToken.AccessToken)
	if err != nil || claims.ConfirmationThumbprint != fingerprint || claims.WorkspaceID != workspace.ID {
		t.Fatalf("claims=%+v err=%v", claims, err)
	}

	if err := refreshes.Create(model.RefreshSession{
		UserID: owner.ID, TokenHash: "phase43-refresh-hash", UserAgent: "integration",
		IPAddress: "10.0.0.10", ExpiresAt: now.Add(24 * time.Hour), LastUsedAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	sessions, err := refreshes.ListActive(owner.ID, time.Now().UTC())
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
	}
	risk, err := zero.EvaluateRisk(owner.ID, org.ID, false, model.RiskEvaluationRequest{
		SessionID: sessions[0].ID, SourceIP: "203.0.113.9", WAFScore: 95,
	})
	if err != nil {
		t.Fatal(err)
	}
	if risk.Action != model.ZeroTrustActionRevoke || !risk.SessionRevoked {
		t.Fatalf("unexpected risk result: %+v", risk)
	}

	checkpoint, err := zero.CreateCheckpoint(owner.ID, org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.EventCount < 3 || len(checkpoint.Signature) != 64 {
		t.Fatalf("unexpected checkpoint: %+v", checkpoint)
	}
	exported, err := zero.CreateWORMExport(owner.ID, org.ID)
	if err != nil || exported.CheckpointHash != checkpoint.CheckpointHash {
		t.Fatalf("export=%+v err=%v", exported, err)
	}
	dashboard, err := zero.Dashboard(owner.ID, org.ID)
	if err != nil || dashboard.ActiveWorkloads != 1 || dashboard.HighRiskEvents == 0 || dashboard.LatestCheckpoint == nil {
		t.Fatalf("dashboard=%+v err=%v", dashboard, err)
	}
}

func integrationWorkloadCertificate(t *testing.T, spiffeID string) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := url.Parse(spiffeID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(4301),
		Subject: pkix.Name{CommonName: "phase43-integration-worker"},
		NotBefore: now.Add(-time.Minute),
		NotAfter: now.Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs: []*url.URL{uri},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), hex.EncodeToString(sum[:])
}
