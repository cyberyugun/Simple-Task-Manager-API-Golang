package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIVersion(t *testing.T) {
	handler := APIVersion("v1", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := res.Header().Get("X-API-Version"); got != "v1" {
		t.Fatalf("X-API-Version = %q, want v1", got)
	}
	if got := res.Header().Get("API-Supported-Versions"); got != "v1" {
		t.Fatalf("API-Supported-Versions = %q, want v1", got)
	}
}
