package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestWorkspaceScopeDefaultsToPersonalWorkspace(t *testing.T) {
	repo := repository.NewInMemoryWorkspaceRepository()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		access, ok := WorkspaceAccessFromContext(r.Context())
		if !ok || access.Role != model.WorkspaceRoleOwner || !access.IsPersonal {
			t.Fatalf("unexpected workspace access: %+v, ok=%v", access, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req = req.WithContext(WithUserID(req.Context(), 7))
	res := httptest.NewRecorder()
	WorkspaceScope(repo)(next).ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("status = %d", res.Code)
	}
	if res.Header().Get("X-Workspace-ID") == "" || res.Header().Get("X-Workspace-Role") != model.WorkspaceRoleOwner {
		t.Fatalf("missing workspace response headers: %+v", res.Header())
	}
}

func TestWorkspaceScopeRejectsCrossTenantSelection(t *testing.T) {
	repo := repository.NewInMemoryWorkspaceRepository()
	owner, err := repo.Create(1, "Shared", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	req.Header.Set("X-Workspace-ID", strconv.FormatInt(owner.ID, 10))
	req = req.WithContext(WithUserID(req.Context(), 2))
	res := httptest.NewRecorder()

	WorkspaceScope(repo)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("next handler should not run")
	})).ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusNotFound)
	}
}
