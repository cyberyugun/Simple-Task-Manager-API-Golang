package middleware

import (
	"net/http"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/pkg/response"
)

type ExtensionAuthorizer interface {
	AuthorizeExtensionRequest(clientID string, workspaceID int64, now time.Time) model.ExtensionRequestDecision
	RecordExtensionResponse(installationID, workspaceID int64, statusCode int, latency time.Duration, now time.Time) error
}

func ExtensionUsage(platform ExtensionAuthorizer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if platform == nil {
				next.ServeHTTP(w, r)
				return
			}
			claims, ok := AuthClaimsFromContext(r.Context())
			if !ok || claims.ClientID == "" || claims.WorkspaceID <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			started := time.Now()
			decision := platform.AuthorizeExtensionRequest(claims.ClientID, claims.WorkspaceID, started.UTC())
			if !decision.IsExtension {
				next.ServeHTTP(w, r)
				return
			}
			if !decision.Allowed {
				status := decision.StatusCode
				if status == 0 {
					status = http.StatusForbidden
				}
				if status == http.StatusTooManyRequests {
					w.Header().Set("Retry-After", "60")
				}
				response.JSON(w, status, response.Envelope{Success: false, Message: decision.Message})
				return
			}
			recorder := &extensionStatusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(recorder, r)
			_ = platform.RecordExtensionResponse(
				decision.InstallationID,
				decision.WorkspaceID,
				recorder.status,
				time.Since(started),
				time.Now().UTC(),
			)
		})
	}
}

type extensionStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *extensionStatusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *extensionStatusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}
