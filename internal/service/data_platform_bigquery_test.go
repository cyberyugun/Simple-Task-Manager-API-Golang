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

type bigQueryTestState struct {
	mu            sync.Mutex
	tableExists   bool
	createCalls   int
	insertCalls   int
	failInserts   int
	insertIDs     []string
	lastRows      []map[string]any
	authorization []string
}

func newBigQueryTestServer(t *testing.T, token string, failInserts int) (*httptest.Server, *bigQueryTestState) {
	t.Helper()
	state := &bigQueryTestState{failInserts: failInserts}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		state.authorization = append(state.authorization, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")

		tablePath := "/bigquery/v2/projects/test-project/datasets/analytics/tables/task_analytics"
		switch {
		case r.Method == http.MethodGet && r.URL.Path == tablePath:
			if !state.tableExists {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"message":"not found"}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tableReference": map[string]string{
					"projectId": "test-project", "datasetId": "analytics", "tableId": "task_analytics",
				},
				"schema": map[string]any{"fields": bigQuerySchemaFields()},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/bigquery/v2/projects/test-project/datasets/analytics/tables":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			state.tableExists = true
			state.createCalls++
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(body)
		case r.Method == http.MethodPost && r.URL.Path == tablePath+"/insertAll":
			state.insertCalls++
			if state.insertCalls <= state.failInserts {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":{"message":"temporary"}}`))
				return
			}
			var body struct {
				Rows []struct {
					InsertID string         `json:"insertId"`
					JSON     map[string]any `json:"json"`
				} `json:"rows"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			for _, row := range body.Rows {
				state.insertIDs = append(state.insertIDs, row.InsertID)
				state.lastRows = append(state.lastRows, row.JSON)
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	return server, state
}

func TestBigQueryWarehouseAdapterCreatesTableAndDeliversRows(t *testing.T) {
	server, state := newBigQueryTestServer(t, "bq-token", 0)
	defer server.Close()

	adapter, err := NewBigQueryWarehouseAdapter(BigQueryWarehouseConfig{
		Endpoint: server.URL + "/bigquery/v2",
		AccessToken: "bq-token",
		RetryAttempts: 2,
		RetryBackoff: time.Millisecond,
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	rows := []model.AnalyticsTaskRecord{
		{
			OrganizationID: 7, WorkspaceID: 11, TaskID: 101, Title: "Warehouse row",
			Status: model.TaskStatusTodo, Priority: model.TaskPriorityHigh,
			CreatedAt: now.Add(-time.Hour), UpdatedAt: now,
		},
	}
	connection := model.DataPlatformConnection{
		Provider: model.DataWarehouseBigQuery, Target: "analytics.task_analytics",
		Config: map[string]string{"project_id": "test-project", "dataset": "analytics"},
	}
	receipt, err := adapter.Deliver(connection, rows)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Provider != model.DataWarehouseBigQuery || receipt.RowCount != 1 ||
		receipt.PayloadHash == "" || !strings.HasPrefix(receipt.DeliveryURI, "bigquery://test-project/analytics/task_analytics/") {
		t.Fatalf("receipt=%+v", receipt)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.tableExists || state.createCalls != 1 || state.insertCalls != 1 || len(state.lastRows) != 1 {
		t.Fatalf("state=%+v", state)
	}
	if len(state.insertIDs) != 1 || len(state.insertIDs[0]) != 64 {
		t.Fatalf("insert ids=%v", state.insertIDs)
	}
	if got := state.lastRows[0]["title"]; got != "Warehouse row" {
		t.Fatalf("title=%v", got)
	}
}

func TestBigQueryWarehouseAdapterRetriesTransientInsert(t *testing.T) {
	server, state := newBigQueryTestServer(t, "bq-token", 1)
	defer server.Close()

	adapter, err := NewBigQueryWarehouseAdapter(BigQueryWarehouseConfig{
		Endpoint: server.URL + "/bigquery/v2",
		AccessToken: "bq-token",
		RetryAttempts: 3,
		RetryBackoff: time.Millisecond,
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_, err = adapter.Deliver(model.DataPlatformConnection{
		Provider: model.DataWarehouseBigQuery, Target: "analytics.task_analytics",
		Config: map[string]string{"project_id": "test-project", "dataset": "analytics"},
	}, []model.AnalyticsTaskRecord{{
		OrganizationID: 1, WorkspaceID: 2, TaskID: 3, Title: "retry",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityMedium,
		CreatedAt: now, UpdatedAt: now,
	}})
	if err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.insertCalls != 2 {
		t.Fatalf("insert calls=%d want=2", state.insertCalls)
	}
}

func TestNativeBigQueryFailureDoesNotAdvanceCheckpoint(t *testing.T) {
	server, _ := newBigQueryTestServer(t, "bq-token", 10)
	defer server.Close()

	adapter, err := NewBigQueryWarehouseAdapter(BigQueryWarehouseConfig{
		Endpoint: server.URL + "/bigquery/v2",
		AccessToken: "bq-token",
		RetryAttempts: 2,
		RetryBackoff: time.Millisecond,
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc, tasks, orgID, workspaceID := newDataPlatformTestService(t)
	svc.RegisterWarehouseAdapter(adapter)
	connection := testBigQueryConnection(t, svc, orgID, model.DataMaskingPolicy{Mode: model.MaskingNone})

	now := time.Now().UTC()
	if _, err := tasks.Create(model.Task{
		WorkspaceID: workspaceID, UserID: 1, Title: "do not checkpoint",
		Status: model.TaskStatusTodo, Priority: model.TaskPriorityHigh,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	job, err := svc.RunExport(1, orgID, connection.ID, model.RunDataExportRequest{})
	if err == nil || job.Status != model.DataExportFailed {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	if _, err := svc.Checkpoint(1, orgID, connection.ID); err == nil {
		t.Fatal("checkpoint advanced after failed native BigQuery delivery")
	}
}

func TestBigQueryWarehouseAdapterRejectsUnsafeConfiguration(t *testing.T) {
	if _, err := NewBigQueryWarehouseAdapter(BigQueryWarehouseConfig{
		Endpoint: "http://bigquery.example.test/bigquery/v2",
		AccessToken: "token",
	}); err == nil {
		t.Fatal("expected insecure endpoint rejection")
	}
	if _, err := NewBigQueryWarehouseAdapter(BigQueryWarehouseConfig{
		Endpoint: "https://bigquery.googleapis.com/bigquery/v2",
		UseMetadata: false,
	}); err == nil {
		t.Fatal("expected missing identity rejection")
	}
}

func TestBigQueryTableTargetParsing(t *testing.T) {
	if got := bigQueryTableFromTarget("analytics.task_analytics"); got != "task_analytics" {
		t.Fatalf("table=%q", got)
	}
	if got := bigQueryTableFromTarget("task_analytics"); got != "task_analytics" {
		t.Fatalf("table=%q", got)
	}
}
