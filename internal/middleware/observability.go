package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"go-simple-task-api/internal/observability"
	"go-simple-task-api/pkg/response"
)

const requestIDHeader = "X-Request-ID"

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := sanitizeRequestID(r.Header.Get(requestIDHeader))
		if requestID == "" {
			requestID = newRequestID()
		}

		w.Header().Set(requestIDHeader, requestID)
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func AccessLog(logger *slog.Logger, metrics *observability.Metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}

		metrics.IncInFlight()
		defer metrics.DecInFlight()

		next.ServeHTTP(recorder, r)

		duration := time.Since(start)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		metrics.ObserveHTTPRequest(r.Method, routeTemplate(r.URL.Path), status, duration)
		logger.InfoContext(
			r.Context(),
			"http_request",
			"request_id", RequestIDFromContext(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"bytes", recorder.bytes,
			"duration_ms", float64(duration.Microseconds())/1000,
			"remote_ip", clientIP(r),
			"trace_id", observability.TraceIDFromContext(r.Context()),
			"span_id", observability.SpanIDFromContext(r.Context()),
		)
	})
}

func Recover(logger *slog.Logger, metrics *observability.Metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				metrics.IncPanic()
				logger.ErrorContext(
					r.Context(),
					"http_panic",
					"request_id", RequestIDFromContext(r.Context()),
					"panic", recovered,
					"stack", string(debug.Stack()),
					"trace_id", observability.TraceIDFromContext(r.Context()),
					"span_id", observability.SpanIDFromContext(r.Context()),
				)
				response.JSON(w, http.StatusInternalServerError, response.Envelope{
					Success: false,
					Message: "internal server error",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func RequestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

func sanitizeRequestID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '-' || char == '_' || char == '.' {
			continue
		}
		return ""
	}
	return value
}

func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "request-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(raw[:])
}

func Trace(tracer *observability.Tracer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := routeTemplate(r.URL.Path)
		recorder := &statusRecorder{ResponseWriter: w}
		ctx, traceparent, finish := tracer.StartServerSpan(
			r.Context(),
			r.Method,
			route,
			r.Header.Get("traceparent"),
			RequestIDFromContext(r.Context()),
		)
		if traceparent != "" {
			recorder.Header().Set("traceparent", traceparent)
		}
		next.ServeHTTP(recorder, r.WithContext(ctx))
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		finish(status)
	})
}

func routeTemplate(path string) string {
	switch {
	case path == "/health", path == "/ready", path == "/metrics", path == "/version":
		return path
	case path == "/api/tasks":
		return "/api/tasks"
	case strings.HasPrefix(path, "/api/tasks/"):
		return "/api/tasks/{id}"
	case path == "/api/auth/sessions":
		return "/api/auth/sessions"
	case strings.HasPrefix(path, "/api/auth/sessions/"):
		return "/api/auth/sessions/{id}"
	case strings.HasPrefix(path, "/api/auth/"):
		return path
	case strings.HasPrefix(path, "/docs"), strings.HasPrefix(path, "/openapi"):
		return "/docs"
	default:
		return "/unmatched"
	}
}
