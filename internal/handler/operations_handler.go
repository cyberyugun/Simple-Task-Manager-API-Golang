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

type OperationsHandler struct {
	service *service.OperationsService
}

func NewOperationsHandler(service *service.OperationsService) *OperationsHandler {
	return &OperationsHandler{service: service}
}

func (h *OperationsHandler) Policy(w http.ResponseWriter, r *http.Request) {
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
		var req model.UpdateOperationsPolicyRequest
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

func (h *OperationsHandler) Costs(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Costs(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateCostAllocationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateCostAllocation(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *OperationsHandler) Alerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.Alerts(userID, organizationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *OperationsHandler) AcknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	alertID, ok := pathInt64(w, r.PathValue("alert_id"), "invalid operational alert id")
	if !ok {
		return
	}
	item, err := h.service.AcknowledgeAlert(userID, organizationID, alertID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *OperationsHandler) Maintenance(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Maintenance(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateMaintenanceWindowRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateMaintenance(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *OperationsHandler) MaintenanceByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	windowID, ok := pathInt64(w, r.PathValue("window_id"), "invalid maintenance window id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.UpdateMaintenanceWindowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.UpdateMaintenance(userID, organizationID, windowID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *OperationsHandler) Incidents(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Incidents(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateOperationalIncidentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateIncident(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *OperationsHandler) IncidentByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	incidentID, ok := pathInt64(w, r.PathValue("incident_id"), "invalid operational incident id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.UpdateOperationalIncidentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.UpdateIncident(userID, organizationID, incidentID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *OperationsHandler) Evaluate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.Evaluate(userID, organizationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *OperationsHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.Dashboard(userID, organizationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *OperationsHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrOperationsForbidden):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidOperationsPolicy),
		errors.Is(err, service.ErrInvalidCostAllocation),
		errors.Is(err, service.ErrInvalidMaintenanceWindow),
		errors.Is(err, service.ErrInvalidOperationalIncident):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrOperationalAlertNotFound),
		errors.Is(err, repository.ErrMaintenanceWindowNotFound),
		errors.Is(err, repository.ErrOperationalIncidentNotFound),
		errors.Is(err, repository.ErrOrganizationNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
