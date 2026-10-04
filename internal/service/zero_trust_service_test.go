package service

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net/url"
	"testing"
	"time"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func newZeroTrustTestService(t *testing.T) (*ZeroTrustService, *repository.InMemoryRefreshTokenRepository, *auth.TokenManager, int64, int64) {
	t.Helper()
	orgs := repository.NewInMemoryOrganizationRepository()
	regions := repository.NewInMemoryZeroTrustRepository()
	refreshes := repository.NewInMemoryRefreshTokenRepository()
	tokens := auth.NewTokenManager("phase-43-test-secret-12345678901234567890", 15*time.Minute)
	now := time.Now().UTC()
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Zero Trust Test", Status: model.OrganizationStatusActive, OwnerUserID: 1,
		MaxWorkspaces: 10, MaxMembers: 10, CreatedByUserID: 1, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	const workspaceID int64 = 77
	if _, err := orgs.AttachWorkspace(model.OrganizationWorkspace{
		OrganizationID: org.ID, WorkspaceID: workspaceID, AttachedByID: 1, AttachedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return NewZeroTrustService(regions, orgs, refreshes, tokens, "phase-43-checkpoint-secret-1234567890"), refreshes, tokens, org.ID, workspaceID
}

func TestZeroTrustWorkloadMTLSBindingAndRotation(t *testing.T) {
	svc, _, tokens, orgID, workspaceID := newZeroTrustTestService(t)
	_, err := svc.UpdatePolicy(1, orgID, model.UpdateZeroTrustPolicyRequest{
		Enabled: true, RequireWorkloadMTLS: true, RequireBoundTokens: true,
		StepUpRiskScore: 40, RevokeRiskScore: 75, ImpossibleTravelKPH: 900, WAFBlockScore: 80,
		AutoRevokeHighRisk: true, AuditCheckpointInterval: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	workload, err := svc.CreateWorkload(1, orgID, model.CreateWorkloadIdentityRequest{
		WorkspaceID: workspaceID,
		Name: "payments-worker",
		SPIFFEID: "spiffe://simple-task-manager/workload/payments",
		AllowedScopes: []string{model.ScopeTasksRead, model.ScopeTasksWrite},
		MTLSRequired: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	certPEM, fingerprint := testWorkloadCertificate(t, workload.SPIFFEID, time.Now().Add(24*time.Hour))
	cert, err := svc.RegisterCertificate(1, orgID, workload.ID, model.RegisterWorkloadCertificateRequest{
		CertificatePEM: certPEM, ReplaceExisting: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cert.SHA256Fingerprint != fingerprint {
		t.Fatalf("fingerprint = %q, want %q", cert.SHA256Fingerprint, fingerprint)
	}
	result, err := svc.ExchangeWorkloadToken(fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := tokens.ParseClaims(result.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.TokenUse != model.TokenUseService || claims.WorkspaceID != workspaceID ||
		claims.ConfirmationThumbprint != fingerprint {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestZeroTrustRiskRevokesHighRiskSessionAndBuildsAuditChain(t *testing.T) {
	svc, refreshes, _, orgID, _ := newZeroTrustTestService(t)
	_, err := svc.UpdatePolicy(1, orgID, model.UpdateZeroTrustPolicyRequest{
		Enabled: true, RequireWorkloadMTLS: true, RequireBoundTokens: true,
		StepUpRiskScore: 40, RevokeRiskScore: 75, ImpossibleTravelKPH: 900, WAFBlockScore: 80,
		AllowedCIDRs: []string{"10.0.0.0/8"}, DeniedCIDRs: []string{"203.0.113.0/24"},
		AutoRevokeHighRisk: true, RequireTrustedDevice: true, SIEMFederationEnabled: true,
		AuditCheckpointInterval: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := refreshes.Create(model.RefreshSession{
		UserID: 1, TokenHash: "session-token-hash", UserAgent: "test", IPAddress: "10.0.0.1",
		ExpiresAt: now.Add(24 * time.Hour), LastUsedAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	sessions, err := refreshes.ListActive(1, now)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions=%v err=%v", sessions, err)
	}
	evaluation, err := svc.EvaluateRisk(1, orgID, false, model.RiskEvaluationRequest{
		SessionID: sessions[0].ID,
		SourceIP: "203.0.113.10",
		WAFScore: 95,
	})
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Action != model.ZeroTrustActionRevoke || !evaluation.SessionRevoked || evaluation.RiskScore != 100 {
		t.Fatalf("unexpected evaluation: %+v", evaluation)
	}
	active, err := refreshes.ListActive(1, time.Now().UTC())
	if err != nil || len(active) != 0 {
		t.Fatalf("active sessions after revoke=%v err=%v", active, err)
	}

	checkpoint, err := svc.CreateCheckpoint(1, orgID)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Sequence != 1 || checkpoint.EventCount < 2 || len(checkpoint.CheckpointHash) != 64 || len(checkpoint.Signature) != 64 {
		t.Fatalf("unexpected checkpoint: %+v", checkpoint)
	}
	exported, err := svc.CreateWORMExport(1, orgID)
	if err != nil {
		t.Fatal(err)
	}
	if exported.CheckpointHash != checkpoint.CheckpointHash || exported.ObjectURI == "" {
		t.Fatalf("unexpected export: %+v", exported)
	}
	feed, err := svc.SIEMFeed(1, orgID, 0)
	if err != nil || len(feed) < 2 {
		t.Fatalf("feed=%v err=%v", feed, err)
	}
	dashboard, err := svc.Dashboard(1, orgID)
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.HighRiskEvents == 0 || dashboard.RevokedSessions == 0 || dashboard.LatestCheckpoint == nil {
		t.Fatalf("unexpected dashboard: %+v", dashboard)
	}
}

func TestZeroTrustImpossibleTravelRequiresStepUp(t *testing.T) {
	svc, _, _, orgID, _ := newZeroTrustTestService(t)
	_, err := svc.UpdatePolicy(1, orgID, model.UpdateZeroTrustPolicyRequest{
		Enabled: true, RequireWorkloadMTLS: true, RequireBoundTokens: true,
		StepUpRiskScore: 40, RevokeRiskScore: 90, ImpossibleTravelKPH: 900, WAFBlockScore: 80,
		AutoRevokeHighRisk: true, AuditCheckpointInterval: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	previousLat, previousLon := 0.0, 0.0
	currentLat, currentLon := 40.7128, -74.0060
	previousSeen := time.Now().UTC().Add(-2 * time.Hour)
	result, err := svc.EvaluateRisk(1, orgID, false, model.RiskEvaluationRequest{
		SourceIP: "10.0.0.1",
		PreviousLatitude: &previousLat, PreviousLongitude: &previousLon,
		CurrentLatitude: &currentLat, CurrentLongitude: &currentLon,
		PreviousSeenAt: &previousSeen,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != model.ZeroTrustActionStepUp {
		t.Fatalf("expected step-up, got %+v", result)
	}
}

func testWorkloadCertificate(t *testing.T, spiffeID string, notAfter time.Time) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := url.Parse(spiffeID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject: pkix.Name{CommonName: "phase43-test-workload"},
		NotBefore: now,
		NotAfter: notAfter,
		KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		URIs: []*url.URL{uri},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256Bytes(der)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), hex.EncodeToString(sum)
}

func sha256Bytes(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}
