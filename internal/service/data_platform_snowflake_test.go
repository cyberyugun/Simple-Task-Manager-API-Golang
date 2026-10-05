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

type snowflakeTestState struct {
	mu         sync.Mutex
	createCall int
	mergeCall  int
	failMerge  int
	statements []string
	requestIDs []string
}

func newSnowflakeTestServer(t *testing.T, token string, failMerge int) (*httptest.Server, *snowflakeTestState) {
	t.Helper()
	state := &snowflakeTestState{failMerge: failMerge}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/statements" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-Snowflake-Authorization-Token-Type") != "OAUTH" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var body struct {
			Statement string `json:"statement"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		state.statements = append(state.statements, body.Statement)
		state.requestIDs = append(state.requestIDs, r.URL.Query().Get("requestId"))
		w.Header().Set("Content-Type", "application/json")
		upper := strings.ToUpper(strings.TrimSpace(body.Statement))
		switch {
		case strings.HasPrefix(upper, "CREATE TABLE IF NOT EXISTS"):
			state.createCall++
			_ = json.NewEncoder(w).Encode(map[string]any{"statementHandle": "create-ok"})
		case strings.HasPrefix(upper, "MERGE INTO"):
			state.mergeCall++
			if state.mergeCall <= state.failMerge {
				w.WriteHeader(http.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(map[string]any{"code": "390100", "message": "temporary"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"statementHandle": "merge-ok"})
		default:
			t.Fatalf("unexpected statement: %s", body.Statement)
		}
	}))
	return server, state
}

func TestSnowflakeWarehouseAdapterCreatesTableAndMergesRows(t *testing.T) {
	server, state := newSnowflakeTestServer(t, "snow-token", 0)
	defer server.Close()

	adapter, err := NewSnowflakeWarehouseAdapter(SnowflakeWarehouseConfig{
		Endpoint: server.URL, Token: "snow-token", RetryAttempts: 2,
		RetryBackoff: time.Millisecond, PollInterval: time.Millisecond, AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	receipt, err := adapter.Deliver(model.DataPlatformConnection{
		Provider: model.DataWarehouseSnowflake, Target: "analytics.task_analytics",
		Config: map[string]string{"account": "org-account", "database": "analytics_db", "schema": "analytics", "warehouse": "compute_wh"},
	}, []model.AnalyticsTaskRecord{{
		OrganizationID: 7, WorkspaceID: 11, TaskID: 101, Title: "O'Reilly warehouse",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityHigh,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Provider != model.DataWarehouseSnowflake || receipt.RowCount != 1 || receipt.PayloadHash == "" ||
		!strings.HasPrefix(receipt.DeliveryURI, "snowflake://analytics_db/analytics/task_analytics/") {
		t.Fatalf("receipt=%+v", receipt)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.createCall != 1 || state.mergeCall != 1 || len(state.requestIDs) != 2 {
		t.Fatalf("state=%+v", state)
	}
	if !strings.Contains(state.statements[1], "O''Reilly warehouse") {
		t.Fatalf("task title was not SQL escaped: %s", state.statements[1])
	}
	for _, requestID := range state.requestIDs {
		if len(requestID) != 36 {
			t.Fatalf("request id=%q", requestID)
		}
	}
}

func TestSnowflakeWarehouseAdapterRetriesTransientMerge(t *testing.T) {
	server, state := newSnowflakeTestServer(t, "snow-token", 1)
	defer server.Close()
	adapter, err := NewSnowflakeWarehouseAdapter(SnowflakeWarehouseConfig{
		Endpoint: server.URL, Token: "snow-token", RetryAttempts: 3,
		RetryBackoff: time.Millisecond, PollInterval: time.Millisecond, AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = adapter.Deliver(model.DataPlatformConnection{
		Provider: model.DataWarehouseSnowflake, Target: "analytics.task_analytics",
		Config: map[string]string{"account": "org-account", "database": "analytics_db", "schema": "analytics"},
	}, []model.AnalyticsTaskRecord{{
		OrganizationID: 1, WorkspaceID: 2, TaskID: 3, Title: "retry",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityMedium, CreatedAt: now, UpdatedAt: now,
	}})
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.mergeCall != 2 {
		t.Fatalf("merge calls=%d want=2", state.mergeCall)
	}
	if len(state.requestIDs) != 3 || state.requestIDs[1] != state.requestIDs[2] {
		t.Fatalf("retry request id changed: %v", state.requestIDs)
	}
}

func TestNativeSnowflakeFailureDoesNotAdvanceCheckpoint(t *testing.T) {
	server, _ := newSnowflakeTestServer(t, "snow-token", 10)
	defer server.Close()
	adapter, err := NewSnowflakeWarehouseAdapter(SnowflakeWarehouseConfig{
		Endpoint: server.URL, Token: "snow-token", RetryAttempts: 2,
		RetryBackoff: time.Millisecond, PollInterval: time.Millisecond, AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc, tasks, orgID, workspaceID := newDataPlatformTestService(t)
	svc.RegisterWarehouseAdapter(adapter)
	connection, err := svc.CreateConnection(1, orgID, model.CreateDataPlatformConnectionRequest{
		Name: "Snowflake Warehouse", Provider: model.DataWarehouseSnowflake, Target: "analytics.task_analytics",
		BIContracts: []string{model.BIContractPowerBI},
		Config: map[string]string{"account": "org-account", "database": "analytics_db", "schema": "analytics"},
		SecretRef: "secret://warehouse/snowflake", Masking: model.DataMaskingPolicy{Mode: model.MaskingNone},
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
		t.Fatal("checkpoint advanced after failed Snowflake delivery")
	}
}

func TestSnowflakeWarehouseAdapterRejectsUnsafeConfiguration(t *testing.T) {
	if _, err := NewSnowflakeWarehouseAdapter(SnowflakeWarehouseConfig{
		Endpoint: "http://account.snowflakecomputing.com", Token: "token",
	}); err == nil {
		t.Fatal("expected insecure endpoint rejection")
	}
	if _, err := NewSnowflakeWarehouseAdapter(SnowflakeWarehouseConfig{
		Endpoint: "https://account.snowflakecomputing.com",
	}); err == nil {
		t.Fatal("expected missing token rejection")
	}
}
