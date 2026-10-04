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

type DataPlatformHandler struct {
	service *service.DataPlatformService
}

func NewDataPlatformHandler(service *service.DataPlatformService) *DataPlatformHandler {
	return &DataPlatformHandler{service: service}
}

func (h *DataPlatformHandler) Adapters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: h.service.Adapters()})
}

func (h *DataPlatformHandler) BIContracts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: h.service.BIContracts()})
}

func (h *DataPlatformHandler) Connections(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Connections(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateDataPlatformConnectionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateConnection(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *DataPlatformHandler) Schemas(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	connectionID, ok := pathInt64(w, r.PathValue("connection_id"), "invalid data platform connection id")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Schemas(userID, organizationID, connectionID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateDatasetSchemaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateSchema(userID, organizationID, connectionID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *DataPlatformHandler) Exports(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	connectionID, ok := pathInt64(w, r.PathValue("connection_id"), "invalid data platform connection id")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.ExportJobs(userID, organizationID, connectionID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		req := model.RunDataExportRequest{}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				badJSON(w)
				return
			}
		}
		item, err := h.service.RunExport(userID, organizationID, connectionID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *DataPlatformHandler) Checkpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	connectionID, ok := pathInt64(w, r.PathValue("connection_id"), "invalid data platform connection id")
	if !ok {
		return
	}
	item, err := h.service.Checkpoint(userID, organizationID, connectionID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *DataPlatformHandler) Lineage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.Lineage(userID, organizationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *DataPlatformHandler) ReverseETLHooks(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.ReverseETLHooks(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateReverseETLHookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateReverseETLHook(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *DataPlatformHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
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

func (h *DataPlatformHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrDataPlatformForbidden):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidDataPlatformConnection),
		errors.Is(err, service.ErrInvalidDatasetSchema),
		errors.Is(err, service.ErrInvalidDataExportRequest),
		errors.Is(err, service.ErrInvalidReverseETLHook):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrIncompatibleDatasetSchema),
		errors.Is(err, service.ErrDataPlatformCostBudgetExceeded):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrDataPlatformConnectionNotFound),
		errors.Is(err, repository.ErrDatasetSchemaNotFound),
		errors.Is(err, repository.ErrDataExportJobNotFound),
		errors.Is(err, repository.ErrDataExportCheckpointNotFound),
		errors.Is(err, repository.ErrReverseETLHookNotFound),
		errors.Is(err, repository.ErrOrganizationNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
