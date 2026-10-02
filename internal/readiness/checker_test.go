package readiness

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-simple-task-api/internal/observability"
)

func TestCheckerWithoutDependenciesIsReady(t *testing.T) {
	checker := New(nil, nil, time.Second, observability.NewMetrics())

	res := httptest.NewRecorder()
	checker.Handler(res, httptest.NewRequest(http.MethodGet, "/ready", nil))

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", res.Code, res.Body.String())
	}
}

func TestCheckerRejectsUnsupportedMethod(t *testing.T) {
	checker := New(nil, nil, time.Second, observability.NewMetrics())

	res := httptest.NewRecorder()
	checker.Handler(res, httptest.NewRequest(http.MethodPost, "/ready", nil))

	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusMethodNotAllowed)
	}
}
