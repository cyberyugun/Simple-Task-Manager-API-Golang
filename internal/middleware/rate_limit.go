package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"go-simple-task-api/pkg/response"
)

type rateLimitEntry struct {
	Count   int
	ResetAt time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	clients map[string]rateLimitEntry
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:   limit,
		window:  window,
		clients: make(map[string]rateLimitEntry),
	}
}

func (l *RateLimiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(clientIP(r), time.Now()) {
			response.JSON(w, http.StatusTooManyRequests, response.Envelope{
				Success: false,
				Message: "too many requests",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (l *RateLimiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.clients[key]
	if !ok || !now.Before(entry.ResetAt) {
		l.clients[key] = rateLimitEntry{
			Count:   1,
			ResetAt: now.Add(l.window),
		}
		return true
	}
	if entry.Count >= l.limit {
		return false
	}

	entry.Count++
	l.clients[key] = entry
	return true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "unknown"
}
