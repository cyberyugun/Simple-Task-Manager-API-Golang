package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-simple-task-api/internal/middleware"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
)

type testEnvelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func newTestHandler() *TaskHandler {
	repo := repository.NewInMemoryTaskRepository()
	taskService := service.NewTaskService(repo)
	return NewTaskHandler(taskService)
}

func scoped(req *http.Request, userID, workspaceID int64, role string) *http.Request {
	ctx := middleware.WithUserID(req.Context(), userID)
	ctx = middleware.WithWorkspaceAccess(ctx, model.WorkspaceAccess{
		Workspace: model.Workspace{ID: workspaceID},
		Role:      role,
	})
	return req.WithContext(ctx)
}

func createTask(t *testing.T, h *TaskHandler, userID, workspaceID int64, body string) {
	t.Helper()
	req := scoped(httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body)), userID, workspaceID, model.WorkspaceRoleMember)
	res := httptest.NewRecorder()
	h.Tasks(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body=%s", res.Code, res.Body.String())
	}
}

func TestTaskHandlerPaginationSearchFilterAndSort(t *testing.T) {
	h := newTestHandler()
	createTask(t, h, 1, 10, `{"title":"Go API","description":"backend"}`)
	createTask(t, h, 2, 10, `{"title":"Angular","description":"frontend"}`)
	createTask(t, h, 1, 10, `{"title":"Go Testing","description":"tests"}`)
	createTask(t, h, 1, 20, `{"title":"Go Hidden","description":"other workspace"}`)

	completeReq := scoped(httptest.NewRequest(http.MethodPatch, "/api/tasks/3/complete", nil), 1, 10, model.WorkspaceRoleMember)
	completeRes := httptest.NewRecorder()
	h.TaskByID(completeRes, completeReq)
	if completeRes.Code != http.StatusOK {
		t.Fatalf("complete status = %d", completeRes.Code)
	}

	listReq := scoped(httptest.NewRequest(http.MethodGet, "/api/tasks?search=go&completed=false&page=1&limit=1&sort=title&order=asc", nil), 1, 10, model.WorkspaceRoleMember)
	listRes := httptest.NewRecorder()
	h.Tasks(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list status = %d; body=%s", listRes.Code, listRes.Body.String())
	}

	var envelope testEnvelope
	if err := json.Unmarshal(listRes.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var page model.TaskPage
	if err := json.Unmarshal(envelope.Data, &page); err != nil {
		t.Fatal(err)
	}
	if page.Pagination.Total != 1 || len(page.Items) != 1 || page.Items[0].Title != "Go API" {
		t.Fatalf("page = %+v", page)
	}
}

func TestTaskHandlerRejectsInvalidQuery(t *testing.T) {
	h := newTestHandler()
	queries := []string{
		"/api/tasks?page=0",
		"/api/tasks?limit=101",
		"/api/tasks?completed=yes",
		"/api/tasks?sort=unknown",
		"/api/tasks?order=random",
	}
	for _, target := range queries {
		req := scoped(httptest.NewRequest(http.MethodGet, target, nil), 1, 10, model.WorkspaceRoleMember)
		res := httptest.NewRecorder()
		h.Tasks(res, req)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want %d; body=%s", target, res.Code, http.StatusBadRequest, res.Body.String())
		}
	}
}

func TestTaskHandlerRequiresAuthenticationAndHidesOtherWorkspaceTasks(t *testing.T) {
	h := newTestHandler()
	unauthorized := httptest.NewRecorder()
	h.Tasks(unauthorized, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	createTask(t, h, 1, 10, `{"title":"Private"}`)
	getReq := scoped(httptest.NewRequest(http.MethodGet, "/api/tasks/1", nil), 1, 20, model.WorkspaceRoleMember)
	getRes := httptest.NewRecorder()
	h.TaskByID(getRes, getReq)
	if getRes.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace get status = %d", getRes.Code)
	}
}
