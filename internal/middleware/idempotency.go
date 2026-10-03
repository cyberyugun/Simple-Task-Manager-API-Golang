package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"go-simple-task-api/internal/repository"
	"go-simple-task-api/pkg/response"
)

const maxIdempotentBodyBytes = 1 << 20

func Idempotency(repo repository.EventRepository, ttl time.Duration) func(http.Handler) http.Handler {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := strings.TrimSpace(r.Header.Get("X-Idempotency-Key"))
			if r.Method != http.MethodPost || key == "" {
				next.ServeHTTP(w, r)
				return
			}
			if len(key) > 200 {
				response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "X-Idempotency-Key must be at most 200 characters"})
				return
			}
			access, ok := WorkspaceAccessFromContext(r.Context())
			if !ok {
				response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "workspace scope is missing"})
				return
			}

			body, err := io.ReadAll(io.LimitReader(r.Body, maxIdempotentBodyBytes+1))
			if err != nil {
				response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "failed to read request body"})
				return
			}
			if len(body) > maxIdempotentBodyBytes {
				response.JSON(w, http.StatusRequestEntityTooLarge, response.Envelope{Success: false, Message: "idempotent request body exceeds 1 MiB"})
				return
			}
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))

			sum := sha256.Sum256(body)
			requestHash := hex.EncodeToString(sum[:])
			now := time.Now().UTC()
			record, acquired, err := repo.BeginIdempotency(
				access.ID, key, r.Method, r.URL.Path, requestHash, now, now.Add(ttl),
			)
			if err != nil {
				switch {
				case errors.Is(err, repository.ErrIdempotencyConflict):
					response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
				case errors.Is(err, repository.ErrIdempotencyInProgress):
					w.Header().Set("Retry-After", "1")
					response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
				default:
					response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "idempotency check failed"})
				}
				return
			}
			if !acquired {
				if record.ResponseContentType != "" {
					w.Header().Set("Content-Type", record.ResponseContentType)
				}
				w.Header().Set("Idempotent-Replayed", "true")
				w.WriteHeader(record.ResponseStatus)
				_, _ = w.Write(record.ResponseBody)
				return
			}

			capture := newBufferedResponse()
			next.ServeHTTP(capture, r)
			status := capture.statusCode()
			if status >= http.StatusInternalServerError {
				_ = repo.AbortIdempotency(access.ID, key)
				copyBufferedResponse(w, capture)
				return
			}
			contentType := capture.Header().Get("Content-Type")
			if contentType == "" {
				contentType = "application/json"
			}
			if err := repo.CompleteIdempotency(access.ID, key, status, contentType, capture.body.Bytes(), time.Now().UTC()); err != nil {
				response.JSON(w, http.StatusInternalServerError, response.Envelope{
					Success: false,
					Message: "request completed but idempotency result could not be persisted",
				})
				return
			}
			copyBufferedResponse(w, capture)
		})
	}
}

type bufferedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{header: make(http.Header)}
}

func (w *bufferedResponse) Header() http.Header { return w.header }

func (w *bufferedResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *bufferedResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func (w *bufferedResponse) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func copyBufferedResponse(dst http.ResponseWriter, src *bufferedResponse) {
	for key, values := range src.Header() {
		for _, value := range values {
			dst.Header().Add(key, value)
		}
	}
	dst.WriteHeader(src.statusCode())
	_, _ = dst.Write(src.body.Bytes())
}
