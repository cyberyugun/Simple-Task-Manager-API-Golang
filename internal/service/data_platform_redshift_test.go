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

type redshiftTestState struct {
	mu            sync.Mutex
	executeCalls  int
	describeCalls int
	mergeCalls    int
	failMerges    int
	clientTokens  []string
	statements    []string
	authHeaders   []string
	statementType map[string]string
}

func newRedshiftTestServer(t *testing.T, failMerges int) (*httptest.Server, *redshiftTestState) {
	t.Helper()
	state := &redshiftTestState{failMerges: failMerges, statementType: map[string]string{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/") {
			http.Error(w, "unsigned", http.StatusUnauthorized)
			return
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		state.authHeaders = append(state.authHeaders, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		switch r.Header.Get("X-Amz-Target") {
		case "RedshiftData.ExecuteStatement":
			var body struct {
				SQL         string `json:"Sql"`
				ClientToken string `json:"ClientToken"`
				Cluster     string `json:"ClusterIdentifier"`
				Database    string `json:"Database"`
				DBUser      string `json:"DbUser"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Cluster != "analytics-cluster" || body.Database != "analytics_db" || body.DBUser != "app_user" {
				t.Fatalf("unexpected execute body: %+v", body)
			}
			state.executeCalls++
			state.clientTokens = append(state.clientTokens, body.ClientToken)
			state.statements = append(state.statements, body.SQL)
			kind := "create"
			if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(body.SQL)), "MERGE INTO") {
				kind = "merge"
				state.mergeCalls++
				if state.mergeCalls <= state.failMerges {
					w.WriteHeader(http.StatusServiceUnavailable)
					_ = json.NewEncoder(w).Encode(map[string]any{"__type": "InternalServerException", "message": "temporary"})
					return
				}
			}
			id := kind + "-statement"
			state.statementType[id] = kind
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": id})
		case "RedshiftData.DescribeStatement":
			var body struct {
				ID string `json:"Id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			state.describeCalls++
			if state.statementType[body.ID] == "" {
				t.Fatalf("unknown statement id: %s", body.ID)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"Status": "FINISHED"})
		default:
			t.Fatalf("unexpected target: %s", r.Header.Get("X-Amz-Target"))
		}
	}))
	return server, state
}

func newTestRedshiftAdapter(t *testing.T, endpoint string, failAttempts int) *RedshiftWarehouseAdapter {
	t.Helper()
	adapter, err := NewRedshiftWarehouseAdapter(RedshiftWarehouseConfig{
		Region: "us-east-1", Endpoint: endpoint,
		AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "secret-example-key",
		RetryAttempts: failAttempts, RetryBackoff: time.Millisecond, PollInterval: time.Millisecond,
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func testRedshiftConnection() model.DataPlatformConnection {
	return model.DataPlatformConnection{
		Provider: model.DataWarehouseRedshift, Target: "analytics.task_analytics",
		Config: map[string]string{
			"cluster": "analytics-cluster", "database": "analytics_db", "schema": "analytics", "db_user": "app_user",
		},
	}
}

func TestRedshiftWarehouseAdapterCreatesTableAndMergesRows(t *testing.T) {
	server, state := newRedshiftTestServer(t, 0)
	defer server.Close()
	adapter := newTestRedshiftAdapter(t, server.URL, 2)
	now := time.Now().UTC().Truncate(time.Microsecond)
	receipt, err := adapter.Deliver(testRedshiftConnection(), []model.AnalyticsTaskRecord{{
		OrganizationID: 7, WorkspaceID: 11, TaskID: 101, Title: "O'Reilly warehouse",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityHigh,
		CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Provider != model.DataWarehouseRedshift || receipt.RowCount != 1 || receipt.PayloadHash == "" ||
		!strings.HasPrefix(receipt.DeliveryURI, "redshift://analytics-cluster/analytics_db/analytics/task_analytics/") {
		t.Fatalf("receipt=%+v", receipt)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.executeCalls != 2 || state.describeCalls != 2 || state.mergeCalls != 1 {
		t.Fatalf("state=%+v", state)
	}
	if len(state.statements) != 2 || !strings.Contains(state.statements[1], "O''Reilly warehouse") {
		t.Fatalf("merge SQL did not escape title: %v", state.statements)
	}
	if len(state.clientTokens) != 2 || len(state.clientTokens[0]) != 64 || len(state.clientTokens[1]) != 64 {
		t.Fatalf("client tokens=%v", state.clientTokens)
	}
}

func TestRedshiftWarehouseAdapterRetriesTransientExecute(t *testing.T) {
	server, state := newRedshiftTestServer(t, 1)
	defer server.Close()
	adapter := newTestRedshiftAdapter(t, server.URL, 3)
	now := time.Now().UTC()
	_, err := adapter.Deliver(testRedshiftConnection(), []model.AnalyticsTaskRecord{{
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
	if len(state.clientTokens) != 3 || state.clientTokens[1] != state.clientTokens[2] {
		t.Fatalf("retry client token changed: %v", state.clientTokens)
	}
}

func TestNativeRedshiftFailureDoesNotAdvanceCheckpoint(t *testing.T) {
	server, _ := newRedshiftTestServer(t, 10)
	defer server.Close()
	adapter := newTestRedshiftAdapter(t, server.URL, 2)
	svc, tasks, orgID, workspaceID := newDataPlatformTestService(t)
	svc.RegisterWarehouseAdapter(adapter)
	connection, err := svc.CreateConnection(1, orgID, model.CreateDataPlatformConnectionRequest{
		Name: "Redshift Warehouse", Provider: model.DataWarehouseRedshift, Target: "analytics.task_analytics",
		BIContracts: []string{model.BIContractPowerBI},
		Config: map[string]string{
			"cluster": "analytics-cluster", "database": "analytics_db", "schema": "analytics", "db_user": "app_user",
		},
		SecretRef: "secret://warehouse/redshift", Masking: model.DataMaskingPolicy{Mode: model.MaskingNone},
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
		t.Fatal("checkpoint advanced after failed Redshift delivery")
	}
}

func TestRedshiftWarehouseAdapterRejectsUnsafeConfiguration(t *testing.T) {
	if _, err := NewRedshiftWarehouseAdapter(RedshiftWarehouseConfig{
		Region: "us-east-1", Endpoint: "http://redshift-data.us-east-1.amazonaws.com",
		AccessKeyID: "AKID", SecretAccessKey: "secret",
	}); err == nil {
		t.Fatal("expected insecure endpoint rejection")
	}
	adapter := newTestRedshiftAdapter(t, "http://127.0.0.1:1", 1)
	connection := testRedshiftConnection()
	delete(connection.Config, "db_user")
	if _, err := adapter.Deliver(connection, []model.AnalyticsTaskRecord{{OrganizationID: 1}}); err == nil {
		t.Fatal("expected db_user or secret_arn requirement")
	}
}
