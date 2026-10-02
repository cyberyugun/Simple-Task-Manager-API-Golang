package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func decodeEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) testEnvelope {
	t.Helper()

	var envelope testEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to decode response: %v; body=%s", err, recorder.Body.String())
	}
	return envelope
}

func TestTaskHandlerCreateAndList(t *testing.T) {
	h := newTestHandler()

	createReq := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{"title":"Learn Go","description":"Write tests"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRes := httptest.NewRecorder()
	h.Tasks(createRes, createReq)

	if createRes.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body=%s", createRes.Code, http.StatusCreated, createRes.Body.String())
	}
	createEnvelope := decodeEnvelope(t, createRes)
	if !createEnvelope.Success {
		t.Fatalf("create success = false; body=%s", createRes.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
	listRes := httptest.NewRecorder()
	h.Tasks(listRes, listReq)

	if listRes.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d", listRes.Code, http.StatusOK)
	}

	listEnvelope := decodeEnvelope(t, listRes)
	var tasks []map[string]any
	if err := json.Unmarshal(listEnvelope.Data, &tasks); err != nil {
		t.Fatalf("failed to decode task list: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("task count = %d, want 1", len(tasks))
	}
}

func TestTaskHandlerFullLifecycle(t *testing.T) {
	h := newTestHandler()

	createReq := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{"title":"Task One","description":"Initial"}`))
	createRes := httptest.NewRecorder()
	h.Tasks(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d", createRes.Code, http.StatusCreated)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/tasks/1", nil)
	getRes := httptest.NewRecorder()
	h.TaskByID(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d", getRes.Code, http.StatusOK)
	}

	updateReq := httptest.NewRequest(http.MethodPut, "/api/tasks/1", strings.NewReader(`{"title":"Task Updated","description":"Changed"}`))
	updateRes := httptest.NewRecorder()
	h.TaskByID(updateRes, updateReq)
	if updateRes.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d; body=%s", updateRes.Code, http.StatusOK, updateRes.Body.String())
	}

	completeReq := httptest.NewRequest(http.MethodPatch, "/api/tasks/1/complete", nil)
	completeRes := httptest.NewRecorder()
	h.TaskByID(completeRes, completeReq)
	if completeRes.Code != http.StatusOK {
		t.Fatalf("complete status = %d, want %d", completeRes.Code, http.StatusOK)
	}

	completeEnvelope := decodeEnvelope(t, completeRes)
	var task struct {
		Completed bool `json:"completed"`
	}
	if err := json.Unmarshal(completeEnvelope.Data, &task); err != nil {
		t.Fatalf("failed to decode completed task: %v", err)
	}
	if !task.Completed {
		t.Fatal("completed = false, want true")
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/tasks/1", nil)
	deleteRes := httptest.NewRecorder()
	h.TaskByID(deleteRes, deleteReq)
	if deleteRes.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want %d", deleteRes.Code, http.StatusOK)
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/api/tasks/1", nil)
	missingRes := httptest.NewRecorder()
	h.TaskByID(missingRes, missingReq)
	if missingRes.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d", missingRes.Code, http.StatusNotFound)
	}
}

func TestTaskHandlerValidationAndMethodErrors(t *testing.T) {
	h := newTestHandler()

	invalidJSONReq := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{"title":`))
	invalidJSONRes := httptest.NewRecorder()
	h.Tasks(invalidJSONRes, invalidJSONReq)
	if invalidJSONRes.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON status = %d, want %d", invalidJSONRes.Code, http.StatusBadRequest)
	}

	missingTitleReq := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{"title":"   "}`))
	missingTitleRes := httptest.NewRecorder()
	h.Tasks(missingTitleRes, missingTitleReq)
	if missingTitleRes.Code != http.StatusBadRequest {
		t.Fatalf("missing title status = %d, want %d", missingTitleRes.Code, http.StatusBadRequest)
	}

	invalidIDReq := httptest.NewRequest(http.MethodGet, "/api/tasks/abc", nil)
	invalidIDRes := httptest.NewRecorder()
	h.TaskByID(invalidIDRes, invalidIDReq)
	if invalidIDRes.Code != http.StatusBadRequest {
		t.Fatalf("invalid ID status = %d, want %d", invalidIDRes.Code, http.StatusBadRequest)
	}

	methodReq := httptest.NewRequest(http.MethodPatch, "/api/tasks", nil)
	methodRes := httptest.NewRecorder()
	h.Tasks(methodRes, methodReq)
	if methodRes.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method status = %d, want %d", methodRes.Code, http.StatusMethodNotAllowed)
	}
}
