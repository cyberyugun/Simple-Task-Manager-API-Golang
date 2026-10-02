package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllowResetsWindow(t *testing.T) {
	limiter := NewRateLimiter(2, time.Minute)
	now := time.Now()

	if !limiter.Allow("client", now) || !limiter.Allow("client", now.Add(time.Second)) {
		t.Fatal("first two requests should be allowed")
	}
	if limiter.Allow("client", now.Add(2*time.Second)) {
		t.Fatal("third request should be rejected")
	}
	if !limiter.Allow("client", now.Add(time.Minute)) {
		t.Fatal("request after window should be allowed")
	}
}

func TestRateLimiterHandlerReturns429(t *testing.T) {
	limiter := NewRateLimiter(1, time.Minute)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := limiter.Handler(next)

	first := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	handler.ServeHTTP(first, req)
	if first.Code != http.StatusNoContent {
		t.Fatalf("first status = %d", first.Code)
	}

	second := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req2.RemoteAddr = "203.0.113.1:5678"
	handler.ServeHTTP(second, req2)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
}
