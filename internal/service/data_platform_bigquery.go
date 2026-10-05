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

type BigQueryWarehouseConfig struct {
	Endpoint         string
	AccessToken      string
	UseMetadata      bool
	MetadataEndpoint string
	Timeout          time.Duration
	RetryAttempts    int
	RetryBackoff     time.Duration
	AllowInsecure    bool
}

type BigQueryWarehouseAdapter struct {
	endpoint      *url.URL
	client        *http.Client
	tokens        gcpAccessTokenProvider
	retryAttempts int
	retryBackoff  time.Duration
}

type bigQueryAPIError struct {
	Status  int
	Message string
}

func (e *bigQueryAPIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("BigQuery HTTP %d", e.Status)
	}
	return fmt.Sprintf("BigQuery HTTP %d: %s", e.Status, e.Message)
}

func NewBigQueryWarehouseAdapter(cfg BigQueryWarehouseConfig) (*BigQueryWarehouseAdapter, error) {
	endpointRaw := strings.TrimSpace(cfg.Endpoint)
	if endpointRaw == "" {
		endpointRaw = "https://bigquery.googleapis.com/bigquery/v2"
	}
	endpoint, err := parseBigQueryEndpoint(endpointRaw, cfg.AllowInsecure)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	attempts := cfg.RetryAttempts
	if attempts <= 0 {
		attempts = 3
	}
	if attempts > 6 {
		return nil, fmt.Errorf("%w: BigQuery retry attempts too high", ErrInvalidDataPlatformConnection)
	}
	backoff := cfg.RetryBackoff
	if backoff <= 0 {
		backoff = 250 * time.Millisecond
	}
	tokens, err := newGCPAccessTokenProvider(GCPSecretManagerConfig{
		AccessToken: cfg.AccessToken, UseMetadata: cfg.UseMetadata,
		MetadataEndpoint: cfg.MetadataEndpoint, Timeout: timeout, AllowInsecure: cfg.AllowInsecure,
	}, timeout)
	if err != nil {
		return nil, err
	}
	return &BigQueryWarehouseAdapter{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		tokens: tokens, retryAttempts: attempts, retryBackoff: backoff,
	}, nil
}

func NewBigQueryWarehouseAdapterFromEnv(allowInsecure bool) (*BigQueryWarehouseAdapter, error) {
	rawEnabled := strings.TrimSpace(os.Getenv("DATA_PLATFORM_BIGQUERY_NATIVE"))
	if rawEnabled == "" {
		return nil, nil
	}
	enabled, err := strconv.ParseBool(rawEnabled)
	if err != nil {
		return nil, fmt.Errorf("%w: DATA_PLATFORM_BIGQUERY_NATIVE must be true or false", ErrInvalidDataPlatformConnection)
	}
	if !enabled {
		return nil, nil
	}
	timeout := 20 * time.Second
	if raw := strings.TrimSpace(os.Getenv("DATA_PLATFORM_BIGQUERY_TIMEOUT")); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil || parsed <= 0 {
			return nil, fmt.Errorf("%w: DATA_PLATFORM_BIGQUERY_TIMEOUT must be a positive duration", ErrInvalidDataPlatformConnection)
		}
		timeout = parsed
	}
	attempts := 3
	if raw := strings.TrimSpace(os.Getenv("DATA_PLATFORM_BIGQUERY_RETRY_ATTEMPTS")); raw != "" {
		value, parseErr := strconv.Atoi(raw)
		if parseErr != nil || value < 1 || value > 6 {
			return nil, fmt.Errorf("%w: DATA_PLATFORM_BIGQUERY_RETRY_ATTEMPTS must be 1..6", ErrInvalidDataPlatformConnection)
		}
		attempts = value
	}
	backoff := 250 * time.Millisecond
	if raw := strings.TrimSpace(os.Getenv("DATA_PLATFORM_BIGQUERY_RETRY_BACKOFF")); raw != "" {
		parsed, parseErr := time.ParseDuration(raw)
		if parseErr != nil || parsed <= 0 {
			return nil, fmt.Errorf("%w: DATA_PLATFORM_BIGQUERY_RETRY_BACKOFF must be a positive duration", ErrInvalidDataPlatformConnection)
		}
		backoff = parsed
	}
	useMetadata := true
	if raw := strings.TrimSpace(os.Getenv("DATA_PLATFORM_BIGQUERY_USE_METADATA")); raw != "" {
		value, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: DATA_PLATFORM_BIGQUERY_USE_METADATA must be true or false", ErrInvalidDataPlatformConnection)
		}
		useMetadata = value
	}
	return NewBigQueryWarehouseAdapter(BigQueryWarehouseConfig{
		Endpoint: os.Getenv("DATA_PLATFORM_BIGQUERY_ENDPOINT"),
		AccessToken: os.Getenv("DATA_PLATFORM_BIGQUERY_ACCESS_TOKEN"),
		UseMetadata: useMetadata,
		MetadataEndpoint: os.Getenv("DATA_PLATFORM_BIGQUERY_METADATA_ENDPOINT"),
		Timeout: timeout, RetryAttempts: attempts, RetryBackoff: backoff,
		AllowInsecure: allowInsecure,
	})
}

func (a *BigQueryWarehouseAdapter) Capability() model.WarehouseAdapterCapability {
	return model.WarehouseAdapterCapability{
		Key: model.DataWarehouseBigQuery,
		DisplayName: "Google BigQuery (native)",
		DeliveryMode: "streaming_insert_live",
		BIContracts: []string{model.BIContractPowerBI, model.BIContractTableau, model.BIContractLooker},
		SupportsRecovery: true,
	}
}

func (a *BigQueryWarehouseAdapter) Deliver(connection model.DataPlatformConnection, rows []model.AnalyticsTaskRecord) (model.WarehouseDeliveryReceipt, error) {
	project := strings.TrimSpace(connection.Config["project_id"])
	dataset := strings.TrimSpace(connection.Config["dataset"])
	table := strings.TrimSpace(connection.Config["table"])
	if table == "" {
		table = bigQueryTableFromTarget(connection.Target)
	}
	if !validBigQueryProject(project) || !validBigQueryIdentifier(dataset) || !validBigQueryIdentifier(table) {
		return model.WarehouseDeliveryReceipt{}, ErrInvalidDataPlatformConnection
	}

	payload, err := json.Marshal(rows)
	if err != nil {
		return model.WarehouseDeliveryReceipt{}, err
	}
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	batchID := hash[:16]
	receipt := model.WarehouseDeliveryReceipt{
		Provider: model.DataWarehouseBigQuery,
		Target: connection.Target,
		DeliveryURI: fmt.Sprintf("bigquery://%s/%s/%s/batches/%s", project, dataset, table, batchID),
		BatchID: batchID, RowCount: len(rows), Bytes: int64(len(payload)), PayloadHash: hash,
	}
	if len(rows) == 0 {
		return receipt, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), a.client.Timeout)
	defer cancel()
	if err := a.ensureTable(ctx, project, dataset, table); err != nil {
		return model.WarehouseDeliveryReceipt{}, err
	}
	const batchSize = 500
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := a.insertRows(ctx, project, dataset, table, rows[start:end]); err != nil {
			return model.WarehouseDeliveryReceipt{}, err
		}
	}
	return receipt, nil
}

func (a *BigQueryWarehouseAdapter) ensureTable(ctx context.Context, project, dataset, table string) error {
	endpoint := a.tableURL(project, dataset, table)
	resp, raw, err := a.request(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		return a.createTable(ctx, project, dataset, table)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeBigQueryError(resp.StatusCode, raw)
	}
	var current struct {
		Schema struct {
			Fields []struct {
				Name string `json:"name"`
				Type string `json:"type"`
				Mode string `json:"mode"`
			} `json:"fields"`
		} `json:"schema"`
	}
	if err := json.Unmarshal(raw, &current); err != nil {
		return fmt.Errorf("%w: invalid BigQuery table response", ErrInvalidDataPlatformConnection)
	}
	expected := bigQuerySchemaFields()
	if !bigQuerySchemaCompatible(current.Schema.Fields, expected) {
		return fmt.Errorf("%w: BigQuery table schema is incompatible", ErrIncompatibleDatasetSchema)
	}
	return nil
}

func (a *BigQueryWarehouseAdapter) createTable(ctx context.Context, project, dataset, table string) error {
	body := map[string]any{
		"tableReference": map[string]string{
			"projectId": project, "datasetId": dataset, "tableId": table,
		},
		"schema": map[string]any{"fields": bigQuerySchemaFields()},
	}
	endpoint := a.datasetTablesURL(project, dataset)
	resp, raw, err := a.request(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusConflict {
		return a.ensureTable(ctx, project, dataset, table)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeBigQueryError(resp.StatusCode, raw)
	}
	return nil
}

func (a *BigQueryWarehouseAdapter) insertRows(ctx context.Context, project, dataset, table string, rows []model.AnalyticsTaskRecord) error {
	payloadRows := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		raw, err := analyticsTaskRecordMap(row)
		if err != nil {
			return err
		}
		insertSeed := fmt.Sprintf("%d:%d:%d:%s", row.OrganizationID, row.WorkspaceID, row.TaskID, row.UpdatedAt.UTC().Format(time.RFC3339Nano))
		insertHash := sha256.Sum256([]byte(insertSeed))
		payloadRows = append(payloadRows, map[string]any{
			"insertId": hex.EncodeToString(insertHash[:]),
			"json": raw,
		})
	}
	body := map[string]any{
		"kind": "bigquery#tableDataInsertAllRequest",
		"skipInvalidRows": false,
		"ignoreUnknownValues": false,
		"rows": payloadRows,
	}
	endpoint := a.tableURL(project, dataset, table) + "/insertAll"
	var lastErr error
	for attempt := 0; attempt < a.retryAttempts; attempt++ {
		resp, raw, err := a.request(ctx, http.MethodPost, endpoint, body)
		if err != nil {
			lastErr = err
		} else if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var result struct {
				InsertErrors []struct {
					Index  int `json:"index"`
					Errors []struct {
						Reason  string `json:"reason"`
						Message string `json:"message"`
					} `json:"errors"`
				} `json:"insertErrors"`
			}
			if err := json.Unmarshal(raw, &result); err != nil {
				return fmt.Errorf("%w: invalid BigQuery insert response", ErrInvalidDataExportRequest)
			}
			if len(result.InsertErrors) == 0 {
				return nil
			}
			return fmt.Errorf("%w: BigQuery rejected %d row groups: %s", ErrInvalidDataExportRequest, len(result.InsertErrors), summarizeBigQueryInsertErrors(result.InsertErrors))
		} else {
			lastErr = decodeBigQueryError(resp.StatusCode, raw)
			if !bigQueryRetryableStatus(resp.StatusCode) {
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
		lastErr = fmt.Errorf("%w: BigQuery delivery failed", ErrInvalidDataExportRequest)
	}
	return lastErr
}

func (a *BigQueryWarehouseAdapter) request(ctx context.Context, method, endpoint string, body any) (*http.Response, []byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, nil, err
	}
	token, err := a.tokens.Token(ctx)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "simple-task-manager-bigquery/1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: BigQuery request failed: %v", ErrInvalidDataExportRequest, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return resp, nil, err
	}
	return resp, raw, nil
}

func (a *BigQueryWarehouseAdapter) datasetTablesURL(project, dataset string) string {
	u := *a.endpoint
	u.Path = strings.TrimRight(u.Path, "/") + "/projects/" + url.PathEscape(project) +
		"/datasets/" + url.PathEscape(dataset) + "/tables"
	u.RawQuery = ""
	return u.String()
}

func (a *BigQueryWarehouseAdapter) tableURL(project, dataset, table string) string {
	return a.datasetTablesURL(project, dataset) + "/" + url.PathEscape(table)
}

func bigQuerySchemaFields() []map[string]string {
	fields := canonicalDataPlatformFields()
	result := make([]map[string]string, 0, len(fields))
	for _, field := range fields {
		mode := "REQUIRED"
		if field.Nullable {
			mode = "NULLABLE"
		}
		result = append(result, map[string]string{
			"name": field.Name, "type": bigQueryType(field.Type), "mode": mode,
		})
	}
	return result
}

func bigQuerySchemaCompatible(current []struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Mode string `json:"mode"`
}, expected []map[string]string) bool {
	byName := map[string]struct {
		Type string
		Mode string
	}{}
	for _, field := range current {
		byName[strings.ToLower(strings.TrimSpace(field.Name))] = struct {
			Type string
			Mode string
		}{Type: strings.ToUpper(strings.TrimSpace(field.Type)), Mode: strings.ToUpper(strings.TrimSpace(field.Mode))}
	}
	for _, wanted := range expected {
		field, ok := byName[wanted["name"]]
		if !ok || field.Type != wanted["type"] {
			return false
		}
		if wanted["mode"] == "REQUIRED" && field.Mode != "REQUIRED" {
			return false
		}
	}
	return true
}

func analyticsTaskRecordMap(row model.AnalyticsTaskRecord) (map[string]any, error) {
	raw, err := json.Marshal(row)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func bigQueryType(value string) string {
	switch value {
	case "int64":
		return "INTEGER"
	case "float64":
		return "FLOAT"
	case "bool":
		return "BOOLEAN"
	case "timestamp":
		return "TIMESTAMP"
	case "date":
		return "DATE"
	case "json":
		return "JSON"
	default:
		return "STRING"
	}
}

func bigQueryTableFromTarget(target string) string {
	target = strings.Trim(strings.TrimSpace(target), ".")
	if index := strings.LastIndex(target, "."); index >= 0 {
		return strings.TrimSpace(target[index+1:])
	}
	return target
}

func parseBigQueryEndpoint(raw string, allowInsecure bool) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" || endpoint.RawQuery != "" {
		return nil, fmt.Errorf("%w: invalid BigQuery endpoint", ErrInvalidDataPlatformConnection)
	}
	if endpoint.Scheme != "https" && !(allowInsecure && endpoint.Scheme == "http") {
		return nil, fmt.Errorf("%w: BigQuery endpoint must use HTTPS", ErrInvalidDataPlatformConnection)
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/")
	return endpoint, nil
}

func validBigQueryProject(value string) bool {
	if len(value) < 3 || len(value) > 63 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == ':' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func validBigQueryIdentifier(value string) bool {
	if value == "" || len(value) > 1024 {
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

func decodeBigQueryError(status int, raw []byte) error {
	var decoded struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &decoded)
	return &bigQueryAPIError{Status: status, Message: strings.TrimSpace(decoded.Error.Message)}
}

func bigQueryRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout ||
		status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout ||
		status >= 500
}

func summarizeBigQueryInsertErrors(items []struct {
	Index  int `json:"index"`
	Errors []struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	} `json:"errors"`
}) string {
	parts := make([]string, 0, 3)
	for _, item := range items {
		for _, entry := range item.Errors {
			message := strings.TrimSpace(entry.Reason)
			if strings.TrimSpace(entry.Message) != "" {
				message += ":" + strings.TrimSpace(entry.Message)
			}
			parts = append(parts, message)
			if len(parts) == 3 {
				return strings.Join(parts, ", ")
			}
		}
	}
	return strings.Join(parts, ", ")
}
