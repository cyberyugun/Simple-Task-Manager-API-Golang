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

type EventFabricHandler struct {
	service *service.EventFabricService
}

func NewEventFabricHandler(service *service.EventFabricService) *EventFabricHandler {
	return &EventFabricHandler{service: service}
}

func (h *EventFabricHandler) Adapters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if _, _, ok := webhookScope(w, r); !ok {
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: h.service.Adapters()})
}

func (h *EventFabricHandler) Schemas(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := webhookScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Schemas(userID, workspaceID, strings.TrimSpace(r.URL.Query().Get("event_type")))
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateEventSchemaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, compatibility, err := h.service.CreateSchema(userID, workspaceID, req)
		if err != nil {
			if errors.Is(err, service.ErrEventSchemaIncompatible) {
				response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error(), Data: compatibility})
				return
			}
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{
			Success: true,
			Data: map[string]any{"schema": item, "compatibility": compatibility},
		})
	default:
		methodNotAllowed(w)
	}
}

func (h *EventFabricHandler) DeprecateSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := webhookScope(w, r)
	if !ok {
		return
	}
	schemaID, ok := pathInt64(w, r.PathValue("schema_id"), "invalid event schema id")
	if !ok {
		return
	}
	if err := h.service.DeprecateSchema(userID, workspaceID, schemaID); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "event schema deprecated"})
}

func (h *EventFabricHandler) Subscriptions(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := webhookScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Subscriptions(userID, workspaceID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateEventFabricSubscriptionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateSubscription(userID, workspaceID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *EventFabricHandler) SubscriptionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := webhookScope(w, r)
	if !ok {
		return
	}
	subscriptionID, ok := pathInt64(w, r.PathValue("subscription_id"), "invalid event subscription id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.UpdateEventFabricSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.UpdateSubscription(userID, workspaceID, subscriptionID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *EventFabricHandler) Offset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := webhookScope(w, r)
	if !ok {
		return
	}
	subscriptionID, ok := pathInt64(w, r.PathValue("subscription_id"), "invalid event subscription id")
	if !ok {
		return
	}
	item, err := h.service.Offset(userID, workspaceID, subscriptionID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *EventFabricHandler) ReplaySubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := webhookScope(w, r)
	if !ok {
		return
	}
	subscriptionID, ok := pathInt64(w, r.PathValue("subscription_id"), "invalid event subscription id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.EventReplayRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
	}
	count, err := h.service.ReplayRange(userID, workspaceID, subscriptionID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusAccepted, response.Envelope{
		Success: true, Message: "event replay queued", Data: map[string]any{"queued": count},
	})
}

func (h *EventFabricHandler) Routes(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := webhookScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Routes(userID, workspaceID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateEventRouteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateRoute(userID, workspaceID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *EventFabricHandler) Deliveries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := webhookScope(w, r)
	if !ok {
		return
	}
	var subscriptionID int64
	if raw := strings.TrimSpace(r.URL.Query().Get("subscription_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid event subscription id"})
			return
		}
		subscriptionID = value
	}
	items, err := h.service.Deliveries(userID, workspaceID, subscriptionID, r.URL.Query().Get("status"))
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *EventFabricHandler) RedriveDelivery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := webhookScope(w, r)
	if !ok {
		return
	}
	deliveryID, ok := pathInt64(w, r.PathValue("delivery_id"), "invalid event delivery id")
	if !ok {
		return
	}
	if err := h.service.ReplayDelivery(userID, workspaceID, deliveryID); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusAccepted, response.Envelope{Success: true, Message: "event delivery queued for redrive"})
}

func (h *EventFabricHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrEventFabricForbidden):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidEventSchema),
		errors.Is(err, service.ErrInvalidEventFabricSubscription),
		errors.Is(err, service.ErrInvalidEventRoute),
		errors.Is(err, service.ErrInvalidEventReplay):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrEventSchemaNotFound),
		errors.Is(err, repository.ErrEventFabricSubscriptionNotFound),
		errors.Is(err, repository.ErrEventRouteNotFound),
		errors.Is(err, repository.ErrEventFabricDeliveryNotFound),
		errors.Is(err, repository.ErrWorkspaceNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
