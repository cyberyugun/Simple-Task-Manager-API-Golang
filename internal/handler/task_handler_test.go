package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-simple-task-api/internal/middleware"
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
	service := service.NewTaskService(repo)
	return NewTaskHandler(service)
}

func authenticated(req *http.Request, userID int64) *http.Request {
	return req.WithContext(middleware.WithUserID(req.Context(), userID))
}

func TestTaskHandlerCreateAndList(t *testing.T) {
	h := newTestHandler()

	createReq := authenticated(httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{"title":"Learn Go","description":"Write tests"}`)), 1)
	createRes := httptest.NewRecorder()
	h.Tasks(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body=%s", createRes.Code, http.StatusCreated, createRes.Body.String())
	}

	listReq := authenticated(httptest.NewRequest(http.MethodGet, "/api/tasks", nil), 1)
	listRes := httptest.NewRecorder()
	h.Tasks(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listRes.Code, http.StatusOK)
	}

	var envelope testEnvelope
	if err := json.Unmarshal(listRes.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var tasks []map[string]any
	if err := json.Unmarshal(envelope.Data, &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task count = %d, want 1", len(tasks))
	}
}

func TestTaskHandlerRequiresAuthentication(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	res := httptest.NewRecorder()
	h.Tasks(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestTaskHandlerHidesOtherUsersTasks(t *testing.T) {
	h := newTestHandler()
	createReq := authenticated(httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{"title":"Private"}`)), 1)
	createRes := httptest.NewRecorder()
	h.Tasks(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create status = %d", createRes.Code)
	}

	getReq := authenticated(httptest.NewRequest(http.MethodGet, "/api/tasks/1", nil), 2)
	getRes := httptest.NewRecorder()
	h.TaskByID(getRes, getReq)
	if getRes.Code != http.StatusNotFound {
		t.Fatalf("cross-user get status = %d, want %d", getRes.Code, http.StatusNotFound)
	}
}
