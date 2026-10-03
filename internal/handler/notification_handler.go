package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

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

func (h *NotificationHandler) Inbox(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "authentication required"})
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	items, err := h.service.Inbox(userID, limit)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *NotificationHandler) NotificationByID(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "authentication required"})
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	notificationID, ok := pathInt64(w, r.PathValue("notification_id"), "invalid notification id")
	if !ok {
		return
	}
	item, err := h.service.MarkRead(userID, notificationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *NotificationHandler) Preference(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "authentication required"})
		return
	}
	orgID, valid := optionalOrganizationID(w, r)
	if !valid {
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := h.service.Preference(userID, orgID)
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
		item, err := h.service.UpdatePreference(userID, orgID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *NotificationHandler) Stats(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "authentication required"})
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	orgID, valid := optionalOrganizationID(w, r)
	if !valid {
		return
	}
	stats, err := h.service.Stats(userID, orgID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: stats})
}

func optionalOrganizationID(w http.ResponseWriter, r *http.Request) (*int64, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("organization_id"))
	if raw == "" {
		return nil, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid organization id"})
		return nil, false
	}
	return &id, true
}

func (h *NotificationHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNotificationForbidden):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidNotificationPreference):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrNotificationNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
