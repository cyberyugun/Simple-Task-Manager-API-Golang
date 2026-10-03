package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

type LifecycleHandler struct {
	service *service.LifecycleService
}

func NewLifecycleHandler(service *service.LifecycleService) *LifecycleHandler {
	return &LifecycleHandler{service: service}
}

func (h *LifecycleHandler) Runs(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit := 50
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 || parsed > 200 {
				response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "limit must be between 1 and 200"})
				return
			}
			limit = parsed
		}
		items, err := h.service.ListRuns(userID, workspaceID, limit)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		item, err := h.service.Run(userID, workspaceID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *LifecycleHandler) ExportPrivacyRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, requestID, ok := lifecycleRequestScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.ExportPrivacyRequest(userID, workspaceID, requestID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
}

func (h *LifecycleHandler) ErasePrivacyRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, requestID, ok := lifecycleRequestScope(w, r)
	if !ok {
		return
	}
	count, err := h.service.ErasePrivacyRequest(userID, workspaceID, requestID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{
		Success: true,
		Data:    map[string]any{"anonymized_tasks": count},
	})
}

func (h *LifecycleHandler) Consents(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.ListConsents(userID, workspaceID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateConsentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.AddConsent(userID, workspaceID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *LifecycleHandler) Report(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.Report(userID, workspaceID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *LifecycleHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrGovernanceAccessDenied):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrPrivacyLegalHold):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrPrivacyExportType),
		errors.Is(err, service.ErrPrivacyEraseType),
		errors.Is(err, service.ErrInvalidConsent):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrPrivacyRequestNotFound),
		errors.Is(err, repository.ErrWorkspaceNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}

func lifecycleRequestScope(w http.ResponseWriter, r *http.Request) (int64, int64, int64, bool) {
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return 0, 0, 0, false
	}
	requestID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("request_id")), 10, 64)
	if err != nil || requestID <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid privacy request id"})
		return 0, 0, 0, false
	}
	return userID, workspaceID, requestID, true
}
