package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestIdempotencyReplaysCompletedResponse(t *testing.T) {
	repo := repository.NewInMemoryEventRepository()
	var calls atomic.Int32
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":42}}`))
	})
	handler := Idempotency(repo, time.Hour)(next)

	request := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
		req.Header.Set("X-Idempotency-Key", "task-create-42")
		req = req.WithContext(WithWorkspaceAccess(req.Context(), model.WorkspaceAccess{
			Workspace: model.Workspace{ID: 7},
			Role:      model.WorkspaceRoleOwner,
		}))
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}

	first := request(`{"title":"A"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d body=%s", first.Code, first.Body.String())
	}
	second := request(`{"title":"A"}`)
	if second.Code != http.StatusCreated || second.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay status=%d headers=%+v body=%s", second.Code, second.Header(), second.Body.String())
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("replayed body differs: first=%s second=%s", first.Body.String(), second.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d, want 1", calls.Load())
	}

	conflict := request(`{"title":"B"}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d body=%s", conflict.Code, conflict.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("handler called for conflicting key: %d", calls.Load())
	}
}

func TestIdempotencyAbortsServerErrors(t *testing.T) {
	repo := repository.NewInMemoryEventRepository()
	var calls atomic.Int32
	handler := Idempotency(repo, time.Hour)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))

	run := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{"title":"retry"}`))
		req.Header.Set("X-Idempotency-Key", "retry-key")
		req = req.WithContext(WithWorkspaceAccess(req.Context(), model.WorkspaceAccess{
			Workspace: model.Workspace{ID: 8},
			Role:      model.WorkspaceRoleOwner,
		}))
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res.Code
	}

	if got := run(); got != http.StatusServiceUnavailable {
		t.Fatalf("first status = %d", got)
	}
	if got := run(); got != http.StatusCreated {
		t.Fatalf("retry status = %d", got)
	}
	if calls.Load() != 2 {
		t.Fatalf("handler calls = %d, want 2", calls.Load())
	}
}
