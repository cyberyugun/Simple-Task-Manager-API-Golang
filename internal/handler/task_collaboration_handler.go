package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

type TaskCollaborationHandler struct {
	service *service.TaskCollaborationService
}

func NewTaskCollaborationHandler(service *service.TaskCollaborationService) *TaskCollaborationHandler {
	return &TaskCollaborationHandler{service: service}
}

func (h *TaskCollaborationHandler) Projects(w http.ResponseWriter, r *http.Request) {
	userID, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Projects(access.ID, r.URL.Query().Get("archived") == "true")
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateTaskProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateProject(userID, access, req)
		h.write(w, item, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *TaskCollaborationHandler) ProjectByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		methodNotAllowed(w)
		return
	}
	_, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	id, ok := collaborationPathID(w, r.PathValue("project_id"), "invalid project id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req struct {
		Archived bool `json:"archived"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.ArchiveProject(access.ID, id, req.Archived)
	h.write(w, item, err, http.StatusOK)
}

func (h *TaskCollaborationHandler) Lists(w http.ResponseWriter, r *http.Request) {
	userID, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		var projectID *int64
		if raw := r.URL.Query().Get("project_id"); raw != "" {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || id <= 0 {
				response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid project id"})
				return
			}
			projectID = &id
		}
		items, err := h.service.Lists(access.ID, projectID, r.URL.Query().Get("archived") == "true")
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateTaskListRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateList(userID, access, req)
		h.write(w, item, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *TaskCollaborationHandler) ListByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		methodNotAllowed(w)
		return
	}
	_, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	id, ok := collaborationPathID(w, r.PathValue("list_id"), "invalid list id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req struct {
		Archived bool `json:"archived"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.ArchiveList(access.ID, id, req.Archived)
	h.write(w, item, err, http.StatusOK)
}

func (h *TaskCollaborationHandler) Labels(w http.ResponseWriter, r *http.Request) {
	_, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Labels(access.ID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateTaskLabelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateLabel(access, req)
		h.write(w, item, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *TaskCollaborationHandler) CustomFields(w http.ResponseWriter, r *http.Request) {
	_, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.CustomFields(access.ID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateTaskCustomFieldRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateCustomField(access, req)
		h.write(w, item, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *TaskCollaborationHandler) TaskLabels(w http.ResponseWriter, r *http.Request) {
	userID, access, taskID, ok := taskRelationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.TaskLabels(access.ID, taskID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req struct {
			LabelID int64 `json:"label_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LabelID <= 0 {
			response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid label id"})
			return
		}
		err := h.service.AddLabel(userID, access.ID, taskID, req.LabelID)
		h.write(w, map[string]any{"label_id": req.LabelID}, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *TaskCollaborationHandler) TaskLabelByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	userID, access, taskID, ok := taskRelationScope(w, r)
	if !ok {
		return
	}
	labelID, ok := collaborationPathID(w, r.PathValue("label_id"), "invalid label id")
	if !ok {
		return
	}
	err := h.service.RemoveLabel(userID, access.ID, taskID, labelID)
	h.write(w, map[string]any{"label_id": labelID}, err, http.StatusOK)
}

func (h *TaskCollaborationHandler) Assignees(w http.ResponseWriter, r *http.Request) {
	userID, access, taskID, ok := taskRelationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Assignees(access.ID, taskID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.AddTaskUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID <= 0 {
			response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid user id"})
			return
		}
		err := h.service.AddAssignee(userID, access.ID, taskID, req.UserID)
		h.write(w, map[string]any{"user_id": req.UserID}, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *TaskCollaborationHandler) AssigneeByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	actorUserID, access, taskID, ok := taskRelationScope(w, r)
	if !ok {
		return
	}
	userID, ok := collaborationPathID(w, r.PathValue("user_id"), "invalid user id")
	if !ok {
		return
	}
	err := h.service.RemoveAssignee(actorUserID, access.ID, taskID, userID)
	h.write(w, map[string]any{"user_id": userID}, err, http.StatusOK)
}

func (h *TaskCollaborationHandler) Watchers(w http.ResponseWriter, r *http.Request) {
	userID, access, taskID, ok := taskRelationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Watchers(access.ID, taskID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.AddTaskUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID <= 0 {
			response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid user id"})
			return
		}
		err := h.service.AddWatcher(userID, access.ID, taskID, req.UserID)
		h.write(w, map[string]any{"user_id": req.UserID}, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *TaskCollaborationHandler) WatcherByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	actorUserID, access, taskID, ok := taskRelationScope(w, r)
	if !ok {
		return
	}
	userID, ok := collaborationPathID(w, r.PathValue("user_id"), "invalid user id")
	if !ok {
		return
	}
	err := h.service.RemoveWatcher(actorUserID, access.ID, taskID, userID)
	h.write(w, map[string]any{"user_id": userID}, err, http.StatusOK)
}

func (h *TaskCollaborationHandler) Comments(w http.ResponseWriter, r *http.Request) {
	userID, access, taskID, ok := taskRelationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Comments(access.ID, taskID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateTaskCommentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateComment(userID, access.ID, taskID, req)
		h.write(w, item, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *TaskCollaborationHandler) CommentByID(w http.ResponseWriter, r *http.Request) {
	userID, access, taskID, ok := taskRelationScope(w, r)
	if !ok {
		return
	}
	commentID, ok := collaborationPathID(w, r.PathValue("comment_id"), "invalid comment id")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodPut:
		defer r.Body.Close()
		var req model.UpdateTaskCommentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.UpdateComment(userID, access.ID, taskID, commentID, req)
		h.write(w, item, err, http.StatusOK)
	case http.MethodDelete:
		item, err := h.service.DeleteComment(userID, access.ID, taskID, commentID)
		h.write(w, item, err, http.StatusOK)
	default:
		methodNotAllowed(w)
	}
}

func (h *TaskCollaborationHandler) Dependencies(w http.ResponseWriter, r *http.Request) {
	userID, access, taskID, ok := taskRelationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Dependencies(access.ID, taskID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.AddTaskDependencyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.AddDependency(userID, access.ID, taskID, req)
		h.write(w, item, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *TaskCollaborationHandler) DependencyByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	userID, access, taskID, ok := taskRelationScope(w, r)
	if !ok {
		return
	}
	dependencyID, ok := collaborationPathID(w, r.PathValue("depends_on_id"), "invalid dependency id")
	if !ok {
		return
	}
	err := h.service.RemoveDependency(userID, access.ID, taskID, dependencyID)
	h.write(w, map[string]any{"depends_on_task_id": dependencyID}, err, http.StatusOK)
}

func (h *TaskCollaborationHandler) Recurrence(w http.ResponseWriter, r *http.Request) {
	userID, access, taskID, ok := taskRelationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := h.service.Recurrence(access.ID, taskID)
		h.write(w, item, err, http.StatusOK)
	case http.MethodPut:
		defer r.Body.Close()
		var req model.SetTaskRecurrenceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.SetRecurrence(userID, access.ID, taskID, req)
		h.write(w, item, err, http.StatusOK)
	case http.MethodDelete:
		err := h.service.DeleteRecurrence(userID, access.ID, taskID)
		h.write(w, map[string]any{"task_id": taskID}, err, http.StatusOK)
	default:
		methodNotAllowed(w)
	}
}

func (h *TaskCollaborationHandler) Activity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	_, access, taskID, ok := taskRelationScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.Activity(access.ID, taskID)
	h.write(w, items, err, http.StatusOK)
}

func (h *TaskCollaborationHandler) CustomFieldValue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	userID, access, taskID, ok := taskRelationScope(w, r)
	if !ok {
		return
	}
	fieldID, ok := collaborationPathID(w, r.PathValue("field_id"), "invalid field id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.SetTaskCustomFieldValueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.SetCustomFieldValue(userID, access.ID, taskID, fieldID, req)
	h.write(w, item, err, http.StatusOK)
}

func (h *TaskCollaborationHandler) CustomFieldValues(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	_, access, taskID, ok := taskRelationScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.TaskCustomFieldValues(access.ID, taskID)
	h.write(w, items, err, http.StatusOK)
}

func taskRelationScope(w http.ResponseWriter, r *http.Request) (int64, model.WorkspaceAccess, int64, bool) {
	userID, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return 0, model.WorkspaceAccess{}, 0, false
	}
	taskID, ok := collaborationPathID(w, r.PathValue("id"), "invalid task id")
	if !ok {
		return 0, model.WorkspaceAccess{}, 0, false
	}
	return userID, access, taskID, true
}

func collaborationPathID(w http.ResponseWriter, raw, message string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: message})
		return 0, false
	}
	return id, true
}

func (h *TaskCollaborationHandler) write(w http.ResponseWriter, data any, err error, status int) {
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, status, response.Envelope{Success: true, Data: data})
}

func (h *TaskCollaborationHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrTaskNotFound),
		errors.Is(err, repository.ErrTaskProjectNotFound),
		errors.Is(err, repository.ErrTaskListNotFound),
		errors.Is(err, repository.ErrTaskLabelNotFound),
		errors.Is(err, repository.ErrTaskCommentNotFound),
		errors.Is(err, repository.ErrTaskDependencyNotFound),
		errors.Is(err, repository.ErrTaskRecurrenceNotFound),
		errors.Is(err, repository.ErrTaskCustomFieldNotFound),
		errors.Is(err, repository.ErrWorkspaceMemberNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrTaskRelationExists):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrTaskDependencyCycle):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidTaskProject),
		errors.Is(err, service.ErrInvalidTaskList),
		errors.Is(err, service.ErrInvalidTaskLabel),
		errors.Is(err, service.ErrInvalidTaskComment),
		errors.Is(err, service.ErrInvalidTaskDependency),
		errors.Is(err, service.ErrInvalidTaskRecurrence),
		errors.Is(err, service.ErrInvalidTaskCustomField):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
