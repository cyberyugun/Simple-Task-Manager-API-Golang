package observability

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsHandler(t *testing.T) {
	metrics := NewMetrics()
	metrics.ObserveHTTPRequest("GET", "/api/tasks", 200, 250*time.Millisecond)
	metrics.IncRateLimited()
	metrics.IncPanic()
	metrics.IncTraceExportError()
	metrics.ObserveReadiness(false)
	metrics.ObserveReadiness(true)
	metrics.SetDependencyReady("postgresql", true)
	metrics.SetDependencyReady("redis", false)

	recorder := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))

	body := recorder.Body.String()
	for _, expected := range []string{
		"task_api_http_requests_total{method=\"GET\",route=\"/api/tasks\",status_class=\"2xx\"} 1",
		"task_api_http_request_duration_seconds_count{method=\"GET\",route=\"/api/tasks\",status_class=\"2xx\"} 1",
		"task_api_http_request_duration_seconds_bucket{method=\"GET\",route=\"/api/tasks\",status_class=\"2xx\",le=\"+Inf\"} 1",
		"task_api_panics_total 1",
		"task_api_rate_limited_total 1",
		"task_api_readiness_checks_total 2",
		"task_api_readiness_failures_total 1",
		"task_api_trace_export_errors_total 1",
		"task_api_dependency_ready{dependency=\"postgresql\"} 1",
		"task_api_dependency_ready{dependency=\"redis\"} 0",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics body missing %q:\n%s", expected, body)
		}
	}
}

func TestStatusClass(t *testing.T) {
	tests := map[int]string{
		0: "unknown", 99: "unknown", 200: "2xx", 404: "4xx", 503: "5xx", 600: "unknown",
	}
	for status, expected := range tests {
		if actual := statusClass(status); actual != expected {
			t.Fatalf("statusClass(%d) = %q, want %q", status, actual, expected)
		}
	}
}
