package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

type WorkflowHandler struct {
	service *service.WorkflowService
}

func NewWorkflowHandler(service *service.WorkflowService) *WorkflowHandler {
	return &WorkflowHandler{service: service}
}

func (h *WorkflowHandler) NodeSchemas(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.NodeSchemas(userID, organizationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *WorkflowHandler) Workflows(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Workflows(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateWorkflowRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		workflow, version, err := h.service.CreateWorkflow(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: map[string]any{"workflow": workflow, "version": version}})
	default:
		methodNotAllowed(w)
	}
}

func (h *WorkflowHandler) WorkflowByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	workflowID, ok := pathInt64(w, r.PathValue("workflow_id"), "invalid workflow id")
	if !ok {
		return
	}
	item, err := h.service.GetWorkflow(userID, organizationID, workflowID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *WorkflowHandler) Versions(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	workflowID, ok := pathInt64(w, r.PathValue("workflow_id"), "invalid workflow id")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Versions(userID, organizationID, workflowID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateWorkflowDraftRequest
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				badJSON(w)
				return
			}
		}
		item, err := h.service.CreateDraft(userID, organizationID, workflowID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *WorkflowHandler) VersionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, workflowID, versionID, ok := h.workflowVersionScope(w, r)
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.UpdateWorkflowDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.UpdateDraft(userID, organizationID, workflowID, versionID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *WorkflowHandler) Publish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, workflowID, versionID, ok := h.workflowVersionScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.Publish(userID, organizationID, workflowID, versionID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *WorkflowHandler) Activate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, workflowID, versionID, ok := h.workflowVersionScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.Activate(userID, organizationID, workflowID, versionID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *WorkflowHandler) Start(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	workflowID, ok := pathInt64(w, r.PathValue("workflow_id"), "invalid workflow id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.StartWorkflowExecutionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.Start(userID, organizationID, workflowID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
}

func (h *WorkflowHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.TriggerWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	items, err := h.service.Trigger(userID, organizationID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: items})
}

func (h *WorkflowHandler) Executions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.Executions(userID, organizationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *WorkflowHandler) ExecutionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	executionID, ok := pathInt64(w, r.PathValue("execution_id"), "invalid workflow execution id")
	if !ok {
		return
	}
	item, err := h.service.ExecutionDetail(userID, organizationID, executionID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *WorkflowHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	executionID, ok := pathInt64(w, r.PathValue("execution_id"), "invalid workflow execution id")
	if !ok {
		return
	}
	item, err := h.service.Cancel(userID, organizationID, executionID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *WorkflowHandler) Retry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	executionID, ok := pathInt64(w, r.PathValue("execution_id"), "invalid workflow execution id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.RetryWorkflowExecutionRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
	}
	item, err := h.service.Retry(userID, organizationID, executionID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *WorkflowHandler) DecideApproval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	approvalID, ok := pathInt64(w, r.PathValue("approval_id"), "invalid workflow approval id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.DecideWorkflowApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.DecideApproval(userID, organizationID, approvalID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *WorkflowHandler) workflowVersionScope(w http.ResponseWriter, r *http.Request) (int64, int64, int64, int64, bool) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return 0, 0, 0, 0, false
	}
	workflowID, ok := pathInt64(w, r.PathValue("workflow_id"), "invalid workflow id")
	if !ok {
		return 0, 0, 0, 0, false
	}
	versionID, ok := pathInt64(w, r.PathValue("version_id"), "invalid workflow version id")
	if !ok {
		return 0, 0, 0, 0, false
	}
	return userID, organizationID, workflowID, versionID, true
}

func (h *WorkflowHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrWorkflowForbidden):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidWorkflow),
		errors.Is(err, service.ErrWorkflowExecutorNotAllowed),
		errors.Is(err, service.ErrInvalidWorkflowDecision):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrWorkflowVersionImmutable),
		errors.Is(err, service.ErrWorkflowNotPublished),
		errors.Is(err, service.ErrWorkflowNotActive),
		errors.Is(err, service.ErrInvalidWorkflowExecution),
		errors.Is(err, service.ErrWorkflowLimit),
		errors.Is(err, service.ErrWorkflowRecursion):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrWorkflowNotFound),
		errors.Is(err, repository.ErrWorkflowVersionNotFound),
		errors.Is(err, repository.ErrWorkflowExecutionNotFound),
		errors.Is(err, repository.ErrWorkflowApprovalNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
