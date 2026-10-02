package apidocs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPI(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	res := httptest.NewRecorder()
	OpenAPI(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	if !strings.Contains(res.Header().Get("Content-Type"), "application/yaml") {
		t.Fatalf("content type = %q", res.Header().Get("Content-Type"))
	}
	if !strings.Contains(res.Body.String(), "openapi: 3.1.0") {
		t.Fatal("OpenAPI spec was not served")
	}
}

func TestSwaggerUI(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	res := httptest.NewRecorder()
	SwaggerUI(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	body := res.Body.String()
	if !strings.Contains(body, "SwaggerUIBundle") || !strings.Contains(body, "/openapi.yaml") {
		t.Fatal("Swagger UI page is incomplete")
	}
}

func TestDocsRejectUnsupportedMethod(t *testing.T) {
	res := httptest.NewRecorder()
	SwaggerUI(res, httptest.NewRequest(http.MethodPost, "/docs", nil))
	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusMethodNotAllowed)
	}
}
