package observability

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type traceContextKey string

const (
	traceIDContextKey traceContextKey = "trace-id"
	spanIDContextKey  traceContextKey = "span-id"
)

type Tracer struct {
	enabled     bool
	endpoint    string
	serviceName string
	environment string
	client      *http.Client
	logger      *slog.Logger
	metrics     *Metrics
	spans       chan otlpSpan
	done        chan struct{}
	closeOnce   sync.Once
}

type otlpSpan struct {
	TraceID      string
	SpanID       string
	ParentSpanID string
	Name         string
	Start        time.Time
	End          time.Time
	Method       string
	Route        string
	Status       int
	RequestID    string
}

func NewTracer(endpoint, serviceName, environment string, logger *slog.Logger, metrics *Metrics) (*Tracer, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return &Tracer{logger: logger, metrics: metrics}, nil
	}

	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT must be a valid http or https URL")
	}

	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		serviceName = "task-api"
	}
	environment = strings.TrimSpace(environment)
	if environment == "" {
		environment = "unknown"
	}

	tracer := &Tracer{
		enabled:     true,
		endpoint:    strings.TrimRight(endpoint, "/") + "/v1/traces",
		serviceName: serviceName,
		environment: environment,
		client:      &http.Client{Timeout: 5 * time.Second},
		logger:      logger,
		metrics:     metrics,
		spans:       make(chan otlpSpan, 512),
		done:        make(chan struct{}),
	}
	go tracer.run()
	return tracer, nil
}

func (t *Tracer) Enabled() bool {
	return t != nil && t.enabled
}

func (t *Tracer) StartServerSpan(ctx context.Context, method, route, traceparent, requestID string) (context.Context, string, func(int)) {
	if t == nil || !t.enabled {
		return ctx, "", func(int) {}
	}

	traceID, parentSpanID, flags, ok := parseTraceparent(traceparent)
	if !ok {
		traceID = randomHex(16)
		parentSpanID = ""
		flags = "01"
	}
	spanID := randomHex(8)
	start := time.Now().UTC()

	ctx = context.WithValue(ctx, traceIDContextKey, traceID)
	ctx = context.WithValue(ctx, spanIDContextKey, spanID)
	outboundTraceparent := "00-" + traceID + "-" + spanID + "-" + flags

	var once sync.Once
	finish := func(status int) {
		once.Do(func() {
			span := otlpSpan{
				TraceID:      traceID,
				SpanID:       spanID,
				ParentSpanID: parentSpanID,
				Name:         "HTTP " + method + " " + route,
				Start:        start,
				End:          time.Now().UTC(),
				Method:       method,
				Route:        route,
				Status:       status,
				RequestID:    requestID,
			}
			select {
			case t.spans <- span:
			default:
				if t.metrics != nil {
					t.metrics.IncTraceExportError()
				}
				if t.logger != nil {
					t.logger.Warn("trace_span_dropped", "reason", "buffer_full")
				}
			}
		})
	}
	return ctx, outboundTraceparent, finish
}

func TraceIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(traceIDContextKey).(string)
	return value
}

func SpanIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(spanIDContextKey).(string)
	return value
}

func (t *Tracer) Shutdown(ctx context.Context) error {
	if t == nil || !t.enabled {
		return nil
	}
	t.closeOnce.Do(func() {
		close(t.spans)
	})
	select {
	case <-t.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *Tracer) run() {
	defer close(t.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	batch := make([]otlpSpan, 0, 100)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := t.export(batch); err != nil {
			if t.metrics != nil {
				t.metrics.IncTraceExportError()
			}
			if t.logger != nil {
				t.logger.Warn("trace_export_failed", "error", err, "span_count", len(batch))
			}
		}
		batch = batch[:0]
	}

	for {
		select {
		case span, ok := <-t.spans:
			if !ok {
				flush()
				return
			}
			batch = append(batch, span)
			if len(batch) >= 100 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (t *Tracer) export(spans []otlpSpan) error {
	payloadSpans := make([]any, 0, len(spans))
	for _, span := range spans {
		statusCode := 1
		if span.Status >= http.StatusInternalServerError {
			statusCode = 2
		}

		attributes := []any{
			otlpStringAttribute("http.request.method", span.Method),
			otlpStringAttribute("http.route", span.Route),
			otlpIntAttribute("http.response.status_code", span.Status),
		}
		if span.RequestID != "" {
			attributes = append(attributes, otlpStringAttribute("http.request_id", span.RequestID))
		}

		payloadSpan := map[string]any{
			"traceId":           span.TraceID,
			"spanId":            span.SpanID,
			"name":              span.Name,
			"kind":              2,
			"startTimeUnixNano": fmt.Sprintf("%d", span.Start.UnixNano()),
			"endTimeUnixNano":   fmt.Sprintf("%d", span.End.UnixNano()),
			"attributes":        attributes,
			"status":            map[string]any{"code": statusCode},
		}
		if span.ParentSpanID != "" {
			payloadSpan["parentSpanId"] = span.ParentSpanID
		}
		payloadSpans = append(payloadSpans, payloadSpan)
	}

	payload := map[string]any{
		"resourceSpans": []any{
			map[string]any{
				"resource": map[string]any{
					"attributes": []any{
						otlpStringAttribute("service.name", t.serviceName),
						otlpStringAttribute("deployment.environment.name", t.environment),
					},
				},
				"scopeSpans": []any{
					map[string]any{
						"scope": map[string]any{
							"name":    "go-simple-task-api/http",
							"version": "1",
						},
						"spans": payloadSpans,
					},
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal OTLP traces: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build OTLP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("send OTLP traces: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("OTLP collector returned %s", resp.Status)
	}
	return nil
}

func otlpStringAttribute(key, value string) map[string]any {
	return map[string]any{
		"key": key,
		"value": map[string]any{
			"stringValue": value,
		},
	}
}

func otlpIntAttribute(key string, value int) map[string]any {
	return map[string]any{
		"key": key,
		"value": map[string]any{
			"intValue": fmt.Sprintf("%d", value),
		},
	}
}

func parseTraceparent(value string) (traceID, parentSpanID, flags string, ok bool) {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) != 4 || len(parts[0]) != 2 || len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return "", "", "", false
	}
	if strings.EqualFold(parts[0], "ff") {
		return "", "", "", false
	}
	for _, part := range parts {
		if _, err := hex.DecodeString(part); err != nil {
			return "", "", "", false
		}
	}
	if allZero(parts[1]) || allZero(parts[2]) {
		return "", "", "", false
	}
	return strings.ToLower(parts[1]), strings.ToLower(parts[2]), strings.ToLower(parts[3]), true
}

func randomHex(bytesCount int) string {
	raw := make([]byte, bytesCount)
	if _, err := rand.Read(raw); err != nil {
		fallback := fmt.Sprintf("%032x", time.Now().UTC().UnixNano())
		if bytesCount == 8 {
			return fallback[len(fallback)-16:]
		}
		return fallback[len(fallback)-32:]
	}
	return hex.EncodeToString(raw)
}

func allZero(value string) bool {
	return strings.Trim(value, "0") == ""
}
