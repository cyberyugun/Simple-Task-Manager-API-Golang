package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
)

type DatabricksWarehouseConfig struct {
	Endpoint      string
	Token         string
	Timeout       time.Duration
	RetryAttempts int
	RetryBackoff  time.Duration
	PollInterval  time.Duration
	AllowInsecure bool
}

type DatabricksWarehouseAdapter struct {
	endpoint      *url.URL
	token         string
	client        *http.Client
	retryAttempts int
	retryBackoff  time.Duration
	pollInterval  time.Duration
	allowInsecure bool
}

type databricksStatementResponse struct {
	StatementID string `json:"statement_id"`
	Status      struct {
		State string `json:"state"`
		Error struct {
			ErrorCode string `json:"error_code"`
			Message   string `json:"message"`
		} `json:"error"`
	} `json:"status"`
}

type databricksAPIError struct {
	Status  int
	Code    string
	Message string
}

func (e *databricksAPIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("Databricks HTTP %d %s", e.Status, e.Code)
	}
	return fmt.Sprintf("Databricks HTTP %d %s: %s", e.Status, e.Code, e.Message)
}

func NewDatabricksWarehouseAdapter(cfg DatabricksWarehouseConfig) (*DatabricksWarehouseAdapter, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("%w: Databricks access token is required", ErrInvalidDataPlatformConnection)
	}
	var endpoint *url.URL
	var err error
	if strings.TrimSpace(cfg.Endpoint) != "" {
		endpoint, err = parseDatabricksEndpoint(cfg.Endpoint, cfg.AllowInsecure)
		if err != nil {
			return nil, err
		}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	attempts := cfg.RetryAttempts
	if attempts <= 0 {
		attempts = 3
	}
	if attempts > 6 {
		return nil, fmt.Errorf("%w: Databricks retry attempts too high", ErrInvalidDataPlatformConnection)
	}
	backoff := cfg.RetryBackoff
	if backoff <= 0 {
		backoff = 300 * time.Millisecond
	}
	poll := cfg.PollInterval
	if poll <= 0 {
		poll = 300 * time.Millisecond
	}
	return &DatabricksWarehouseAdapter{
		endpoint: endpoint,
		token:    strings.TrimSpace(cfg.Token),
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		retryAttempts: attempts,
		retryBackoff:  backoff,
		pollInterval:  poll,
		allowInsecure: cfg.AllowInsecure,
	}, nil
}

func NewDatabricksWarehouseAdapterFromEnv(allowInsecure bool) (*DatabricksWarehouseAdapter, error) {
	raw := strings.TrimSpace(os.Getenv("DATA_PLATFORM_DATABRICKS_NATIVE"))
	if raw == "" {
		return nil, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: DATA_PLATFORM_DATABRICKS_NATIVE must be true or false", ErrInvalidDataPlatformConnection)
	}
	if !enabled {
		return nil, nil
	}
	timeout, err := databricksDurationEnv("DATA_PLATFORM_DATABRICKS_TIMEOUT", 30*time.Second)
	if err != nil {
		return nil, err
	}
	backoff, err := databricksDurationEnv("DATA_PLATFORM_DATABRICKS_RETRY_BACKOFF", 300*time.Millisecond)
	if err != nil {
		return nil, err
	}
	poll, err := databricksDurationEnv("DATA_PLATFORM_DATABRICKS_POLL_INTERVAL", 300*time.Millisecond)
	if err != nil {
		return nil, err
	}
	attempts := 3
	if value := strings.TrimSpace(os.Getenv("DATA_PLATFORM_DATABRICKS_RETRY_ATTEMPTS")); value != "" {
		attempts, err = strconv.Atoi(value)
		if err != nil || attempts < 1 || attempts > 6 {
			return nil, fmt.Errorf("%w: DATA_PLATFORM_DATABRICKS_RETRY_ATTEMPTS must be 1..6", ErrInvalidDataPlatformConnection)
		}
	}
	return NewDatabricksWarehouseAdapter(DatabricksWarehouseConfig{
		Endpoint:      os.Getenv("DATA_PLATFORM_DATABRICKS_ENDPOINT"),
		Token:         firstNonEmpty(os.Getenv("DATA_PLATFORM_DATABRICKS_TOKEN"), os.Getenv("DATABRICKS_TOKEN")),
		Timeout:       timeout,
		RetryAttempts: attempts,
		RetryBackoff:  backoff,
		PollInterval:  poll,
		AllowInsecure: allowInsecure,
	})
}

func (a *DatabricksWarehouseAdapter) Capability() model.WarehouseAdapterCapability {
	return model.WarehouseAdapterCapability{
		Key:              model.DataWarehouseDatabricks,
		DisplayName:      "Databricks (native SQL Statement Execution)",
		DeliveryMode:     "statement_execution_delta_merge_live",
		BIContracts:      []string{model.BIContractPowerBI, model.BIContractTableau, model.BIContractLooker},
		SupportsRecovery: true,
	}
}

func (a *DatabricksWarehouseAdapter) Deliver(connection model.DataPlatformConnection, rows []model.AnalyticsTaskRecord) (model.WarehouseDeliveryReceipt, error) {
	base, err := a.endpointForConnection(connection)
	if err != nil {
		return model.WarehouseDeliveryReceipt{}, err
	}
	warehouseID := strings.TrimSpace(connection.Config["warehouse_id"])
	catalog := strings.TrimSpace(connection.Config["catalog"])
	schema := strings.TrimSpace(connection.Config["schema"])
	table := strings.TrimSpace(connection.Config["table"])
	if table == "" {
		table = databricksTableFromTarget(connection.Target)
	}
	if !validDatabricksWarehouseID(warehouseID) || !validDatabricksIdentifier(catalog) ||
		!validDatabricksIdentifier(schema) || !validDatabricksIdentifier(table) {
		return model.WarehouseDeliveryReceipt{}, ErrInvalidDataPlatformConnection
	}

	payload, err := json.Marshal(rows)
	if err != nil {
		return model.WarehouseDeliveryReceipt{}, err
	}
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	receipt := model.WarehouseDeliveryReceipt{
		Provider:    model.DataWarehouseDatabricks,
		Target:      connection.Target,
		DeliveryURI: fmt.Sprintf("databricks://%s/%s/%s/%s/batches/%s", base.Host, catalog, schema, table, hash[:16]),
		BatchID:     hash[:16],
		RowCount:    len(rows),
		Bytes:       int64(len(payload)),
		PayloadHash: hash,
	}
	if len(rows) == 0 {
		return receipt, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), a.client.Timeout)
	defer cancel()
	if err := a.executeAndWait(ctx, base, warehouseID, catalog, schema, databricksCreateTableSQL(catalog, schema, table)); err != nil {
		return model.WarehouseDeliveryReceipt{}, err
	}
	const batchSize = 100
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := a.executeAndWait(ctx, base, warehouseID, catalog, schema, databricksMergeSQL(catalog, schema, table, rows[start:end])); err != nil {
			return model.WarehouseDeliveryReceipt{}, err
		}
	}
	return receipt, nil
}

func (a *DatabricksWarehouseAdapter) executeAndWait(ctx context.Context, base *url.URL, warehouseID, catalog, schema, statement string) error {
	body := map[string]any{
		"warehouse_id":    warehouseID,
		"catalog":         catalog,
		"schema":          schema,
		"statement":       statement,
		"wait_timeout":    "0s",
		"on_wait_timeout": "CONTINUE",
		"format":          "JSON_ARRAY",
		"disposition":     "INLINE",
	}
	endpoint := databricksStatementEndpoint(base)
	status, raw, err := a.requestWithRetry(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return decodeDatabricksAPIError(status, raw)
	}
	var submitted databricksStatementResponse
	if err := json.Unmarshal(raw, &submitted); err != nil {
		return fmt.Errorf("%w: invalid Databricks execute response", ErrInvalidDataExportRequest)
	}
	if err := databricksStatementTerminalError(submitted); err != nil {
		return err
	}
	if strings.EqualFold(submitted.Status.State, "SUCCEEDED") {
		return nil
	}
	statementID := strings.TrimSpace(submitted.StatementID)
	if statementID == "" {
		return fmt.Errorf("%w: Databricks returned empty statement id", ErrInvalidDataExportRequest)
	}

	statusEndpoint := strings.TrimRight(endpoint, "/") + "/" + url.PathEscape(statementID)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(a.pollInterval):
		}
		status, raw, err := a.requestWithRetry(ctx, http.MethodGet, statusEndpoint, nil)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return decodeDatabricksAPIError(status, raw)
		}
		var current databricksStatementResponse
		if err := json.Unmarshal(raw, &current); err != nil {
			return fmt.Errorf("%w: invalid Databricks status response", ErrInvalidDataExportRequest)
		}
		if err := databricksStatementTerminalError(current); err != nil {
			return err
		}
		switch strings.ToUpper(strings.TrimSpace(current.Status.State)) {
		case "SUCCEEDED":
			return nil
		case "PENDING", "RUNNING":
		default:
			return fmt.Errorf("%w: unknown Databricks statement state %q", ErrInvalidDataExportRequest, current.Status.State)
		}
	}
}

func (a *DatabricksWarehouseAdapter) requestWithRetry(ctx context.Context, method, endpoint string, body any) (int, []byte, error) {
	var lastStatus int
	var lastRaw []byte
	var lastErr error
	for attempt := 0; attempt < a.retryAttempts; attempt++ {
		status, raw, err := a.request(ctx, method, endpoint, body)
		lastStatus, lastRaw, lastErr = status, raw, err
		if err == nil && status >= 200 && status < 300 {
			return status, raw, nil
		}
		if err == nil && !databricksRetryableStatus(status) {
			return status, raw, nil
		}
		if attempt+1 < a.retryAttempts {
			select {
			case <-ctx.Done():
				return 0, nil, ctx.Err()
			case <-time.After(a.retryBackoff * time.Duration(attempt+1)):
			}
		}
	}
	if lastErr != nil {
		return 0, nil, lastErr
	}
	return lastStatus, lastRaw, nil
}

func (a *DatabricksWarehouseAdapter) request(ctx context.Context, method, endpoint string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "simple-task-manager-databricks/1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: Databricks request failed: %v", ErrInvalidDataExportRequest, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}

func (a *DatabricksWarehouseAdapter) endpointForConnection(connection model.DataPlatformConnection) (*url.URL, error) {
	if a.endpoint != nil {
		copy := *a.endpoint
		return &copy, nil
	}
	return parseDatabricksEndpoint(connection.Config["workspace_url"], a.allowInsecure)
}

func databricksStatementEndpoint(base *url.URL) string {
	copy := *base
	copy.Path = strings.TrimRight(copy.Path, "/") + "/api/2.0/sql/statements"
	copy.RawQuery = ""
	return copy.String()
}

func databricksCreateTableSQL(catalog, schema, table string) string {
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (organization_id BIGINT NOT NULL, workspace_id BIGINT NOT NULL, task_id BIGINT NOT NULL, title STRING NOT NULL, status STRING NOT NULL, priority STRING NOT NULL, project_id BIGINT, due_at TIMESTAMP, completed_at TIMESTAMP, created_at TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL) USING DELTA", databricksQualifiedTable(catalog, schema, table))
}

func databricksMergeSQL(catalog, schema, table string, rows []model.AnalyticsTaskRecord) string {
	selects := make([]string, 0, len(rows))
	for _, row := range rows {
		selects = append(selects, "SELECT "+strings.Join([]string{
			strconv.FormatInt(row.OrganizationID, 10) + " AS organization_id",
			strconv.FormatInt(row.WorkspaceID, 10) + " AS workspace_id",
			strconv.FormatInt(row.TaskID, 10) + " AS task_id",
			databricksString(row.Title) + " AS title",
			databricksString(row.Status) + " AS status",
			databricksString(row.Priority) + " AS priority",
			databricksInt(row.ProjectID) + " AS project_id",
			databricksTimestamp(row.DueAt) + " AS due_at",
			databricksTimestamp(row.CompletedAt) + " AS completed_at",
			databricksTimestamp(&row.CreatedAt) + " AS created_at",
			databricksTimestamp(&row.UpdatedAt) + " AS updated_at",
		}, ","))
	}
	return fmt.Sprintf("MERGE INTO %s AS t USING (%s) AS s ON t.organization_id=s.organization_id AND t.workspace_id=s.workspace_id AND t.task_id=s.task_id WHEN MATCHED THEN UPDATE SET title=s.title,status=s.status,priority=s.priority,project_id=s.project_id,due_at=s.due_at,completed_at=s.completed_at,created_at=s.created_at,updated_at=s.updated_at WHEN NOT MATCHED THEN INSERT (organization_id,workspace_id,task_id,title,status,priority,project_id,due_at,completed_at,created_at,updated_at) VALUES (s.organization_id,s.workspace_id,s.task_id,s.title,s.status,s.priority,s.project_id,s.due_at,s.completed_at,s.created_at,s.updated_at)", databricksQualifiedTable(catalog, schema, table), strings.Join(selects, " UNION ALL "))
}

func databricksQualifiedTable(catalog, schema, table string) string {
	return databricksQuotedIdentifier(catalog) + "." + databricksQuotedIdentifier(schema) + "." + databricksQuotedIdentifier(table)
}

func databricksQuotedIdentifier(value string) string {
	return "`" + value + "`"
}

func databricksString(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "'", "''")
	return "'" + value + "'"
}

func databricksInt(value *int64) string {
	if value == nil {
		return "CAST(NULL AS BIGINT)"
	}
	return strconv.FormatInt(*value, 10)
}

func databricksTimestamp(value *time.Time) string {
	if value == nil {
		return "CAST(NULL AS TIMESTAMP)"
	}
	return "CAST(" + databricksString(value.UTC().Format(time.RFC3339Nano)) + " AS TIMESTAMP)"
}

func databricksTableFromTarget(target string) string {
	target = strings.Trim(strings.TrimSpace(target), ".")
	if i := strings.LastIndex(target, "."); i >= 0 {
		return strings.TrimSpace(target[i+1:])
	}
	return target
}

func validDatabricksIdentifier(value string) bool {
	if value == "" || len(value) > 255 || strings.Contains(value, "`") {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validDatabricksWarehouseID(value string) bool {
	if value == "" || len(value) > 255 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func parseDatabricksEndpoint(raw string, allowInsecure bool) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" || endpoint.RawQuery != "" {
		return nil, fmt.Errorf("%w: invalid Databricks workspace endpoint", ErrInvalidDataPlatformConnection)
	}
	if endpoint.Scheme != "https" && !(allowInsecure && endpoint.Scheme == "http") {
		return nil, fmt.Errorf("%w: Databricks workspace endpoint must use HTTPS", ErrInvalidDataPlatformConnection)
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/")
	return endpoint, nil
}

func databricksStatementTerminalError(response databricksStatementResponse) error {
	switch strings.ToUpper(strings.TrimSpace(response.Status.State)) {
	case "", "PENDING", "RUNNING", "SUCCEEDED":
		return nil
	case "FAILED", "CANCELED", "CLOSED":
		message := strings.TrimSpace(response.Status.Error.Message)
		code := strings.TrimSpace(response.Status.Error.ErrorCode)
		if message == "" {
			message = "statement did not finish successfully"
		}
		if code != "" {
			message = code + ": " + message
		}
		return fmt.Errorf("%w: Databricks statement %s: %s", ErrInvalidDataExportRequest, strings.ToLower(response.Status.State), message)
	default:
		return nil
	}
}

func decodeDatabricksAPIError(status int, raw []byte) error {
	apiErr := &databricksAPIError{Status: status, Code: "Unknown"}
	var body struct {
		ErrorCode string `json:"error_code"`
		Message   string `json:"message"`
	}
	if json.Unmarshal(raw, &body) == nil {
		if strings.TrimSpace(body.ErrorCode) != "" {
			apiErr.Code = strings.TrimSpace(body.ErrorCode)
		}
		apiErr.Message = strings.TrimSpace(body.Message)
	}
	return apiErr
}

func databricksRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout ||
		status == http.StatusBadGateway || status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout || status >= 500
}

func databricksDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%w: %s must be a positive duration", ErrInvalidDataPlatformConnection, name)
	}
	return value, nil
}
