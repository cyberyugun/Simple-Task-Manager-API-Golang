package service

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
)

func TestLiveRegionAutomationProviderContract(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_CONTRACTS")), "true") {
		t.Skip("live provider contracts are opt-in")
	}
	if target := strings.ToLower(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_CONTRACT_TARGET"))); target != "region_automation" {
		t.Fatalf("unsupported live region automation contract target %q", target)
	}

	allowInsecure := strings.EqualFold(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_ALLOW_INSECURE")), "true")
	planner, err := NewRegionAutomationWebhookPlannerFromEnv(allowInsecure)
	if err != nil {
		t.Fatalf("configure live region automation planner: %v", err)
	}
	if planner == nil {
		t.Fatal("region automation planner is not enabled")
	}

	sourceRegion := liveRegionRequiredEnv(t, "LIVE_REGION_SOURCE_REGION")
	targetRegion := liveRegionRequiredEnv(t, "LIVE_REGION_TARGET_REGION")
	if sourceRegion == targetRegion {
		t.Fatal("LIVE_REGION_SOURCE_REGION and LIVE_REGION_TARGET_REGION must differ")
	}
	allowedRegions := liveRegionAllowedRegions(t, sourceRegion, targetRegion)
	rpoSeconds := liveRegionPositiveIntEnv(t, "LIVE_REGION_RPO_SECONDS", 300)
	rtoSeconds := liveRegionPositiveIntEnv(t, "LIVE_REGION_RTO_SECONDS", 1800)

	now := time.Now().UTC().Truncate(time.Second)
	actorID := int64(990000001)
	migrationID := liveRegionSyntheticID()
	migration := model.RegionMigration{
		ID:                migrationID,
		OrganizationID:    990000002,
		Scope:             "organization",
		SourceRegion:      sourceRegion,
		TargetRegion:      targetRegion,
		Status:            model.RegionMigrationApproved,
		Reason:            "synthetic live contract validation; plan only; no execution",
		RequestedByUserID: actorID,
		RequestedAt:       now.Add(-time.Minute),
		DecidedByUserID:   &actorID,
		DecisionComment:   "approved only for synthetic plan-only contract validation",
		DecidedAt:         &now,
		UpdatedAt:         now,
	}
	policy := model.OrganizationRegionPolicy{
		OrganizationID:              migration.OrganizationID,
		HomeRegion:                  sourceRegion,
		AllowedRegions:              allowedRegions,
		FailoverRegions:             []string{targetRegion},
		DataResidencyEnforced:       true,
		CrossRegionApprovalRequired: true,
		RPOSeconds:                  rpoSeconds,
		RTOSeconds:                  rtoSeconds,
		UpdatedByUserID:             actorID,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	}

	expectedRequestID, err := regionAutomationRequestID(migration, policy)
	if err != nil {
		t.Fatalf("derive region automation request id: %v", err)
	}
	repeatedRequestID, err := regionAutomationRequestID(migration, policy)
	if err != nil {
		t.Fatalf("derive repeated region automation request id: %v", err)
	}
	if len(expectedRequestID) != 64 || expectedRequestID != repeatedRequestID {
		t.Fatal("region automation request id is not deterministic")
	}

	first, err := planner.PlanMigration(migration, policy)
	if err != nil {
		t.Fatalf("first live region automation planning request failed: %v", err)
	}
	liveRegionAssertPlanReceipt(t, expectedRequestID, first)

	second, err := planner.PlanMigration(migration, policy)
	if err != nil {
		t.Fatalf("idempotent live region automation planning request failed: %v", err)
	}
	liveRegionAssertPlanReceipt(t, expectedRequestID, second)

	if first.RequestID != second.RequestID || first.PlanID != second.PlanID {
		t.Fatalf("region automation gateway did not preserve idempotent plan identity: first=%+v second=%+v", first, second)
	}

	t.Logf(
		"live region automation contract passed request_id_prefix=%s plan_id=%s first_status=%s second_status=%s mode=plan_only external_gate_required=true",
		expectedRequestID[:12],
		first.PlanID,
		first.Status,
		second.Status,
	)
}

func liveRegionAssertPlanReceipt(t *testing.T, expectedRequestID string, receipt RegionAutomationPlanReceipt) {
	t.Helper()
	if receipt.RequestID != expectedRequestID {
		t.Fatalf("region automation receipt request_id=%q want=%q", receipt.RequestID, expectedRequestID)
	}
	if strings.TrimSpace(receipt.PlanID) == "" {
		t.Fatal("region automation receipt plan id is empty")
	}
	if receipt.Status != "planned" && receipt.Status != "accepted" {
		t.Fatalf("region automation receipt status=%q must be planned or accepted", receipt.Status)
	}
	if receipt.PlannedAt.IsZero() {
		t.Fatal("region automation receipt planned_at is zero")
	}
}

func liveRegionRequiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	return value
}

func liveRegionAllowedRegions(t *testing.T, sourceRegion, targetRegion string) []string {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("LIVE_REGION_ALLOWED_REGIONS"))
	if raw == "" {
		return []string{sourceRegion, targetRegion}
	}
	seen := make(map[string]struct{})
	regions := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		region := strings.TrimSpace(part)
		if region == "" {
			continue
		}
		if _, ok := seen[region]; ok {
			continue
		}
		seen[region] = struct{}{}
		regions = append(regions, region)
	}
	if len(regions) == 0 {
		t.Fatal("LIVE_REGION_ALLOWED_REGIONS must contain at least one region")
	}
	if _, ok := seen[sourceRegion]; !ok {
		t.Fatalf("LIVE_REGION_ALLOWED_REGIONS must include source region %q", sourceRegion)
	}
	if _, ok := seen[targetRegion]; !ok {
		t.Fatalf("LIVE_REGION_ALLOWED_REGIONS must include target region %q", targetRegion)
	}
	return regions
}

func liveRegionPositiveIntEnv(t *testing.T, name string, fallback int) int {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		t.Fatalf("%s must be a positive integer", name)
	}
	return value
}

func liveRegionSyntheticID() int64 {
	if runID := strings.TrimSpace(os.Getenv("GITHUB_RUN_ID")); runID != "" {
		if value, err := strconv.ParseInt(runID, 10, 64); err == nil && value > 0 {
			return 900000000000 + value
		}
	}
	return 900000000000 + time.Now().UTC().UnixNano()%1000000000
}
