package service

import (
	"context"
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

type remoteAITestState struct {
	mu              sync.Mutex
	calls           int
	failures        int
	requestIDs      []string
	bodyRequestIDs  []string
	classifications []string
}

func newRemoteAITestServer(t *testing.T, failures int) (*httptest.Server, *remoteAITestState) {
	t.Helper()
	state := &remoteAITestState{failures: failures}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/generate" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer remote-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body remoteAIRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		state.mu.Lock()
		state.calls++
		state.requestIDs = append(state.requestIDs, r.Header.Get("Idempotency-Key"))
		state.bodyRequestIDs = append(state.bodyRequestIDs, body.RequestID)
		state.classifications = append(state.classifications, r.Header.Get("X-AI-Classification"))
		call := state.calls
		state.mu.Unlock()
		if body.Model != "remote-model-v1" || body.Feature != model.AIFeatureTaskSummary || body.MaxOutputUnits != 1000 {
			t.Fatalf("unexpected request body: %+v", body)
		}
		if call <= failures {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "TEMPORARY", "message": "retry"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request_id": body.RequestID,
			"model":      "remote-model-v1",
			"result":     map[string]any{"summary": "safe structured summary"},
			"usage":      map[string]any{"input_units": 1000, "output_units": 500},
		})
	}))
	return server, state
}

func newRemoteAITestProvider(t *testing.T, endpoint string, attempts int) *RemoteStructuredAIProvider {
	t.Helper()
	provider, err := NewRemoteStructuredAIProvider(RemoteStructuredAIProviderConfig{
		Endpoint: endpoint, Token: "remote-token", Model: "remote-model-v1",
		SupportedClassifications: []string{model.DataClassificationPublic, model.DataClassificationInternal},
		RetryAttempts:            attempts, RetryBackoff: time.Millisecond, Timeout: 2 * time.Second,
		MaxOutputUnits: 1000, InputCostCentsPerThousand: 10, OutputCostCentsPerThousand: 20,
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestRemoteStructuredAIProviderGenerateAndCostAccounting(t *testing.T) {
	server, state := newRemoteAITestServer(t, 0)
	defer server.Close()
	provider := newRemoteAITestProvider(t, server.URL+"/v1/generate", 2)
	req := AIProviderRequest{
		Feature: model.AIFeatureTaskSummary, Text: "test", Classification: model.DataClassificationInternal,
		Context: map[string]any{},
	}
	if estimate := provider.EstimateCostCents(req); estimate != 21 {
		t.Fatalf("estimate=%d want=21", estimate)
	}
	result, err := provider.Generate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Model != "remote-model-v1" || result.InputUnits != 1000 || result.OutputUnits != 500 || result.ActualCostCents != 20 {
		t.Fatalf("result=%+v", result)
	}
	if result.Result["summary"] != "safe structured summary" {
		t.Fatalf("result=%+v", result.Result)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.calls != 1 || len(state.requestIDs) != 1 || len(state.requestIDs[0]) != 64 || state.requestIDs[0] != state.bodyRequestIDs[0] {
		t.Fatalf("state=%+v", state)
	}
	if state.classifications[0] != model.DataClassificationInternal {
		t.Fatalf("classification=%q", state.classifications[0])
	}
}

func TestRemoteStructuredAIProviderRetriesWithStableIdempotencyKey(t *testing.T) {
	server, state := newRemoteAITestServer(t, 1)
	defer server.Close()
	provider := newRemoteAITestProvider(t, server.URL+"/v1/generate", 3)
	_, err := provider.Generate(context.Background(), AIProviderRequest{
		Feature: model.AIFeatureTaskSummary, Text: "retry", Classification: model.DataClassificationInternal,
		Context: map[string]any{"workspace_id": int64(5)},
	})
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.calls != 2 || state.requestIDs[0] == "" || state.requestIDs[0] != state.requestIDs[1] ||
		state.bodyRequestIDs[0] != state.bodyRequestIDs[1] {
		t.Fatalf("retry state=%+v", state)
	}
}

func TestRemoteStructuredAIProviderRejectsMalformedStructuredResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body remoteAIRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"request_id": body.RequestID,
			"model":      "remote-model-v1",
			"result":     map[string]any{"priority": "IMPOSSIBLE"},
			"usage":      map[string]any{"input_units": 10, "output_units": 5},
		})
	}))
	defer server.Close()
	provider := newRemoteAITestProvider(t, server.URL+"/v1/generate", 1)
	_, err := provider.Generate(context.Background(), AIProviderRequest{
		Feature: model.AIFeaturePrioritySuggestion, Text: "priority", Classification: model.DataClassificationInternal,
		Context: map[string]any{},
	})
	if !errors.Is(err, ErrInvalidAIAssistanceRequest) {
		t.Fatalf("error=%v", err)
	}
}

func TestRemoteStructuredAIProviderRejectsUnsafeConfiguration(t *testing.T) {
	if _, err := NewRemoteStructuredAIProvider(RemoteStructuredAIProviderConfig{
		Endpoint: "http://ai.example.com/v1/generate", Token: "token", Model: "model",
	}); err == nil {
		t.Fatal("expected insecure endpoint rejection")
	}
	if _, err := NewRemoteStructuredAIProvider(RemoteStructuredAIProviderConfig{
		Endpoint: "https://ai.example.com/v1/generate", Model: "model",
	}); err == nil {
		t.Fatal("expected missing token rejection")
	}
}

type recordingExternalAIProvider struct {
	request AIProviderRequest
}

func (p *recordingExternalAIProvider) Key() string         { return "recording_external" }
func (p *recordingExternalAIProvider) DisplayName() string { return "Recording External" }
func (p *recordingExternalAIProvider) External() bool      { return true }
func (p *recordingExternalAIProvider) SupportedClassifications() []string {
	return []string{model.DataClassificationPublic, model.DataClassificationInternal}
}
func (p *recordingExternalAIProvider) EstimateCostCents(AIProviderRequest) int64 { return 1 }
func (p *recordingExternalAIProvider) Generate(_ context.Context, req AIProviderRequest) (AIProviderResponse, error) {
	p.request = req
	return AIProviderResponse{
		Model: "recording-v1", Result: map[string]any{"summary": "ok"},
		InputUnits: 1, OutputUnits: 1, ActualCostCents: 1,
	}, nil
}

func TestAIAssistanceRedactsExternalProviderContext(t *testing.T) {
	svc, _, tasks, _, orgID, workspaceID := newAITestService(t)
	provider := &recordingExternalAIProvider{}
	svc.providers.Register(provider)
	if _, err := svc.UpdatePolicy(7, orgID, model.UpdateAIPolicyRequest{
		Enabled: true, Provider: provider.Key(), MonthlyBudgetCents: 100,
		RedactionEnabled: true, MaxInputChars: 12000,
		AllowedClassifications:         []string{model.DataClassificationInternal},
		ExternalMaxClassification:      model.DataClassificationInternal,
		RequireHumanApprovalForActions: true,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task, err := tasks.Create(model.Task{
		WorkspaceID: workspaceID, UserID: 7,
		Title:       "Contact owner@example.com token=abcd1234",
		Description: "password=secret123 before deployment",
		Status:      model.TaskStatusTodo, Priority: model.TaskPriorityMedium,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.Assist(context.Background(), 7, orgID, model.AIAssistRequest{
		Feature: model.AIFeatureTaskSummary, WorkspaceID: &workspaceID, TaskID: &task.ID,
		Classification: model.DataClassificationInternal,
	})
	if err != nil {
		t.Fatal(err)
	}
	contextJSON, _ := json.Marshal(provider.request.Context)
	providerPayload := strings.ToLower(provider.request.Text + " " + string(contextJSON))
	if strings.Contains(providerPayload, "owner@example.com") || strings.Contains(providerPayload, "abcd1234") || strings.Contains(providerPayload, "secret123") {
		t.Fatalf("sensitive data reached external provider: %s", providerPayload)
	}
	if run.RedactionCount < 4 {
		t.Fatalf("redaction_count=%d want>=4", run.RedactionCount)
	}
	if value, ok := aiInt64(run.PromptMetadata["context_redaction_count"]); !ok || value < 2 {
		t.Fatalf("prompt metadata=%+v", run.PromptMetadata)
	}
}
