package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-simple-task-api/internal/observability"
)

func TestRequestIDPreservesValidIncomingID(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestIDFromContext(r.Context()); got != "client-request-123" {
			t.Fatalf("request ID = %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(requestIDHeader, "client-request-123")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if got := res.Header().Get(requestIDHeader); got != "client-request-123" {
		t.Fatalf("response request ID = %q", got)
	}
}

func TestRequestIDReplacesInvalidIncomingID(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(requestIDHeader, "bad request id")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	got := res.Header().Get(requestIDHeader)
	if got == "" || got == "bad request id" {
		t.Fatalf("generated request ID = %q", got)
	}
}

func TestRecoverReturns500AndRecordsMetric(t *testing.T) {
	var logs bytes.Buffer
	logger := observability.NewJSONLoggerTo(&logs, "debug")
	metrics := observability.NewMetrics()

	panicHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	handler := RequestID(Recover(logger, metrics, panicHandler))

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", res.Code)
	}
	if !strings.Contains(logs.String(), "http_panic") {
		t.Fatalf("panic log missing: %s", logs.String())
	}

	metricsRes := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(metricsRes, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(metricsRes.Body.String(), "task_api_panics_total 1") {
		t.Fatalf("panic metric missing: %s", metricsRes.Body.String())
	}
}

func TestAccessLogRecordsRequest(t *testing.T) {
	var logs bytes.Buffer
	logger := observability.NewJSONLoggerTo(&logs, "info")
	metrics := observability.NewMetrics()

	handler := RequestID(AccessLog(logger, metrics, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	})))

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/test", nil))

	logBody := logs.String()
	for _, expected := range []string{"http_request", "request_id", "/test", "201"} {
		if !strings.Contains(logBody, expected) {
			t.Fatalf("access log missing %q: %s", expected, logBody)
		}
	}
}
