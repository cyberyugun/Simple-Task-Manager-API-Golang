package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go-simple-task-api/internal/auth"
)

func TestAuthMiddleware(t *testing.T) {
	manager := auth.NewTokenManager("12345678901234567890123456789012", time.Hour)
	token, err := manager.Generate(7, "user@example.com")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := UserIDFromContext(r.Context())
		if !ok || userID != 7 {
			t.Fatalf("context userID = %d, ok=%v, want 7,true", userID, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res := httptest.NewRecorder()
	Auth(manager)(next).ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusNoContent)
	}
}

func TestAuthMiddlewareRejectsMissingToken(t *testing.T) {
	manager := auth.NewTokenManager("12345678901234567890123456789012", time.Hour)
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	res := httptest.NewRecorder()
	Auth(manager)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler should not run")
	})).ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}
