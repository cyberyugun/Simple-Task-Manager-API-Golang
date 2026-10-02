package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"go-simple-task-api/internal/middleware"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

type TaskHandler struct {
	service *service.TaskService
}

func NewTaskHandler(service *service.TaskService) *TaskHandler {
	return &TaskHandler{service: service}
}

func (h *TaskHandler) Tasks(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "authentication required"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.list(w, userID)
	case http.MethodPost:
		h.create(w, r, userID)
	default:
		response.JSON(w, http.StatusMethodNotAllowed, response.Envelope{Success: false, Message: "method not allowed"})
	}
}

func (h *TaskHandler) TaskByID(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "authentication required"})
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "task not found"})
		return
	}

	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid task id"})
		return
	}

	if len(parts) == 2 && parts[1] == "complete" {
		if r.Method != http.MethodPatch {
			response.JSON(w, http.StatusMethodNotAllowed, response.Envelope{Success: false, Message: "method not allowed"})
			return
		}
		h.complete(w, userID, id)
		return
	}

	if len(parts) != 1 {
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "route not found"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.get(w, userID, id)
	case http.MethodPut:
		h.update(w, r, userID, id)
	case http.MethodDelete:
		h.delete(w, userID, id)
	default:
		response.JSON(w, http.StatusMethodNotAllowed, response.Envelope{Success: false, Message: "method not allowed"})
	}
}

func (h *TaskHandler) list(w http.ResponseWriter, userID int64) {
	tasks, err := h.service.FindAll(userID)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "failed to get tasks"})
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: tasks})
}

func (h *TaskHandler) create(w http.ResponseWriter, r *http.Request, userID int64) {
	defer r.Body.Close()

	var req model.CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid JSON body"})
		return
	}

	task, err := h.service.Create(userID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: task})
}

func (h *TaskHandler) get(w http.ResponseWriter, userID, id int64) {
	task, err := h.service.FindByID(userID, id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: task})
}

func (h *TaskHandler) update(w http.ResponseWriter, r *http.Request, userID, id int64) {
	defer r.Body.Close()

	var req model.UpdateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid JSON body"})
		return
	}

	task, err := h.service.Update(userID, id, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: task})
}

func (h *TaskHandler) complete(w http.ResponseWriter, userID, id int64) {
	task, err := h.service.Complete(userID, id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: task})
}

func (h *TaskHandler) delete(w http.ResponseWriter, userID, id int64) {
	if err := h.service.Delete(userID, id); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "task deleted"})
}

func (h *TaskHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrTaskNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidTask):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
