package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

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

func (h *WebhookHandler) Webhooks(w http.ResponseWriter, r *http.Request) {
	userID, access, ok := requestScope(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		items, err := h.service.List(access)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req struct {
			URL        string   `json:"url"`
			EventTypes []string `json:"event_types"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.Create(userID, access, structToWebhookRequest(req.URL, req.EventTypes))
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func structToWebhookRequest(url string, eventTypes []string) model.CreateWebhookSubscriptionRequest {
	return model.CreateWebhookSubscriptionRequest{URL: url, EventTypes: eventTypes}
}

func (h *WebhookHandler) WebhookByID(w http.ResponseWriter, r *http.Request) {
	_, access, ok := requestScope(w, r)
	if !ok {
		return
	}

	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/webhooks/"), "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "webhook subscription not found"})
		return
	}
	subscriptionID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || subscriptionID <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid webhook subscription id"})
		return
	}

	if len(parts) == 1 {
		if r.Method != http.MethodDelete {
			methodNotAllowed(w)
			return
		}
		if err := h.service.Delete(access, subscriptionID); err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "webhook subscription deleted"})
		return
	}

	if parts[1] != "deliveries" {
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "route not found"})
		return
	}

	if len(parts) == 2 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		limit := 100
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 || parsed > 500 {
				response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "limit must be between 1 and 500"})
				return
			}
			limit = parsed
		}
		items, err := h.service.Deliveries(access, subscriptionID, limit)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
		return
	}

	if len(parts) == 4 && parts[3] == "replay" && r.Method == http.MethodPost {
		deliveryID, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || deliveryID <= 0 {
			response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid webhook delivery id"})
			return
		}
		if err := h.service.Replay(access, subscriptionID, deliveryID); err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "webhook delivery queued for replay"})
		return
	}

	methodNotAllowed(w)
}

func (h *WebhookHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrWorkspaceForbidden):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidWebhookURL), errors.Is(err, service.ErrInvalidWebhookEvent):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrWebhookDisabled):
		response.JSON(w, http.StatusServiceUnavailable, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrWebhookSubscriptionNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrWebhookDeliveryNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
