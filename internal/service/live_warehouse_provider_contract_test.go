package service

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
)

func TestLiveWarehouseProviderContract(t *testing.T) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_CONTRACTS")), "true") {
		t.Skip("live provider contracts are opt-in")
	}

	target := strings.ToLower(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_CONTRACT_TARGET")))
	adapter, connection := liveWarehouseContractAdapter(t, target)
	liveWarehouseDeliveryRoundTrip(t, target, adapter, connection)
}

func liveWarehouseContractAdapter(t *testing.T, target string) (WarehouseAdapter, model.DataPlatformConnection) {
	t.Helper()
	allowInsecure := strings.EqualFold(strings.TrimSpace(os.Getenv("LIVE_PROVIDER_ALLOW_INSECURE")), "true")
	table := strings.TrimSpace(os.Getenv("LIVE_WAREHOUSE_TABLE"))
	if table == "" {
		table = "stm_provider_contract"
	}

	var (
		adapter WarehouseAdapter
		err     error
	)
	connection := model.DataPlatformConnection{
		Name:   "live-provider-contract",
		Target: table,
		Status: model.DataPlatformStatusActive,
		Config: map[string]string{"table": table},
	}

	switch target {
	case "warehouse_bigquery":
		connection.Provider = model.DataWarehouseBigQuery
		connection.Config["project_id"] = liveWarehouseRequiredEnv(t, "LIVE_WAREHOUSE_BIGQUERY_PROJECT_ID")
		connection.Config["dataset"] = liveWarehouseRequiredEnv(t, "LIVE_WAREHOUSE_BIGQUERY_DATASET")
		adapter, err = NewBigQueryWarehouseAdapterFromEnv(allowInsecure)
	case "warehouse_snowflake":
		connection.Provider = model.DataWarehouseSnowflake
		connection.Config["account"] = liveWarehouseRequiredEnv(t, "LIVE_WAREHOUSE_SNOWFLAKE_ACCOUNT")
		connection.Config["database"] = liveWarehouseRequiredEnv(t, "LIVE_WAREHOUSE_SNOWFLAKE_DATABASE")
		connection.Config["schema"] = liveWarehouseRequiredEnv(t, "LIVE_WAREHOUSE_SNOWFLAKE_SCHEMA")
		connection.Config["warehouse"] = strings.TrimSpace(os.Getenv("LIVE_WAREHOUSE_SNOWFLAKE_WAREHOUSE"))
		connection.Config["role"] = strings.TrimSpace(os.Getenv("LIVE_WAREHOUSE_SNOWFLAKE_ROLE"))
		adapter, err = NewSnowflakeWarehouseAdapterFromEnv(allowInsecure)
	case "warehouse_redshift":
		connection.Provider = model.DataWarehouseRedshift
		connection.Config["cluster"] = liveWarehouseRequiredEnv(t, "LIVE_WAREHOUSE_REDSHIFT_CLUSTER")
		connection.Config["database"] = liveWarehouseRequiredEnv(t, "LIVE_WAREHOUSE_REDSHIFT_DATABASE")
		connection.Config["schema"] = liveWarehouseRequiredEnv(t, "LIVE_WAREHOUSE_REDSHIFT_SCHEMA")
		connection.Config["db_user"] = strings.TrimSpace(os.Getenv("LIVE_WAREHOUSE_REDSHIFT_DB_USER"))
		connection.Config["secret_arn"] = strings.TrimSpace(os.Getenv("LIVE_WAREHOUSE_REDSHIFT_SECRET_ARN"))
		if connection.Config["db_user"] == "" && connection.Config["secret_arn"] == "" {
			t.Fatal("LIVE_WAREHOUSE_REDSHIFT_DB_USER or LIVE_WAREHOUSE_REDSHIFT_SECRET_ARN is required")
		}
		adapter, err = NewRedshiftWarehouseAdapterFromEnv(allowInsecure)
	case "warehouse_databricks":
		connection.Provider = model.DataWarehouseDatabricks
		workspaceURL := firstNonEmpty(
			os.Getenv("LIVE_WAREHOUSE_DATABRICKS_WORKSPACE_URL"),
			os.Getenv("DATA_PLATFORM_DATABRICKS_ENDPOINT"),
		)
		if strings.TrimSpace(workspaceURL) == "" {
			t.Fatal("LIVE_WAREHOUSE_DATABRICKS_WORKSPACE_URL or DATA_PLATFORM_DATABRICKS_ENDPOINT is required")
		}
		connection.Config["workspace_url"] = strings.TrimSpace(workspaceURL)
		connection.Config["warehouse_id"] = liveWarehouseRequiredEnv(t, "LIVE_WAREHOUSE_DATABRICKS_WAREHOUSE_ID")
		connection.Config["catalog"] = liveWarehouseRequiredEnv(t, "LIVE_WAREHOUSE_DATABRICKS_CATALOG")
		connection.Config["schema"] = liveWarehouseRequiredEnv(t, "LIVE_WAREHOUSE_DATABRICKS_SCHEMA")
		adapter, err = NewDatabricksWarehouseAdapterFromEnv(allowInsecure)
	default:
		t.Fatalf("unsupported live warehouse contract target %q", target)
	}
	if err != nil {
		t.Fatalf("configure %s live warehouse contract: %v", target, err)
	}
	if adapter == nil {
		t.Fatalf("%s native warehouse adapter is not enabled", target)
	}
	return adapter, connection
}

func liveWarehouseDeliveryRoundTrip(t *testing.T, target string, adapter WarehouseAdapter, connection model.DataPlatformConnection) {
	t.Helper()
	now := time.Now().UTC()
	runID := strings.TrimSpace(os.Getenv("GITHUB_RUN_ID"))
	if runID == "" {
		runID = strconv.FormatInt(now.UnixNano(), 10)
	}
	row := model.AnalyticsTaskRecord{
		OrganizationID: 900000001,
		WorkspaceID:    900000002,
		TaskID:         now.UnixNano(),
		Title:          "live-provider-contract-" + runID,
		Status:         "contract",
		Priority:       "LOW",
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	first, err := adapter.Deliver(connection, []model.AnalyticsTaskRecord{row})
	if err != nil {
		t.Fatalf("%s first live delivery failed: %v", target, err)
	}
	liveWarehouseAssertReceipt(t, connection, first)

	second, err := adapter.Deliver(connection, []model.AnalyticsTaskRecord{row})
	if err != nil {
		t.Fatalf("%s idempotent live delivery failed: %v", target, err)
	}
	liveWarehouseAssertReceipt(t, connection, second)
	if first.BatchID != second.BatchID || first.PayloadHash != second.PayloadHash || first.DeliveryURI != second.DeliveryURI {
		t.Fatalf("%s repeated delivery was not deterministic", target)
	}

	t.Logf(
		"live warehouse contract passed target=%s provider=%s table=%s rows=%d batch_id=%s idempotent=true",
		target,
		first.Provider,
		connection.Config["table"],
		first.RowCount,
		first.BatchID,
	)
}

func liveWarehouseAssertReceipt(t *testing.T, connection model.DataPlatformConnection, receipt model.WarehouseDeliveryReceipt) {
	t.Helper()
	if receipt.Provider != connection.Provider {
		t.Fatalf("receipt provider=%q want=%q", receipt.Provider, connection.Provider)
	}
	if receipt.Target != connection.Target {
		t.Fatalf("receipt target=%q want=%q", receipt.Target, connection.Target)
	}
	if receipt.RowCount != 1 || receipt.Bytes <= 0 {
		t.Fatalf("invalid delivery receipt row_count=%d bytes=%d", receipt.RowCount, receipt.Bytes)
	}
	if len(receipt.PayloadHash) != 64 || strings.TrimSpace(receipt.BatchID) == "" || strings.TrimSpace(receipt.DeliveryURI) == "" {
		t.Fatalf("incomplete delivery receipt: %+v", receipt)
	}
}

func liveWarehouseRequiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	return value
}
