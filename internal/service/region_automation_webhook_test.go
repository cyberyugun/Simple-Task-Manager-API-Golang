package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
)

type regionAutomationTestState struct {
	mu            sync.Mutex
	calls         int
	failures      int
	requestIDs    []string
	bodyIDs       []string
	signatures    []string
	timestamps    []string
	modes         []string
	requestBodies []regionAutomationPlanRequest
}

func newRegionAutomationTestServer(t *testing.T, failures int) (*httptest.Server, *regionAutomationTestState) {
	t.Helper()
	state := &regionAutomationTestState{failures: failures}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/region-plans" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer automation-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body regionAutomationPlanRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Mode != "plan_only" || body.Action != "region_migration" || !body.Guardrails.ExecutionRequiresGate {
			t.Fatalf("unsafe automation request: %+v", body)
		}
		state.mu.Lock()
		state.calls++
		state.requestIDs = append(state.requestIDs, r.Header.Get("Idempotency-Key"))
		state.bodyIDs = append(state.bodyIDs, body.RequestID)
		state.signatures = append(state.signatures, r.Header.Get("X-Region-Automation-Signature"))
		state.timestamps = append(state.timestamps, r.Header.Get("X-Region-Automation-Timestamp"))
		state.modes = append(state.modes, r.Header.Get("X-Region-Automation-Mode"))
		state.requestBodies = append(state.requestBodies, body)
		call := state.calls
		state.mu.Unlock()

		if call <= failures {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "TEMPORARY", "message": "retry"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request_id":  body.RequestID,
			"plan_id":     "plan-123",
			"status":      "planned",
			"evidence_url": "https://evidence.example.com/plans/plan-123",
		})
	}))
	return server, state
}

func approvedRegionMigration() model.RegionMigration {
	now := time.Now().UTC().Truncate(time.Second)
	actor := int64(7)
	return model.RegionMigration{
		ID: 42, OrganizationID: 9, Scope: "organization",
		SourceRegion: "ap-southeast", TargetRegion: "ap-northeast",
		Status: model.RegionMigrationApproved, Reason: "planned resilience move",
		RequestedByUserID: actor, RequestedAt: now.Add(-time.Hour),
		DecidedByUserID: &actor, DecidedAt: &now, UpdatedAt: now,
	}
}

func regionAutomationPolicy() model.OrganizationRegionPolicy {
	return model.OrganizationRegionPolicy{
		OrganizationID: 9, HomeRegion: "ap-southeast",
		AllowedRegions: []string{"ap-southeast", "ap-northeast"},
		FailoverRegions: []string{"ap-northeast"}, DataResidencyEnforced: true,
		CrossRegionApprovalRequired: true, RPOSeconds: 300, RTOSeconds: 1800,
	}
}

func newRegionAutomationTestPlanner(t *testing.T, endpoint string, attempts int) *RegionAutomationWebhookPlanner {
	t.Helper()
	planner, err := NewRegionAutomationWebhookPlanner(RegionAutomationWebhookConfig{
		Endpoint: endpoint, SigningSecret: strings.Repeat("s", 32), BearerToken: "automation-token",
		Timeout: 2 * time.Second, RetryAttempts: attempts, RetryBackoff: time.Millisecond,
		MaxResponseBytes: 32 * 1024, AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return planner
}

func TestRegionAutomationWebhookPlannerCreatesSignedPlanOnlyHandoff(t *testing.T) {
	server, state := newRegionAutomationTestServer(t, 0)
	defer server.Close()
	planner := newRegionAutomationTestPlanner(t, server.URL+"/v1/region-plans", 2)

	receipt, err := planner.PlanMigration(approvedRegionMigration(), regionAutomationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if receipt.PlanID != "plan-123" || receipt.Status != "planned" || receipt.RequestID == "" ||
		receipt.EvidenceURL != "https://evidence.example.com/plans/plan-123" {
		t.Fatalf("receipt=%+v", receipt)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if state.calls != 1 || len(state.requestIDs[0]) != 64 || state.requestIDs[0] != state.bodyIDs[0] {
		t.Fatalf("state=%+v", state)
	}
	if state.modes[0] != "plan_only" || !strings.HasPrefix(state.signatures[0], "sha256=") || state.timestamps[0] == "" {
		t.Fatalf("missing safety/signing headers: %+v", state)
	}
	if state.requestBodies[0].Migration.TargetRegion != "ap-northeast" ||
		state.requestBodies[0].Guardrails.RPOSeconds != 300 {
		t.Fatalf("request=%+v", state.requestBodies[0])
	}
}

func TestRegionAutomationWebhookPlannerRetriesWithStableIdempotencyAndSignature(t *testing.T) {
	server, state := newRegionAutomationTestServer(t, 1)
	defer server.Close()
	planner := newRegionAutomationTestPlanner(t, server.URL+"/v1/region-plans", 3)

	if _, err := planner.PlanMigration(approvedRegionMigration(), regionAutomationPolicy()); err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.calls != 2 || state.requestIDs[0] != state.requestIDs[1] ||
		state.signatures[0] != state.signatures[1] || state.timestamps[0] != state.timestamps[1] {
		t.Fatalf("retry handoff changed: %+v", state)
	}
}

func TestRegionAutomationWebhookPlannerRejectsUnsafeConfigurationAndUnapprovedMigration(t *testing.T) {
	if _, err := NewRegionAutomationWebhookPlanner(RegionAutomationWebhookConfig{
		Endpoint: "http://automation.example.com/v1/region-plans",
		SigningSecret: strings.Repeat("s", 32),
	}); err == nil {
		t.Fatal("expected insecure endpoint rejection")
	}
	planner := newRegionAutomationTestPlanner(t, "http://127.0.0.1:1/v1/region-plans", 1)
	migration := approvedRegionMigration()
	migration.Status = model.RegionMigrationPendingApproval
	if _, err := planner.PlanMigration(migration, regionAutomationPolicy()); !errors.Is(err, ErrInvalidRegionMigration) {
		t.Fatalf("error=%v", err)
	}
}

type recordingRegionAutomationPlanner struct {
	calls   int
	fail    bool
	receipt RegionAutomationPlanReceipt
}

func (p *recordingRegionAutomationPlanner) PlanMigration(migration model.RegionMigration, policy model.OrganizationRegionPolicy) (RegionAutomationPlanReceipt, error) {
	p.calls++
	if migration.Status != model.RegionMigrationApproved || migration.DecidedByUserID == nil || migration.DecidedAt == nil {
		return RegionAutomationPlanReceipt{}, errors.New("migration was not approved before planning")
	}
	if !containsString(policy.AllowedRegions, migration.TargetRegion) {
		return RegionAutomationPlanReceipt{}, errors.New("target outside policy")
	}
	if p.fail {
		return RegionAutomationPlanReceipt{}, ErrRegionAutomationUnavailable
	}
	return p.receipt, nil
}

func TestGlobalRegionApprovalStoresAutomationPlanReceipt(t *testing.T) {
	s, orgID, userID := newRegionTestService(t)
	if _, err := s.UpdatePolicy(userID, orgID, model.UpdateOrganizationRegionPolicyRequest{
		HomeRegion: "ap-southeast", AllowedRegions: []string{"ap-southeast", "ap-northeast"},
		FailoverRegions: []string{"ap-northeast"}, DataResidencyEnforced: true,
		CrossRegionApprovalRequired: true, RPOSeconds: 300, RTOSeconds: 1800,
	}); err != nil {
		t.Fatal(err)
	}
	planner := &recordingRegionAutomationPlanner{receipt: RegionAutomationPlanReceipt{
		RequestID: "request-1", PlanID: "plan-1", Status: "planned",
		EvidenceURL: "https://evidence.example.com/plan-1", PlannedAt: time.Now().UTC(),
	}}
	s.SetAutomationPlanner(planner)
	migration, err := s.CreateMigration(userID, orgID, model.CreateRegionMigrationRequest{
		Scope: "organization", SourceRegion: "ap-southeast", TargetRegion: "ap-northeast", Reason: "approved move",
	})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := s.DecideMigration(userID, orgID, migration.ID, model.DecideRegionMigrationRequest{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	if planner.calls != 1 || approved.Status != model.RegionMigrationApproved {
		t.Fatalf("planner_calls=%d migration=%+v", planner.calls, approved)
	}
	plan, ok := approved.Checkpoint["automation_plan"].(map[string]any)
	if !ok || plan["plan_id"] != "plan-1" || plan["execution_mode"] != "plan_only" {
		t.Fatalf("checkpoint=%+v", approved.Checkpoint)
	}
}

func TestGlobalRegionPlanningFailureKeepsMigrationPending(t *testing.T) {
	s, orgID, userID := newRegionTestService(t)
	if _, err := s.UpdatePolicy(userID, orgID, model.UpdateOrganizationRegionPolicyRequest{
		HomeRegion: "ap-southeast", AllowedRegions: []string{"ap-southeast", "ap-northeast"},
		FailoverRegions: []string{"ap-northeast"}, DataResidencyEnforced: true,
		CrossRegionApprovalRequired: true, RPOSeconds: 300, RTOSeconds: 1800,
	}); err != nil {
		t.Fatal(err)
	}
	planner := &recordingRegionAutomationPlanner{fail: true}
	s.SetAutomationPlanner(planner)
	migration, err := s.CreateMigration(userID, orgID, model.CreateRegionMigrationRequest{
		Scope: "organization", SourceRegion: "ap-southeast", TargetRegion: "ap-northeast", Reason: "retry plan",
	})
	if err != nil {
		t.Fatal(err)
	}
	returned, err := s.DecideMigration(userID, orgID, migration.ID, model.DecideRegionMigrationRequest{Decision: "approve"})
	if !errors.Is(err, ErrRegionAutomationUnavailable) || returned.Status != model.RegionMigrationPendingApproval {
		t.Fatalf("returned=%+v err=%v", returned, err)
	}
	stored, err := s.repo.GetMigration(orgID, migration.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.RegionMigrationPendingApproval {
		t.Fatalf("stored status=%q", stored.Status)
	}
}

func TestGlobalRegionRejectionDoesNotCallAutomationPlanner(t *testing.T) {
	s, orgID, userID := newRegionTestService(t)
	if _, err := s.UpdatePolicy(userID, orgID, model.UpdateOrganizationRegionPolicyRequest{
		HomeRegion: "ap-southeast", AllowedRegions: []string{"ap-southeast", "ap-northeast"},
		FailoverRegions: []string{"ap-northeast"}, DataResidencyEnforced: true,
		CrossRegionApprovalRequired: true, RPOSeconds: 300, RTOSeconds: 1800,
	}); err != nil {
		t.Fatal(err)
	}
	planner := &recordingRegionAutomationPlanner{}
	s.SetAutomationPlanner(planner)
	migration, err := s.CreateMigration(userID, orgID, model.CreateRegionMigrationRequest{
		Scope: "organization", SourceRegion: "ap-southeast", TargetRegion: "ap-northeast", Reason: "reject",
	})
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := s.DecideMigration(userID, orgID, migration.ID, model.DecideRegionMigrationRequest{Decision: "reject"})
	if err != nil {
		t.Fatal(err)
	}
	if planner.calls != 0 || rejected.Status != model.RegionMigrationRejected {
		t.Fatalf("planner_calls=%d migration=%+v", planner.calls, rejected)
	}
}
