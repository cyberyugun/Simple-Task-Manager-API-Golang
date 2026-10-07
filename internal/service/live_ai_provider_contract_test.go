package service

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
)

func TestLiveRemoteAIProviderContract(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_CONTRACTS")), "true") {
		t.Skip("live provider contracts are opt-in")
	}
	if target := strings.ToLower(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_CONTRACT_TARGET"))); target != "ai_remote" {
		t.Fatalf("unsupported live AI contract target %q", target)
	}

	allowInsecure := strings.EqualFold(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_ALLOW_INSECURE")), "true")
	provider, err := NewRemoteStructuredAIProviderFromEnv(allowInsecure)
	if err != nil {
		t.Fatalf("configure live remote AI provider: %v", err)
	}
	if provider == nil {
		t.Fatal("remote AI provider is not enabled")
	}
	if provider.Key() != model.AIProviderRemoteStructured || !provider.External() {
		t.Fatalf("unexpected remote AI capability key=%q external=%v", provider.Key(), provider.External())
	}

	classification := strings.ToLower(strings.TrimSpace(os.Getenv("LIVE_AI_CLASSIFICATION")))
	if classification == "" {
		classification = model.DataClassificationPublic
	}
	if classification != model.DataClassificationPublic && classification != model.DataClassificationInternal {
		t.Fatalf("LIVE_AI_CLASSIFICATION must be public or internal, got %q", classification)
	}
	if !providerSupportsClassification(provider, classification) {
		t.Fatalf("remote AI provider does not support classification %q", classification)
	}

	runID := strings.TrimSpace(os.Getenv("GITHUB_RUN_ID"))
	if runID == "" {
		runID = "local-contract"
	}
	request := AIProviderRequest{
		Feature:        model.AIFeatureTaskSummary,
		Classification: classification,
		Text:           "Summarize this synthetic contract-validation task: verify the external AI gateway returns a concise structured task summary.",
		Context: map[string]any{
			"contract_test": true,
			"run_id":        runID,
			"source":        "simple-task-manager-live-provider-contract",
		},
	}

	requestID, err := remoteAIRequestID(provider.model, request)
	if err != nil {
		t.Fatalf("derive remote AI request id: %v", err)
	}
	repeatedRequestID, err := remoteAIRequestID(provider.model, request)
	if err != nil {
		t.Fatalf("derive repeated remote AI request id: %v", err)
	}
	if len(requestID) != 64 || requestID != repeatedRequestID {
		t.Fatal("remote AI request id is not deterministic")
	}

	estimate := provider.EstimateCostCents(request)
	if estimate < 0 {
		t.Fatalf("remote AI estimated cost cannot be negative: %d", estimate)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	first, err := provider.Generate(ctx, request)
	if err != nil {
		t.Fatalf("first live remote AI request failed: %v", err)
	}
	liveRemoteAIAssertResponse(t, provider, first)

	second, err := provider.Generate(ctx, request)
	if err != nil {
		t.Fatalf("idempotent live remote AI request failed: %v", err)
	}
	liveRemoteAIAssertResponse(t, provider, second)

	if first.Model != second.Model ||
		first.InputUnits != second.InputUnits ||
		first.OutputUnits != second.OutputUnits ||
		first.ActualCostCents != second.ActualCostCents ||
		!reflect.DeepEqual(first.Result, second.Result) {
		t.Fatalf("remote AI gateway did not return an idempotent duplicate response: first=%+v second=%+v", first, second)
	}

	t.Logf(
		"live remote AI contract passed model=%s classification=%s input_units=%d output_units=%d actual_cost_cents=%d request_id_prefix=%s idempotent=true",
		first.Model,
		classification,
		first.InputUnits,
		first.OutputUnits,
		first.ActualCostCents,
		requestID[:12],
	)
}

func liveRemoteAIAssertResponse(t *testing.T, provider *RemoteStructuredAIProvider, response AIProviderResponse) {
	t.Helper()
	if strings.TrimSpace(response.Model) == "" {
		t.Fatal("remote AI response model is empty")
	}
	if response.InputUnits <= 0 || response.OutputUnits <= 0 {
		t.Fatalf("remote AI response usage must be positive: %+v", response)
	}
	if response.OutputUnits > provider.maxOutputUnits {
		t.Fatalf("remote AI output units=%d exceed configured maximum=%d", response.OutputUnits, provider.maxOutputUnits)
	}
	summary, ok := response.Result["summary"].(string)
	if !ok || strings.TrimSpace(summary) == "" {
		t.Fatalf("remote AI task-summary result is invalid: %+v", response.Result)
	}
	expectedCost := remoteAICostCents(
		response.InputUnits,
		response.OutputUnits,
		provider.inputCostPerThousand,
		provider.outputCostPerThousand,
	)
	if response.ActualCostCents != expectedCost {
		t.Fatalf("remote AI actual cost=%d want=%d", response.ActualCostCents, expectedCost)
	}
}
