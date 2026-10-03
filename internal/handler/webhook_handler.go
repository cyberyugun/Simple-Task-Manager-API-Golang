package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"go-simple-task-api/internal/middleware"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

type WebhookHandler struct {
	service *service.WebhookService
}

func NewWebhookHandler(service *service.WebhookService) *WebhookHandler {
	return &WebhookHandler{service: service}
}

func (h *WebhookHandler) Subscriptions(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := webhookScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.List(userID, workspaceID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateWebhookSubscriptionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.Create(userID, workspaceID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *WebhookHandler) SubscriptionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := webhookScope(w, r)
	if !ok {
		return
	}
	subscriptionID, err := strconv.ParseInt(r.PathValue("subscription_id"), 10, 64)
	if err != nil || subscriptionID <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid webhook subscription id"})
		return
	}
	if err := h.service.Delete(userID, workspaceID, subscriptionID); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "webhook subscription deleted"})
}

func webhookScope(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return 0, 0, false
	}
	workspaceID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || workspaceID <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid workspace id"})
		return 0, 0, false
	}
	return userID, workspaceID, true
}

func (h *WebhookHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrWorkspaceNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "workspace not found"})
	case errors.Is(err, repository.ErrWebhookSubscriptionNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "webhook subscription not found"})
	case errors.Is(err, service.ErrWorkspaceForbidden):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidWebhookURL), errors.Is(err, service.ErrInvalidWebhookEvents):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
