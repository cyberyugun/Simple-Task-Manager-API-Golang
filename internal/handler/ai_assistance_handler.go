package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

type AIAssistanceHandler struct {
	service *service.AIAssistanceService
}

func NewAIAssistanceHandler(service *service.AIAssistanceService) *AIAssistanceHandler {
	return &AIAssistanceHandler{service: service}
}

func (h *AIAssistanceHandler) Providers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.Providers(userID, organizationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *AIAssistanceHandler) Policy(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := h.service.Policy(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	case http.MethodPut:
		defer r.Body.Close()
		var req model.UpdateAIPolicyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.UpdatePolicy(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *AIAssistanceHandler) Assist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.AIAssistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.Assist(r.Context(), userID, organizationID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	status := http.StatusOK
	if item.Status == model.AIRequestPendingApproval {
		status = http.StatusAccepted
	}
	response.JSON(w, status, response.Envelope{Success: true, Data: item})
}

func (h *AIAssistanceHandler) Requests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.Requests(userID, organizationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *AIAssistanceHandler) Decide(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	requestID, ok := pathInt64(w, r.PathValue("request_id"), "invalid AI request id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.AIDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.Decide(userID, organizationID, requestID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *AIAssistanceHandler) Usage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	at := time.Now().UTC()
	if raw := strings.TrimSpace(r.URL.Query().Get("month")); raw != "" {
		parsed, err := time.Parse("2006-01", raw)
		if err != nil {
			response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "month must use YYYY-MM"})
			return
		}
		at = parsed
	}
	item, err := h.service.Usage(userID, organizationID, at)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *AIAssistanceHandler) SemanticSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.AISemanticSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	items, err := h.service.SemanticSearch(userID, organizationID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *AIAssistanceHandler) EvaluationCases(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.EvaluationCases(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateAIEvaluationCaseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateEvaluationCase(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *AIAssistanceHandler) RunEvaluation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	caseID, ok := pathInt64(w, r.PathValue("case_id"), "invalid AI evaluation case id")
	if !ok {
		return
	}
	item, err := h.service.RunEvaluation(r.Context(), userID, organizationID, caseID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *AIAssistanceHandler) Quality(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.Quality(userID, organizationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *AIAssistanceHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrAIAssistanceForbidden), errors.Is(err, service.ErrAIClassificationBlocked):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrAIAssistanceDisabled),
		errors.Is(err, service.ErrInvalidAIPolicy),
		errors.Is(err, service.ErrInvalidAIAssistanceRequest),
		errors.Is(err, service.ErrAIProviderUnavailable),
		errors.Is(err, service.ErrAIActionNotAllowed),
		errors.Is(err, service.ErrInvalidAIEvaluation):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrAIBudgetExceeded):
		response.JSON(w, http.StatusPaymentRequired, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrAIApprovalRequired):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrAIRequestNotFound),
		errors.Is(err, repository.ErrAIEvaluationCaseNotFound),
		errors.Is(err, repository.ErrTaskNotFound),
		errors.Is(err, repository.ErrOperationalIncidentNotFound),
		errors.Is(err, repository.ErrOrganizationNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}

func aiQueryInt64(r *http.Request, key string) (int64, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	return value, err == nil && value > 0
}
