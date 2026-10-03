package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

type IntegrationHandler struct {
	service *service.IntegrationService
}

func NewIntegrationHandler(service *service.IntegrationService) *IntegrationHandler {
	return &IntegrationHandler{service: service}
}

func (h *IntegrationHandler) Connectors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: h.service.Connectors()})
}

func (h *IntegrationHandler) Connections(w http.ResponseWriter, r *http.Request) {
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
		var req model.CreateIntegrationConnectionRequest
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

func (h *IntegrationHandler) ConnectionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	connectionID, ok := pathInt64(w, r.PathValue("connection_id"), "invalid integration connection id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.UpdateIntegrationConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.UpdateConnection(userID, organizationID, connectionID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *IntegrationHandler) Deliveries(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Deliveries(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateIntegrationDeliveryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.QueueDelivery(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusAccepted, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *IntegrationHandler) ReplayDelivery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	deliveryID, ok := pathInt64(w, r.PathValue("delivery_id"), "invalid integration delivery id")
	if !ok {
		return
	}
	if err := h.service.ReplayDelivery(userID, organizationID, deliveryID); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "integration delivery queued for replay"})
}

func (h *IntegrationHandler) InboundEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.InboundEvents(userID, organizationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *IntegrationHandler) Inbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	connectionID, ok := pathInt64(w, r.PathValue("connection_id"), "invalid integration connection id")
	if !ok {
		return
	}
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024+1))
	if err != nil || len(raw) == 0 || len(raw) > 1024*1024 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid integration payload"})
		return
	}
	item, duplicate, err := h.service.AcceptInbound(connectionID, r.Header.Get("X-Integration-Signature"), raw)
	if err != nil {
		h.handleError(w, err)
		return
	}
	if duplicate {
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "duplicate integration event ignored", Data: item})
		return
	}
	response.JSON(w, http.StatusAccepted, response.Envelope{Success: true, Data: item})
}

func (h *IntegrationHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrIntegrationForbidden):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidIntegrationConnection),
		errors.Is(err, service.ErrInvalidIntegrationDelivery):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrIntegrationSignature):
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrIntegrationConnectionDisabled):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrIntegrationConnectionNotFound),
		errors.Is(err, repository.ErrIntegrationDeliveryNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
