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

type SnowflakeWarehouseConfig struct {
	Endpoint      string
	Token         string
	TokenType     string
	Timeout       time.Duration
	RetryAttempts int
	RetryBackoff  time.Duration
	PollInterval  time.Duration
	AllowInsecure bool
}

type SnowflakeWarehouseAdapter struct {
	endpoint      *url.URL
	token         string
	tokenType     string
	client        *http.Client
	retryAttempts int
	retryBackoff  time.Duration
	pollInterval  time.Duration
	allowInsecure bool
}

type snowflakeResponse struct {
	StatementHandle    string `json:"statementHandle"`
	StatementStatusURL string `json:"statementStatusUrl"`
	Message            string `json:"message"`
	Code               string `json:"code"`
	SQLState           string `json:"sqlState"`
}

func NewSnowflakeWarehouseAdapter(cfg SnowflakeWarehouseConfig) (*SnowflakeWarehouseAdapter, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("%w: Snowflake token is required", ErrInvalidDataPlatformConnection)
	}
	var endpoint *url.URL
	var err error
	if strings.TrimSpace(cfg.Endpoint) != "" {
		endpoint, err = parseSnowflakeEndpoint(cfg.Endpoint, cfg.AllowInsecure)
		if err != nil {
			return nil, err
		}
	}
	tokenType := strings.ToUpper(strings.TrimSpace(cfg.TokenType))
	if tokenType == "" {
		tokenType = "OAUTH"
	}
	if tokenType != "OAUTH" && tokenType != "PROGRAMMATIC_ACCESS_TOKEN" && tokenType != "KEYPAIR_JWT" {
		return nil, fmt.Errorf("%w: unsupported Snowflake token type", ErrInvalidDataPlatformConnection)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.RetryAttempts <= 0 {
		cfg.RetryAttempts = 3
	}
	if cfg.RetryAttempts > 6 {
		return nil, fmt.Errorf("%w: Snowflake retry attempts too high", ErrInvalidDataPlatformConnection)
	}
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = 300 * time.Millisecond
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 250 * time.Millisecond
	}
	return &SnowflakeWarehouseAdapter{
		endpoint: endpoint, token: strings.TrimSpace(cfg.Token), tokenType: tokenType,
		client:        &http.Client{Timeout: cfg.Timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
		retryAttempts: cfg.RetryAttempts, retryBackoff: cfg.RetryBackoff, pollInterval: cfg.PollInterval,
		allowInsecure: cfg.AllowInsecure,
	}, nil
}

func NewSnowflakeWarehouseAdapterFromEnv(allowInsecure bool) (*SnowflakeWarehouseAdapter, error) {
	raw := strings.TrimSpace(os.Getenv("DATA_PLATFORM_SNOWFLAKE_NATIVE"))
	if raw == "" {
		return nil, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: DATA_PLATFORM_SNOWFLAKE_NATIVE must be true or false", ErrInvalidDataPlatformConnection)
	}
	if !enabled {
		return nil, nil
	}
	timeout, err := snowflakeDurationEnv("DATA_PLATFORM_SNOWFLAKE_TIMEOUT", 30*time.Second)
	if err != nil {
		return nil, err
	}
	backoff, err := snowflakeDurationEnv("DATA_PLATFORM_SNOWFLAKE_RETRY_BACKOFF", 300*time.Millisecond)
	if err != nil {
		return nil, err
	}
	poll, err := snowflakeDurationEnv("DATA_PLATFORM_SNOWFLAKE_POLL_INTERVAL", 250*time.Millisecond)
	if err != nil {
		return nil, err
	}
	attempts := 3
	if value := strings.TrimSpace(os.Getenv("DATA_PLATFORM_SNOWFLAKE_RETRY_ATTEMPTS")); value != "" {
		attempts, err = strconv.Atoi(value)
		if err != nil || attempts < 1 || attempts > 6 {
			return nil, fmt.Errorf("%w: DATA_PLATFORM_SNOWFLAKE_RETRY_ATTEMPTS must be 1..6", ErrInvalidDataPlatformConnection)
		}
	}
	return NewSnowflakeWarehouseAdapter(SnowflakeWarehouseConfig{
		Endpoint: os.Getenv("DATA_PLATFORM_SNOWFLAKE_ENDPOINT"), Token: os.Getenv("DATA_PLATFORM_SNOWFLAKE_TOKEN"),
		TokenType: os.Getenv("DATA_PLATFORM_SNOWFLAKE_TOKEN_TYPE"), Timeout: timeout, RetryAttempts: attempts,
		RetryBackoff: backoff, PollInterval: poll, AllowInsecure: allowInsecure,
	})
}

func (a *SnowflakeWarehouseAdapter) Capability() model.WarehouseAdapterCapability {
	return model.WarehouseAdapterCapability{
		Key: model.DataWarehouseSnowflake, DisplayName: "Snowflake (native SQL API)", DeliveryMode: "sql_api_merge_live",
		BIContracts: []string{model.BIContractPowerBI, model.BIContractTableau, model.BIContractLooker}, SupportsRecovery: true,
	}
}

func (a *SnowflakeWarehouseAdapter) Deliver(connection model.DataPlatformConnection, rows []model.AnalyticsTaskRecord) (model.WarehouseDeliveryReceipt, error) {
	database := strings.TrimSpace(connection.Config["database"])
	schema := strings.TrimSpace(connection.Config["schema"])
	table := strings.TrimSpace(connection.Config["table"])
	if table == "" {
		table = snowflakeTableFromTarget(connection.Target)
	}
	for _, value := range []string{database, schema, table} {
		if !validSnowflakeIdentifier(value) {
			return model.WarehouseDeliveryReceipt{}, ErrInvalidDataPlatformConnection
		}
	}
	endpoint, err := a.endpointForConnection(connection)
	if err != nil {
		return model.WarehouseDeliveryReceipt{}, err
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return model.WarehouseDeliveryReceipt{}, err
	}
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	receipt := model.WarehouseDeliveryReceipt{
		Provider: model.DataWarehouseSnowflake, Target: connection.Target,
		DeliveryURI: fmt.Sprintf("snowflake://%s/%s/%s/batches/%s", database, schema, table, hash[:16]),
		BatchID:     hash[:16], RowCount: len(rows), Bytes: int64(len(payload)), PayloadHash: hash,
	}
	if len(rows) == 0 {
		return receipt, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.client.Timeout)
	defer cancel()
	if err := a.run(ctx, endpoint, connection, snowflakeCreateTableSQL(database, schema, table), snowflakeRequestID("create:"+database+":"+schema+":"+table)); err != nil {
		return model.WarehouseDeliveryReceipt{}, err
	}
	const batchSize = 100
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		id := snowflakeRequestID(fmt.Sprintf("merge:%s:%s:%s:%s:%d", database, schema, table, hash, start))
		if err := a.run(ctx, endpoint, connection, snowflakeMergeSQL(database, schema, table, rows[start:end]), id); err != nil {
			return model.WarehouseDeliveryReceipt{}, err
		}
	}
	return receipt, nil
}

func (a *SnowflakeWarehouseAdapter) run(ctx context.Context, endpoint *url.URL, connection model.DataPlatformConnection, statement, requestID string) error {
	body := map[string]any{"statement": statement, "timeout": int(a.client.Timeout.Seconds()), "database": connection.Config["database"], "schema": connection.Config["schema"]}
	for _, key := range []string{"warehouse", "role"} {
		if value := strings.TrimSpace(connection.Config[key]); value != "" {
			if !validSnowflakeIdentifier(value) {
				return ErrInvalidDataPlatformConnection
			}
			body[key] = value
		}
	}
	u := *endpoint
	u.Path = strings.TrimRight(u.Path, "/") + "/api/v2/statements"
	baseQuery := u.Query()
	baseQuery.Set("requestId", requestID)
	var lastErr error
	for attempt := 0; attempt < a.retryAttempts; attempt++ {
		q := baseQuery
		if attempt > 0 {
			q.Set("retry", "true")
		}
		u.RawQuery = q.Encode()
		status, raw, err := a.request(ctx, http.MethodPost, u.String(), body)
		if err != nil {
			lastErr = err
		} else if status == http.StatusOK || status == http.StatusCreated {
			return nil
		} else if status == http.StatusAccepted {
			var pending snowflakeResponse
			if json.Unmarshal(raw, &pending) != nil {
				return fmt.Errorf("%w: invalid Snowflake async response", ErrInvalidDataExportRequest)
			}
			return a.poll(ctx, endpoint, pending)
		} else {
			lastErr = snowflakeError(status, raw)
			if !snowflakeRetryableStatus(status) {
				return lastErr
			}
		}
		if attempt+1 < a.retryAttempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(a.retryBackoff * time.Duration(attempt+1)):
			}
		}
	}
	return lastErr
}

func (a *SnowflakeWarehouseAdapter) poll(ctx context.Context, endpoint *url.URL, pending snowflakeResponse) error {
	rawURL := strings.TrimSpace(pending.StatementStatusURL)
	if rawURL == "" && pending.StatementHandle != "" {
		rawURL = "/api/v2/statements/" + url.PathEscape(pending.StatementHandle)
	}
	statusURL, err := url.Parse(rawURL)
	if err != nil || rawURL == "" {
		return fmt.Errorf("%w: invalid Snowflake statement status URL", ErrInvalidDataExportRequest)
	}
	resolved := endpoint.ResolveReference(statusURL)
	if !strings.EqualFold(resolved.Scheme, endpoint.Scheme) || !strings.EqualFold(resolved.Host, endpoint.Host) {
		return fmt.Errorf("%w: Snowflake statement status host mismatch", ErrInvalidDataExportRequest)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(a.pollInterval):
		}
		status, raw, err := a.request(ctx, http.MethodGet, resolved.String(), nil)
		if err != nil {
			return err
		}
		if status == http.StatusOK || status == http.StatusCreated {
			return nil
		}
		if status != http.StatusAccepted {
			return snowflakeError(status, raw)
		}
	}
}

func (a *SnowflakeWarehouseAdapter) request(ctx context.Context, method, endpoint string, body any) (int, []byte, error) {
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
	req.Header.Set("X-Snowflake-Authorization-Token-Type", a.tokenType)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: Snowflake request failed: %v", ErrInvalidDataExportRequest, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	return resp.StatusCode, raw, err
}

func (a *SnowflakeWarehouseAdapter) endpointForConnection(connection model.DataPlatformConnection) (*url.URL, error) {
	if a.endpoint != nil {
		copy := *a.endpoint
		return &copy, nil
	}
	account := strings.ToLower(strings.TrimSpace(connection.Config["account"]))
	if !validSnowflakeAccount(account) {
		return nil, ErrInvalidDataPlatformConnection
	}
	return parseSnowflakeEndpoint("https://"+account+".snowflakecomputing.com", a.allowInsecure)
}

func snowflakeCreateTableSQL(database, schema, table string) string {
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (organization_id NUMBER(38,0) NOT NULL, workspace_id NUMBER(38,0) NOT NULL, task_id NUMBER(38,0) NOT NULL, title TEXT NOT NULL, status TEXT NOT NULL, priority TEXT NOT NULL, project_id NUMBER(38,0), due_at TIMESTAMP_TZ, completed_at TIMESTAMP_TZ, created_at TIMESTAMP_TZ NOT NULL, updated_at TIMESTAMP_TZ NOT NULL, PRIMARY KEY (organization_id, workspace_id, task_id))", snowflakeQualifiedTable(database, schema, table))
}

func snowflakeMergeSQL(database, schema, table string, rows []model.AnalyticsTaskRecord) string {
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		values = append(values, "("+strings.Join([]string{
			strconv.FormatInt(row.OrganizationID, 10), strconv.FormatInt(row.WorkspaceID, 10), strconv.FormatInt(row.TaskID, 10),
			snowflakeString(row.Title), snowflakeString(row.Status), snowflakeString(row.Priority), snowflakeInt(row.ProjectID),
			snowflakeTimestamp(row.DueAt), snowflakeTimestamp(row.CompletedAt), snowflakeTimestamp(&row.CreatedAt), snowflakeTimestamp(&row.UpdatedAt),
		}, ",")+")")
	}
	return fmt.Sprintf("MERGE INTO %s t USING (SELECT column1 organization_id,column2 workspace_id,column3 task_id,column4 title,column5 status,column6 priority,column7 project_id,column8 due_at,column9 completed_at,column10 created_at,column11 updated_at FROM VALUES %s) s ON t.organization_id=s.organization_id AND t.workspace_id=s.workspace_id AND t.task_id=s.task_id WHEN MATCHED THEN UPDATE SET title=s.title,status=s.status,priority=s.priority,project_id=s.project_id,due_at=s.due_at,completed_at=s.completed_at,created_at=s.created_at,updated_at=s.updated_at WHEN NOT MATCHED THEN INSERT (organization_id,workspace_id,task_id,title,status,priority,project_id,due_at,completed_at,created_at,updated_at) VALUES (s.organization_id,s.workspace_id,s.task_id,s.title,s.status,s.priority,s.project_id,s.due_at,s.completed_at,s.created_at,s.updated_at)", snowflakeQualifiedTable(database, schema, table), strings.Join(values, ","))
}

func snowflakeQualifiedTable(database, schema, table string) string {
	return strings.ToUpper(database) + "." + strings.ToUpper(schema) + "." + strings.ToUpper(table)
}
func snowflakeString(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
func snowflakeInt(value *int64) string {
	if value == nil {
		return "NULL"
	}
	return strconv.FormatInt(*value, 10)
}
func snowflakeTimestamp(value *time.Time) string {
	if value == nil {
		return "NULL"
	}
	return "TO_TIMESTAMP_TZ(" + snowflakeString(value.UTC().Format(time.RFC3339Nano)) + ")"
}
func snowflakeTableFromTarget(target string) string {
	target = strings.Trim(strings.TrimSpace(target), ".")
	if i := strings.LastIndex(target, "."); i >= 0 {
		return strings.TrimSpace(target[i+1:])
	}
	return target
}
func validSnowflakeIdentifier(value string) bool {
	if value == "" || len(value) > 255 {
		return false
	}
	for i, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r == '$') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}
func validSnowflakeAccount(value string) bool {
	if value == "" || len(value) > 255 || strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}
func parseSnowflakeEndpoint(raw string, allowInsecure bool) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return nil, fmt.Errorf("%w: invalid Snowflake endpoint", ErrInvalidDataPlatformConnection)
	}
	if u.Scheme != "https" && !(allowInsecure && u.Scheme == "http") {
		return nil, fmt.Errorf("%w: Snowflake endpoint must use HTTPS", ErrInvalidDataPlatformConnection)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}
func snowflakeError(status int, raw []byte) error {
	var response snowflakeResponse
	_ = json.Unmarshal(raw, &response)
	message := strings.TrimSpace(response.Message)
	if message == "" {
		message = strings.TrimSpace(string(raw))
	}
	return fmt.Errorf("%w: Snowflake HTTP %d code=%s sqlstate=%s %s", ErrInvalidDataExportRequest, status, response.Code, response.SQLState, message)
}
func snowflakeRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout || status >= 500
}
func snowflakeRequestID(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	value := hex.EncodeToString(sum[:16])
	return value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:32]
}
func snowflakeDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
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
