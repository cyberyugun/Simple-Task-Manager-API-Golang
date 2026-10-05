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

type RedshiftWarehouseConfig struct {
	Region               string
	Endpoint             string
	STSEndpoint          string
	RoleARN              string
	WebIdentityTokenFile string
	RoleSessionName      string
	AccessKeyID          string
	SecretAccessKey      string
	SessionToken         string
	Timeout              time.Duration
	RetryAttempts        int
	RetryBackoff         time.Duration
	PollInterval         time.Duration
	AllowInsecure        bool
}

type RedshiftWarehouseAdapter struct {
	region        string
	endpoint      *url.URL
	client        *http.Client
	credentials   awsCredentialProvider
	retryAttempts int
	retryBackoff  time.Duration
	pollInterval  time.Duration
}

type redshiftDataAPIError struct {
	Status  int
	Code    string
	Message string
}

func (e *redshiftDataAPIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("Redshift Data API %s (HTTP %d)", e.Code, e.Status)
	}
	return fmt.Sprintf("Redshift Data API %s: %s", e.Code, e.Message)
}

func NewRedshiftWarehouseAdapter(cfg RedshiftWarehouseConfig) (*RedshiftWarehouseAdapter, error) {
	region := strings.TrimSpace(cfg.Region)
	if !validRedshiftAWSRegion(region) {
		return nil, fmt.Errorf("%w: valid AWS region is required for Redshift", ErrInvalidDataPlatformConnection)
	}
	endpointRaw := strings.TrimSpace(cfg.Endpoint)
	if endpointRaw == "" {
		endpointRaw = "https://redshift-data." + region + ".amazonaws.com"
	}
	endpoint, err := parseRedshiftEndpoint(endpointRaw, cfg.AllowInsecure)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("%w: Redshift retry attempts too high", ErrInvalidDataPlatformConnection)
	}
	backoff := cfg.RetryBackoff
	if backoff <= 0 {
		backoff = 300 * time.Millisecond
	}
	poll := cfg.PollInterval
	if poll <= 0 {
		poll = 300 * time.Millisecond
	}
	credentials, err := newAWSCredentialProvider(AWSSecretsManagerConfig{
		Region: region, STSEndpoint: cfg.STSEndpoint, RoleARN: cfg.RoleARN,
		WebIdentityTokenFile: cfg.WebIdentityTokenFile, RoleSessionName: cfg.RoleSessionName,
		AccessKeyID: cfg.AccessKeyID, SecretAccessKey: cfg.SecretAccessKey, SessionToken: cfg.SessionToken,
		Timeout: timeout, AllowInsecure: cfg.AllowInsecure,
	}, region, timeout)
	if err != nil {
		return nil, err
	}
	return &RedshiftWarehouseAdapter{
		region: region, endpoint: endpoint,
		client:      &http.Client{Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
		credentials: credentials, retryAttempts: attempts, retryBackoff: backoff, pollInterval: poll,
	}, nil
}

func NewRedshiftWarehouseAdapterFromEnv(allowInsecure bool) (*RedshiftWarehouseAdapter, error) {
	raw := strings.TrimSpace(os.Getenv("DATA_PLATFORM_REDSHIFT_NATIVE"))
	if raw == "" {
		return nil, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: DATA_PLATFORM_REDSHIFT_NATIVE must be true or false", ErrInvalidDataPlatformConnection)
	}
	if !enabled {
		return nil, nil
	}
	timeout, err := redshiftDurationEnv("DATA_PLATFORM_REDSHIFT_TIMEOUT", 30*time.Second)
	if err != nil {
		return nil, err
	}
	backoff, err := redshiftDurationEnv("DATA_PLATFORM_REDSHIFT_RETRY_BACKOFF", 300*time.Millisecond)
	if err != nil {
		return nil, err
	}
	poll, err := redshiftDurationEnv("DATA_PLATFORM_REDSHIFT_POLL_INTERVAL", 300*time.Millisecond)
	if err != nil {
		return nil, err
	}
	attempts := 3
	if value := strings.TrimSpace(os.Getenv("DATA_PLATFORM_REDSHIFT_RETRY_ATTEMPTS")); value != "" {
		attempts, err = strconv.Atoi(value)
		if err != nil || attempts < 1 || attempts > 6 {
			return nil, fmt.Errorf("%w: DATA_PLATFORM_REDSHIFT_RETRY_ATTEMPTS must be 1..6", ErrInvalidDataPlatformConnection)
		}
	}
	region := firstNonEmpty(
		os.Getenv("DATA_PLATFORM_REDSHIFT_REGION"),
		os.Getenv("AWS_REGION"),
		os.Getenv("AWS_DEFAULT_REGION"),
	)
	return NewRedshiftWarehouseAdapter(RedshiftWarehouseConfig{
		Region: region, Endpoint: os.Getenv("DATA_PLATFORM_REDSHIFT_ENDPOINT"), STSEndpoint: os.Getenv("DATA_PLATFORM_REDSHIFT_STS_ENDPOINT"),
		RoleARN: os.Getenv("AWS_ROLE_ARN"), WebIdentityTokenFile: os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE"),
		RoleSessionName: os.Getenv("AWS_ROLE_SESSION_NAME"), AccessKeyID: os.Getenv("AWS_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"), SessionToken: os.Getenv("AWS_SESSION_TOKEN"),
		Timeout: timeout, RetryAttempts: attempts, RetryBackoff: backoff, PollInterval: poll, AllowInsecure: allowInsecure,
	})
}

func (a *RedshiftWarehouseAdapter) Capability() model.WarehouseAdapterCapability {
	return model.WarehouseAdapterCapability{
		Key: model.DataWarehouseRedshift, DisplayName: "Amazon Redshift (native Data API)", DeliveryMode: "data_api_merge_live",
		BIContracts: []string{model.BIContractPowerBI, model.BIContractTableau, model.BIContractLooker}, SupportsRecovery: true,
	}
}

func (a *RedshiftWarehouseAdapter) Deliver(connection model.DataPlatformConnection, rows []model.AnalyticsTaskRecord) (model.WarehouseDeliveryReceipt, error) {
	cluster := strings.TrimSpace(connection.Config["cluster"])
	database := strings.TrimSpace(connection.Config["database"])
	schema := strings.TrimSpace(connection.Config["schema"])
	table := strings.TrimSpace(connection.Config["table"])
	if table == "" {
		table = redshiftTableFromTarget(connection.Target)
	}
	if !validRedshiftClusterIdentifier(cluster) || !validRedshiftIdentifier(database) || !validRedshiftIdentifier(schema) || !validRedshiftIdentifier(table) {
		return model.WarehouseDeliveryReceipt{}, ErrInvalidDataPlatformConnection
	}
	if strings.TrimSpace(connection.Config["db_user"]) == "" && strings.TrimSpace(connection.Config["secret_arn"]) == "" {
		return model.WarehouseDeliveryReceipt{}, fmt.Errorf("%w: Redshift native delivery requires db_user or secret_arn", ErrInvalidDataPlatformConnection)
	}
	payload, err := json.Marshal(rows)
	if err != nil {
		return model.WarehouseDeliveryReceipt{}, err
	}
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	receipt := model.WarehouseDeliveryReceipt{
		Provider: model.DataWarehouseRedshift, Target: connection.Target,
		DeliveryURI: fmt.Sprintf("redshift://%s/%s/%s/%s/batches/%s", cluster, database, schema, table, hash[:16]),
		BatchID:     hash[:16], RowCount: len(rows), Bytes: int64(len(payload)), PayloadHash: hash,
	}
	if len(rows) == 0 {
		return receipt, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.client.Timeout)
	defer cancel()
	if err := a.executeAndWait(ctx, connection, redshiftCreateTableSQL(schema, table), redshiftClientToken("create:"+cluster+":"+database+":"+schema+":"+table)); err != nil {
		return model.WarehouseDeliveryReceipt{}, err
	}
	const batchSize = 100
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		token := redshiftClientToken(fmt.Sprintf("merge:%s:%s:%s:%s:%s:%d", cluster, database, schema, table, hash, start))
		if err := a.executeAndWait(ctx, connection, redshiftMergeSQL(schema, table, rows[start:end]), token); err != nil {
			return model.WarehouseDeliveryReceipt{}, err
		}
	}
	return receipt, nil
}

func (a *RedshiftWarehouseAdapter) executeAndWait(ctx context.Context, connection model.DataPlatformConnection, sql, clientToken string) error {
	payload := map[string]any{
		"ClusterIdentifier": connection.Config["cluster"],
		"Database":          connection.Config["database"],
		"Sql":               sql,
		"ClientToken":       clientToken,
		"StatementName":     "simple-task-manager-" + clientToken[:16],
	}
	if user := strings.TrimSpace(connection.Config["db_user"]); user != "" {
		payload["DbUser"] = user
	}
	if secretARN := strings.TrimSpace(connection.Config["secret_arn"]); secretARN != "" {
		payload["SecretArn"] = secretARN
	}
	var response struct {
		ID string `json:"Id"`
	}
	if err := a.callWithRetry(ctx, "RedshiftData.ExecuteStatement", payload, &response); err != nil {
		return err
	}
	if strings.TrimSpace(response.ID) == "" {
		return fmt.Errorf("%w: Redshift Data API returned empty statement id", ErrInvalidDataExportRequest)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(a.pollInterval):
		}
		var status struct {
			Status string `json:"Status"`
			Error  string `json:"Error"`
		}
		if err := a.callWithRetry(ctx, "RedshiftData.DescribeStatement", map[string]any{"Id": response.ID}, &status); err != nil {
			return err
		}
		switch strings.ToUpper(strings.TrimSpace(status.Status)) {
		case "FINISHED":
			return nil
		case "FAILED", "ABORTED":
			message := strings.TrimSpace(status.Error)
			if message == "" {
				message = "statement did not finish successfully"
			}
			return fmt.Errorf("%w: Redshift statement %s: %s", ErrInvalidDataExportRequest, strings.ToLower(status.Status), message)
		case "SUBMITTED", "PICKED", "STARTED":
		default:
			return fmt.Errorf("%w: unknown Redshift statement status %q", ErrInvalidDataExportRequest, status.Status)
		}
	}
}

func (a *RedshiftWarehouseAdapter) callWithRetry(ctx context.Context, target string, payload any, output any) error {
	var lastErr error
	for attempt := 0; attempt < a.retryAttempts; attempt++ {
		status, raw, err := a.call(ctx, target, payload)
		if err != nil {
			lastErr = err
		} else if status >= 200 && status < 300 {
			if output != nil && len(raw) > 0 {
				if err := json.Unmarshal(raw, output); err != nil {
					return fmt.Errorf("%w: invalid Redshift Data API response", ErrInvalidDataExportRequest)
				}
			}
			return nil
		} else {
			lastErr = decodeRedshiftDataAPIError(status, raw)
			if !redshiftRetryableStatus(status) {
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
	if lastErr == nil {
		lastErr = fmt.Errorf("%w: Redshift Data API call failed", ErrInvalidDataExportRequest)
	}
	return lastErr
}

func (a *RedshiftWarehouseAdapter) call(ctx context.Context, target string, payload any) (int, []byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint.String(), bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("Accept", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", target)
	req.Header.Set("User-Agent", "simple-task-manager-redshift-data/1")
	credentials, err := a.credentials.Retrieve(ctx)
	if err != nil {
		return 0, nil, err
	}
	if err := signAWSRequestV4(req, raw, credentials, a.region, "redshift-data", time.Now().UTC()); err != nil {
		return 0, nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: Redshift Data API request failed: %v", ErrInvalidDataExportRequest, err)
	}
	defer resp.Body.Close()
	responseRaw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, responseRaw, nil
}

func redshiftCreateTableSQL(schema, table string) string {
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (organization_id BIGINT NOT NULL, workspace_id BIGINT NOT NULL, task_id BIGINT NOT NULL, title VARCHAR(65535) NOT NULL, status VARCHAR(100) NOT NULL, priority VARCHAR(100) NOT NULL, project_id BIGINT, due_at TIMESTAMPTZ, completed_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL, PRIMARY KEY (organization_id, workspace_id, task_id))", redshiftQualifiedTable(schema, table))
}

func redshiftMergeSQL(schema, table string, rows []model.AnalyticsTaskRecord) string {
	selects := make([]string, 0, len(rows))
	for _, row := range rows {
		selects = append(selects, "SELECT "+strings.Join([]string{
			strconv.FormatInt(row.OrganizationID, 10) + "::BIGINT AS organization_id",
			strconv.FormatInt(row.WorkspaceID, 10) + "::BIGINT AS workspace_id",
			strconv.FormatInt(row.TaskID, 10) + "::BIGINT AS task_id",
			redshiftString(row.Title) + "::VARCHAR AS title",
			redshiftString(row.Status) + "::VARCHAR AS status",
			redshiftString(row.Priority) + "::VARCHAR AS priority",
			redshiftInt(row.ProjectID) + "::BIGINT AS project_id",
			redshiftTimestamp(row.DueAt) + " AS due_at",
			redshiftTimestamp(row.CompletedAt) + " AS completed_at",
			redshiftTimestamp(&row.CreatedAt) + " AS created_at",
			redshiftTimestamp(&row.UpdatedAt) + " AS updated_at",
		}, ","))
	}
	return fmt.Sprintf("MERGE INTO %s AS t USING (%s) AS s ON t.organization_id=s.organization_id AND t.workspace_id=s.workspace_id AND t.task_id=s.task_id WHEN MATCHED THEN UPDATE SET title=s.title,status=s.status,priority=s.priority,project_id=s.project_id,due_at=s.due_at,completed_at=s.completed_at,created_at=s.created_at,updated_at=s.updated_at WHEN NOT MATCHED THEN INSERT (organization_id,workspace_id,task_id,title,status,priority,project_id,due_at,completed_at,created_at,updated_at) VALUES (s.organization_id,s.workspace_id,s.task_id,s.title,s.status,s.priority,s.project_id,s.due_at,s.completed_at,s.created_at,s.updated_at)", redshiftQualifiedTable(schema, table), strings.Join(selects, " UNION ALL "))
}

func redshiftQualifiedTable(schema, table string) string {
	return strings.ToLower(schema) + "." + strings.ToLower(table)
}

func redshiftString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func redshiftInt(value *int64) string {
	if value == nil {
		return "NULL"
	}
	return strconv.FormatInt(*value, 10)
}

func redshiftTimestamp(value *time.Time) string {
	if value == nil {
		return "NULL::TIMESTAMPTZ"
	}
	return "CAST(" + redshiftString(value.UTC().Format(time.RFC3339Nano)) + " AS TIMESTAMPTZ)"
}

func redshiftTableFromTarget(target string) string {
	target = strings.Trim(strings.TrimSpace(target), ".")
	if i := strings.LastIndex(target, "."); i >= 0 {
		return strings.TrimSpace(target[i+1:])
	}
	return target
}

func validRedshiftIdentifier(value string) bool {
	if value == "" || len(value) > 127 {
		return false
	}
	for i, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func validRedshiftClusterIdentifier(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for i, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9') || (i > 0 && r == '-') {
			continue
		}
		return false
	}
	return !strings.HasSuffix(value, "-") && !strings.Contains(value, "--")
}

func validRedshiftAWSRegion(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func parseRedshiftEndpoint(raw string, allowInsecure bool) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return nil, fmt.Errorf("%w: invalid Redshift Data API endpoint", ErrInvalidDataPlatformConnection)
	}
	if u.Scheme != "https" && !(allowInsecure && u.Scheme == "http") {
		return nil, fmt.Errorf("%w: Redshift Data API endpoint must use HTTPS", ErrInvalidDataPlatformConnection)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u, nil
}

func decodeRedshiftDataAPIError(status int, raw []byte) error {
	apiErr := &redshiftDataAPIError{Status: status, Code: "Unknown"}
	var body map[string]any
	if json.Unmarshal(raw, &body) == nil {
		apiErr.Code = strings.TrimSpace(fmt.Sprint(body["__type"]))
		if idx := strings.LastIndex(apiErr.Code, "#"); idx >= 0 {
			apiErr.Code = apiErr.Code[idx+1:]
		}
		if apiErr.Code == "" || apiErr.Code == "<nil>" {
			apiErr.Code = "Unknown"
		}
		apiErr.Message = firstNonEmpty(strings.TrimSpace(fmt.Sprint(body["message"])), strings.TrimSpace(fmt.Sprint(body["Message"])))
		if apiErr.Message == "<nil>" {
			apiErr.Message = ""
		}
	}
	return apiErr
}

func redshiftRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout || status >= 500
}

func redshiftClientToken(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func redshiftDurationEnv(name string, fallback time.Duration) (time.Duration, error) {
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
