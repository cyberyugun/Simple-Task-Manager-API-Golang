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

func requestScope(w http.ResponseWriter, r *http.Request) (int64, model.WorkspaceAccess, bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "authentication required"})
		return 0, model.WorkspaceAccess{}, false
	}
	access, ok := middleware.WorkspaceAccessFromContext(r.Context())
	if !ok {
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "workspace scope is missing"})
		return 0, model.WorkspaceAccess{}, false
	}
	return userID, access, true
}

func (h *TaskHandler) Tasks(w http.ResponseWriter, r *http.Request) {
	userID, access, ok := requestScope(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.list(w, r, access.ID)
	case http.MethodPost:
		h.create(w, r, userID, access)
	default:
		response.JSON(w, http.StatusMethodNotAllowed, response.Envelope{Success: false, Message: "method not allowed"})
	}
}

func (h *TaskHandler) TaskByID(w http.ResponseWriter, r *http.Request) {
	_, access, ok := requestScope(w, r)
	if !ok {
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
		h.complete(w, access.ID, id)
		return
	}

	if len(parts) != 1 {
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "route not found"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.get(w, access.ID, id)
	case http.MethodPut:
		h.update(w, r, access.ID, id)
	case http.MethodDelete:
		h.delete(w, access.ID, id)
	default:
		response.JSON(w, http.StatusMethodNotAllowed, response.Envelope{Success: false, Message: "method not allowed"})
	}
}

func (h *TaskHandler) list(w http.ResponseWriter, r *http.Request, workspaceID int64) {
	query, err := parseTaskQuery(r)
	if err != nil {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
		return
	}

	result, err := h.service.FindAll(workspaceID, query)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: result})
}

func parseTaskQuery(r *http.Request) (model.TaskQuery, error) {
	values := r.URL.Query()
	query := model.TaskQuery{
		Search: values.Get("search"),
		Sort:   values.Get("sort"),
		Order:  values.Get("order"),
	}

	if value := values.Get("page"); value != "" {
		page, err := strconv.Atoi(value)
		if err != nil || page < 1 {
			return model.TaskQuery{}, errors.New("page must be a positive integer")
		}
		query.Page = page
	}
	if value := values.Get("limit"); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			return model.TaskQuery{}, errors.New("limit must be between 1 and 100")
		}
		query.Limit = limit
	}
	if value := values.Get("completed"); value != "" {
		if value != "true" && value != "false" {
			return model.TaskQuery{}, errors.New("completed must be true or false")
		}
		completed := value == "true"
		query.Completed = &completed
	}

	return query, nil
}

func (h *TaskHandler) create(w http.ResponseWriter, r *http.Request, userID int64, access model.WorkspaceAccess) {
	defer r.Body.Close()

	var req model.CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid JSON body"})
		return
	}

	task, err := h.service.Create(userID, access, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: task})
}

func (h *TaskHandler) get(w http.ResponseWriter, workspaceID, id int64) {
	task, err := h.service.FindByID(workspaceID, id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: task})
}

func (h *TaskHandler) update(w http.ResponseWriter, r *http.Request, workspaceID, id int64) {
	defer r.Body.Close()

	var req model.UpdateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid JSON body"})
		return
	}

	task, err := h.service.Update(workspaceID, id, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: task})
}

func (h *TaskHandler) complete(w http.ResponseWriter, workspaceID, id int64) {
	task, err := h.service.Complete(workspaceID, id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: task})
}

func (h *TaskHandler) delete(w http.ResponseWriter, workspaceID, id int64) {
	if err := h.service.Delete(workspaceID, id); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "task deleted"})
}

func (h *TaskHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrTaskNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidTask), errors.Is(err, service.ErrInvalidTaskQuery):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
