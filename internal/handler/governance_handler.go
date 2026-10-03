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

type GovernanceHandler struct {
	service *service.GovernanceService
}

func NewGovernanceHandler(service *service.GovernanceService) *GovernanceHandler {
	return &GovernanceHandler{service: service}
}

func (h *GovernanceHandler) Policy(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := h.service.GetPolicy(userID, workspaceID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	case http.MethodPut:
		defer r.Body.Close()
		var req model.UpdateGovernancePolicyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.UpdatePolicy(userID, workspaceID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *GovernanceHandler) DataInventory(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.ListDataInventory(userID, workspaceID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPut:
		defer r.Body.Close()
		var req model.UpsertDataInventoryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.UpsertDataInventory(userID, workspaceID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *GovernanceHandler) LegalHolds(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		activeOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("active_only")), "true")
		items, err := h.service.ListLegalHolds(userID, workspaceID, activeOnly)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateLegalHoldRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateLegalHold(userID, workspaceID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *GovernanceHandler) LegalHoldByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	holdID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("hold_id")), 10, 64)
	if err != nil || holdID <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid legal hold id"})
		return
	}
	if err := h.service.ReleaseLegalHold(userID, workspaceID, holdID); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "legal hold released"})
}

func (h *GovernanceHandler) PrivacyRequests(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.ListPrivacyRequests(userID, workspaceID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreatePrivacyRequestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreatePrivacyRequest(userID, workspaceID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *GovernanceHandler) CompletePrivacyRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	requestID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("request_id")), 10, 64)
	if err != nil || requestID <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid privacy request id"})
		return
	}
	defer r.Body.Close()
	var req model.CompletePrivacyRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.CompletePrivacyRequest(userID, workspaceID, requestID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *GovernanceHandler) Evidence(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit := 100
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 || parsed > 500 {
				response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "limit must be between 1 and 500"})
				return
			}
			limit = parsed
		}
		items, err := h.service.ListComplianceEvidence(userID, workspaceID, limit)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateComplianceEvidenceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateComplianceEvidence(userID, workspaceID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *GovernanceHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrGovernanceAccessDenied):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidGovernancePolicy),
		errors.Is(err, service.ErrInvalidDataInventory),
		errors.Is(err, service.ErrInvalidLegalHold),
		errors.Is(err, service.ErrInvalidPrivacyRequest),
		errors.Is(err, service.ErrInvalidComplianceEvidence):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrWorkspaceNotFound),
		errors.Is(err, repository.ErrLegalHoldNotFound),
		errors.Is(err, repository.ErrPrivacyRequestNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
