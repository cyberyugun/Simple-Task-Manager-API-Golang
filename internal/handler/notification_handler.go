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

type NotificationHandler struct {
	service *service.NotificationService
}

func NewNotificationHandler(service *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{service: service}
}

func notificationUser(w http.ResponseWriter, r *http.Request) (int64, bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return 0, false
	}
	return userID, true
}

func (h *NotificationHandler) Preferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := notificationUser(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := h.service.Preferences(userID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	case http.MethodPut:
		defer r.Body.Close()
		var req model.UpdateNotificationPreferenceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.UpdatePreferences(userID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *NotificationHandler) Endpoints(w http.ResponseWriter, r *http.Request) {
	userID, ok := notificationUser(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Endpoints(userID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateNotificationEndpointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateEndpoint(userID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *NotificationHandler) EndpointByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	userID, ok := notificationUser(w, r)
	if !ok {
		return
	}
	endpointID, err := strconv.ParseInt(r.PathValue("endpoint_id"), 10, 64)
	if err != nil || endpointID <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid notification endpoint id"})
		return
	}
	if err := h.service.DeleteEndpoint(userID, endpointID); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "notification endpoint deleted"})
}

func (h *NotificationHandler) Notifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, ok := notificationUser(w, r)
	if !ok {
		return
	}
	unreadOnly := r.URL.Query().Get("unread") == "true"
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 || parsed > 200 {
			response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "limit must be between 1 and 200"})
			return
		}
		limit = parsed
	}
	items, err := h.service.Notifications(userID, unreadOnly, limit)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *NotificationHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, ok := notificationUser(w, r)
	if !ok {
		return
	}
	notificationID, err := strconv.ParseInt(r.PathValue("notification_id"), 10, 64)
	if err != nil || notificationID <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid notification id"})
		return
	}
	item, err := h.service.MarkRead(userID, notificationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *NotificationHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, ok := notificationUser(w, r)
	if !ok {
		return
	}
	count, err := h.service.MarkAllRead(userID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: map[string]any{"updated": count}})
}

func (h *NotificationHandler) Deliveries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, ok := notificationUser(w, r)
	if !ok {
		return
	}
	items, err := h.service.Deliveries(userID, 100)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *NotificationHandler) RetryDelivery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, ok := notificationUser(w, r)
	if !ok {
		return
	}
	deliveryID, err := strconv.ParseInt(r.PathValue("delivery_id"), 10, 64)
	if err != nil || deliveryID <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid notification delivery id"})
		return
	}
	item, err := h.service.RetryDelivery(userID, deliveryID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *NotificationHandler) Templates(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Templates(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateNotificationTemplateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateTemplate(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *NotificationHandler) PublishTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	templateID, err := strconv.ParseInt(r.PathValue("template_id"), 10, 64)
	if err != nil || templateID <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid notification template id"})
		return
	}
	item, err := h.service.PublishTemplate(userID, organizationID, templateID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *NotificationHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidNotificationPreference),
		errors.Is(err, service.ErrInvalidNotificationEndpoint),
		errors.Is(err, service.ErrInvalidNotificationTemplate):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrNotificationForbidden):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrNotificationNotFound),
		errors.Is(err, repository.ErrNotificationEndpointNotFound),
		errors.Is(err, repository.ErrNotificationTemplateNotFound),
		errors.Is(err, repository.ErrNotificationDeliveryNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrTaskRelationExists):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: "notification endpoint already exists"})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
