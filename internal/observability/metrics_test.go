package observability

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsHandler(t *testing.T) {
	metrics := NewMetrics()
	metrics.ObserveRequest(250 * time.Millisecond)
	metrics.IncRateLimited()
	metrics.IncPanic()
	metrics.ObserveReadiness(false)
	metrics.ObserveReadiness(true)

	recorder := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))

	body := recorder.Body.String()
	for _, expected := range []string{
		"task_api_http_requests_total 1",
		"task_api_http_request_duration_seconds_count 1",
		"task_api_panics_total 1",
		"task_api_rate_limited_total 1",
		"task_api_readiness_checks_total 2",
		"task_api_readiness_failures_total 1",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics body missing %q:\n%s", expected, body)
		}
	}
}
