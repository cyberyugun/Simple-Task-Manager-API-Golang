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

type ConnectorSecurityHandler struct {
	service *service.ConnectorSecurityService
}

func NewConnectorSecurityHandler(service *service.ConnectorSecurityService) *ConnectorSecurityHandler {
	return &ConnectorSecurityHandler{service: service}
}

func (h *ConnectorSecurityHandler) Backends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: h.service.SecretBackends()})
}

func (h *ConnectorSecurityHandler) BeginOAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, connectionID, ok := connectorSecurityScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.BeginOAuth(userID, organizationID, connectionID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
}

func (h *ConnectorSecurityHandler) CompleteOAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, connectionID, ok := connectorSecurityScope(w, r)
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.CompleteConnectorOAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.CompleteOAuth(userID, organizationID, connectionID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *ConnectorSecurityHandler) Credential(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, connectionID, ok := connectorSecurityScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := h.service.Credential(userID, organizationID, connectionID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *ConnectorSecurityHandler) Rotate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, connectionID, ok := connectorSecurityScope(w, r)
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.RotateConnectorCredentialRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
	}
	item, err := h.service.Rotate(userID, organizationID, connectionID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *ConnectorSecurityHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, connectionID, ok := connectorSecurityScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.Revoke(userID, organizationID, connectionID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *ConnectorSecurityHandler) Test(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, connectionID, ok := connectorSecurityScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.TestConnection(userID, organizationID, connectionID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *ConnectorSecurityHandler) Audit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, connectionID, ok := connectorSecurityScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.AccessAudit(userID, organizationID, connectionID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func connectorSecurityScope(w http.ResponseWriter, r *http.Request) (int64, int64, int64, bool) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return 0, 0, 0, false
	}
	connectionID, ok := pathInt64(w, r.PathValue("connection_id"), "invalid integration connection id")
	if !ok {
		return 0, 0, 0, false
	}
	return userID, organizationID, connectionID, true
}

func (h *ConnectorSecurityHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrIntegrationForbidden):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrConnectorOAuthInvalid),
		errors.Is(err, service.ErrConnectorOAuthState),
		errors.Is(err, service.ErrConnectorOAuthExchange),
		errors.Is(err, service.ErrConnectorScopeMismatch),
		errors.Is(err, service.ErrConnectorCredentialRotation):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrConnectorCredentialRevoked),
		errors.Is(err, service.ErrConnectorCredentialExpired):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrIntegrationConnectionNotFound),
		errors.Is(err, repository.ErrConnectorCredentialNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
