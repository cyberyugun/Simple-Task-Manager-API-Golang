package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
)

type databricksTestState struct {
	mu                     sync.Mutex
	createCalls            int
	mergeCalls             int
	statusCalls            int
	transientMergeFailures int
	terminalMergeFailure   bool
	statements             []string
	statementKinds         map[string]string
}

func newDatabricksTestServer(t *testing.T, transientMergeFailures int, terminalMergeFailure bool) (*httptest.Server, *databricksTestState) {
	t.Helper()
	state := &databricksTestState{
		transientMergeFailures: transientMergeFailures,
		terminalMergeFailure:   terminalMergeFailure,
		statementKinds:         map[string]string{},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer db-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/api/2.0/sql/statements" {
			var body struct {
				WarehouseID string `json:"warehouse_id"`
				Catalog     string `json:"catalog"`
				Schema      string `json:"schema"`
				Statement   string `json:"statement"`
				WaitTimeout string `json:"wait_timeout"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.WarehouseID != "warehouse-123" || body.Catalog != "analytics" || body.Schema != "reporting" || body.WaitTimeout != "0s" {
				t.Fatalf("unexpected Databricks execute body: %+v", body)
			}
			state.mu.Lock()
			defer state.mu.Unlock()
			state.statements = append(state.statements, body.Statement)
			kind := "create"
			if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(body.Statement)), "MERGE INTO") {
				kind = "merge"
				state.mergeCalls++
				if state.mergeCalls <= state.transientMergeFailures {
					w.WriteHeader(http.StatusServiceUnavailable)
					_ = json.NewEncoder(w).Encode(map[string]any{"error_code": "TEMPORARILY_UNAVAILABLE", "message": "retry"})
					return
				}
			} else {
				state.createCalls++
			}
			id := kind + "-statement"
			state.statementKinds[id] = kind
			_ = json.NewEncoder(w).Encode(map[string]any{
				"statement_id": id,
				"status":       map[string]any{"state": "PENDING"},
			})
			return
		}
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/2.0/sql/statements/") {
			id := strings.TrimPrefix(r.URL.Path, "/api/2.0/sql/statements/")
			state.mu.Lock()
			defer state.mu.Unlock()
			kind := state.statementKinds[id]
			if kind == "" {
				http.NotFound(w, r)
				return
			}
			state.statusCalls++
			if kind == "merge" && state.terminalMergeFailure {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"statement_id": id,
					"status": map[string]any{
						"state": "FAILED",
						"error": map[string]any{"error_code": "BAD_REQUEST", "message": "merge failed"},
					},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"statement_id": id,
				"status":       map[string]any{"state": "SUCCEEDED"},
			})
			return
		}
		http.NotFound(w, r)
	}))
	return server, state
}

func newTestDatabricksAdapter(t *testing.T, endpoint string, attempts int) *DatabricksWarehouseAdapter {
	t.Helper()
	adapter, err := NewDatabricksWarehouseAdapter(DatabricksWarehouseConfig{
		Endpoint: endpoint, Token: "db-token", RetryAttempts: attempts,
		RetryBackoff: time.Millisecond, PollInterval: time.Millisecond, AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func testDatabricksConnection() model.DataPlatformConnection {
	return model.DataPlatformConnection{
		Provider: model.DataWarehouseDatabricks, Target: "analytics.reporting.task_analytics",
		Config: map[string]string{
			"workspace_url": "https://workspace.example.databricks.com",
			"warehouse_id":  "warehouse-123",
			"catalog":       "analytics",
			"schema":        "reporting",
		},
	}
}

func TestDatabricksWarehouseAdapterCreatesDeltaTableAndMergesRows(t *testing.T) {
	server, state := newDatabricksTestServer(t, 0, false)
	defer server.Close()
	adapter := newTestDatabricksAdapter(t, server.URL, 2)
	now := time.Now().UTC().Truncate(time.Microsecond)
	receipt, err := adapter.Deliver(testDatabricksConnection(), []model.AnalyticsTaskRecord{{
		OrganizationID: 7, WorkspaceID: 11, TaskID: 101, Title: "O'Reilly \\ launch",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityHigh,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Provider != model.DataWarehouseDatabricks || receipt.RowCount != 1 || receipt.PayloadHash == "" ||
		!strings.Contains(receipt.DeliveryURI, "/analytics/reporting/task_analytics/") {
		t.Fatalf("receipt=%+v", receipt)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.createCalls != 1 || state.mergeCalls != 1 || state.statusCalls != 2 {
		t.Fatalf("state=%+v", state)
	}
	if len(state.statements) != 2 || !strings.Contains(state.statements[0], "USING DELTA") ||
		!strings.Contains(state.statements[1], "O''Reilly") || !strings.Contains(state.statements[1], "\\\\ launch") {
		t.Fatalf("unexpected SQL statements: %v", state.statements)
	}
}

func TestDatabricksWarehouseAdapterRetriesTransientMerge(t *testing.T) {
	server, state := newDatabricksTestServer(t, 1, false)
	defer server.Close()
	adapter := newTestDatabricksAdapter(t, server.URL, 3)
	now := time.Now().UTC()
	_, err := adapter.Deliver(testDatabricksConnection(), []model.AnalyticsTaskRecord{{
		OrganizationID: 1, WorkspaceID: 2, TaskID: 3, Title: "retry",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityMedium, CreatedAt: now, UpdatedAt: now,
	}})
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.mergeCalls != 2 {
		t.Fatalf("merge calls=%d want=2", state.mergeCalls)
	}
	if len(state.statements) != 3 || state.statements[1] != state.statements[2] {
		t.Fatalf("retry SQL changed: %v", state.statements)
	}
}

func TestNativeDatabricksFailureDoesNotAdvanceCheckpoint(t *testing.T) {
	server, _ := newDatabricksTestServer(t, 0, true)
	defer server.Close()
	adapter := newTestDatabricksAdapter(t, server.URL, 2)
	svc, tasks, orgID, workspaceID := newDataPlatformTestService(t)
	svc.RegisterWarehouseAdapter(adapter)
	connection, err := svc.CreateConnection(1, orgID, model.CreateDataPlatformConnectionRequest{
		Name: "Databricks Warehouse", Provider: model.DataWarehouseDatabricks, Target: "analytics.reporting.task_analytics",
		BIContracts: []string{model.BIContractPowerBI},
		Config: map[string]string{
			"workspace_url": "https://workspace.example.databricks.com",
			"warehouse_id":  "warehouse-123",
			"catalog":       "analytics",
			"schema":        "reporting",
		},
		SecretRef: "secret://warehouse/databricks", Masking: model.DataMaskingPolicy{Mode: model.MaskingNone},
		FreshnessSLOMinutes: 60, MaxMonthlyCostUSD: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := tasks.Create(model.Task{
		WorkspaceID: workspaceID, UserID: 1, Title: "do not checkpoint",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityHigh, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	job, err := svc.RunExport(1, orgID, connection.ID, model.RunDataExportRequest{})
	if err == nil || job.Status != model.DataExportFailed {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	if _, err := svc.Checkpoint(1, orgID, connection.ID); err == nil {
		t.Fatal("checkpoint advanced after failed Databricks delivery")
	}
}

func TestDatabricksWarehouseAdapterRejectsUnsafeConfiguration(t *testing.T) {
	if _, err := NewDatabricksWarehouseAdapter(DatabricksWarehouseConfig{
		Endpoint: "http://workspace.example.databricks.com", Token: "token",
	}); err == nil {
		t.Fatal("expected insecure endpoint rejection")
	}
	if _, err := NewDatabricksWarehouseAdapter(DatabricksWarehouseConfig{
		Endpoint: "https://workspace.example.databricks.com",
	}); err == nil {
		t.Fatal("expected missing token rejection")
	}
	adapter := newTestDatabricksAdapter(t, "http://127.0.0.1:1", 1)
	connection := testDatabricksConnection()
	delete(connection.Config, "warehouse_id")
	if _, err := adapter.Deliver(connection, []model.AnalyticsTaskRecord{{OrganizationID: 1}}); err == nil {
		t.Fatal("expected warehouse_id validation")
	}
}
