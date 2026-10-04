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

type ExtensionHandler struct {
	service *service.ExtensionService
}

func NewExtensionHandler(service *service.ExtensionService) *ExtensionHandler {
	return &ExtensionHandler{service: service}
}

func (h *ExtensionHandler) MarketplaceApps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	items, err := h.service.MarketplaceApps(r.URL.Query().Get("category"))
	h.write(w, items, err, http.StatusOK)
}

func (h *ExtensionHandler) MarketplaceApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	appID, ok := pathInt64(w, r.PathValue("app_id"), "invalid marketplace application id")
	if !ok {
		return
	}
	item, err := h.service.MarketplaceApp(appID)
	h.write(w, item, err, http.StatusOK)
}

func (h *ExtensionHandler) Publishers(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := extensionWorkspaceScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Publishers(userID, workspaceID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateExtensionPublisherRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreatePublisher(userID, workspaceID, req)
		h.write(w, item, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *ExtensionHandler) SubmitPublisher(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := extensionWorkspaceScope(w, r)
	if !ok {
		return
	}
	publisherID, ok := pathInt64(w, r.PathValue("publisher_id"), "invalid extension publisher id")
	if !ok {
		return
	}
	item, err := h.service.SubmitPublisher(userID, workspaceID, publisherID)
	h.write(w, item, err, http.StatusOK)
}

func (h *ExtensionHandler) ReviewPublisher(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := extensionWorkspaceScope(w, r)
	if !ok {
		return
	}
	publisherID, ok := pathInt64(w, r.PathValue("publisher_id"), "invalid extension publisher id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.ReviewExtensionPublisherRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.ReviewPublisher(userID, workspaceID, publisherID, req)
	h.write(w, item, err, http.StatusOK)
}

func (h *ExtensionHandler) WorkspaceApps(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := extensionWorkspaceScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.WorkspaceApps(userID, workspaceID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateMarketplaceApplicationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateApplication(userID, workspaceID, req)
		h.write(w, item, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *ExtensionHandler) SubmitApplication(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := extensionWorkspaceScope(w, r)
	if !ok {
		return
	}
	appID, ok := pathInt64(w, r.PathValue("app_id"), "invalid marketplace application id")
	if !ok {
		return
	}
	item, err := h.service.SubmitApplication(userID, workspaceID, appID)
	h.write(w, item, err, http.StatusOK)
}

func (h *ExtensionHandler) ReviewApplication(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := extensionWorkspaceScope(w, r)
	if !ok {
		return
	}
	appID, ok := pathInt64(w, r.PathValue("app_id"), "invalid marketplace application id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.ReviewMarketplaceApplicationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.ReviewApplication(userID, workspaceID, appID, req)
	h.write(w, item, err, http.StatusOK)
}

func (h *ExtensionHandler) Installations(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Installations(userID, organizationID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.InstallExtensionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.Install(userID, organizationID, req)
		h.write(w, item, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *ExtensionHandler) InstallationByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	installationID, ok := pathInt64(w, r.PathValue("installation_id"), "invalid extension installation id")
	if !ok {
		return
	}
	item, err := h.service.Uninstall(userID, organizationID, installationID)
	h.write(w, item, err, http.StatusOK)
}

func (h *ExtensionHandler) RotateSecret(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	installationID, ok := pathInt64(w, r.PathValue("installation_id"), "invalid extension installation id")
	if !ok {
		return
	}
	item, err := h.service.RotateSecret(userID, organizationID, installationID)
	h.write(w, item, err, http.StatusCreated)
}

func (h *ExtensionHandler) Usage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	installationID, ok := pathInt64(w, r.PathValue("installation_id"), "invalid extension installation id")
	if !ok {
		return
	}
	days := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 366 {
			response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "days must be between 1 and 366"})
			return
		}
		days = value
	}
	item, err := h.service.Usage(userID, organizationID, installationID, days)
	h.write(w, item, err, http.StatusOK)
}

func (h *ExtensionHandler) Subscriptions(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	installationID, ok := pathInt64(w, r.PathValue("installation_id"), "invalid extension installation id")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Subscriptions(userID, organizationID, installationID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateExtensionEventSubscriptionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateSubscription(userID, organizationID, installationID, req)
		h.write(w, item, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *ExtensionHandler) SubscriptionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	installationID, ok := pathInt64(w, r.PathValue("installation_id"), "invalid extension installation id")
	if !ok {
		return
	}
	subscriptionID, ok := pathInt64(w, r.PathValue("subscription_id"), "invalid extension subscription id")
	if !ok {
		return
	}
	if err := h.service.DeleteSubscription(userID, organizationID, installationID, subscriptionID); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "extension subscription deleted"})
}

func (h *ExtensionHandler) Token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	defer r.Body.Close()
	var req model.ExtensionTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.ExchangeToken(req)
	h.write(w, item, err, http.StatusOK)
}

func extensionWorkspaceScope(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return 0, 0, false
	}
	workspaceID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || workspaceID <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid workspace id"})
		return 0, 0, false
	}
	return userID, workspaceID, true
}

func (h *ExtensionHandler) write(w http.ResponseWriter, data any, err error, status int) {
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, status, response.Envelope{Success: true, Data: data})
}

func (h *ExtensionHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrExtensionWorkspaceForbidden),
		errors.Is(err, service.ErrExtensionOrganizationForbidden),
		errors.Is(err, service.ErrMarketplacePublisherUnverified),
		errors.Is(err, service.ErrMarketplaceApplicationApproval):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidExtensionPublisher),
		errors.Is(err, service.ErrInvalidMarketplaceApplication),
		errors.Is(err, service.ErrInvalidExtensionInstallation),
		errors.Is(err, service.ErrInvalidExtensionConfiguration),
		errors.Is(err, service.ErrInvalidExtensionSubscription):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidExtensionPublisherFlow),
		errors.Is(err, service.ErrInvalidMarketplaceReview),
		errors.Is(err, repository.ErrExtensionInstallationExists):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidExtensionSecret):
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrExtensionQuotaExceeded):
		w.Header().Set("Retry-After", "60")
		response.JSON(w, http.StatusTooManyRequests, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrExtensionPublisherNotFound),
		errors.Is(err, repository.ErrMarketplaceApplicationNotFound),
		errors.Is(err, repository.ErrExtensionInstallationNotFound),
		errors.Is(err, repository.ErrExtensionSubscriptionNotFound),
		errors.Is(err, repository.ErrWebhookSubscriptionNotFound),
		errors.Is(err, repository.ErrOrganizationNotFound),
		errors.Is(err, repository.ErrWorkspaceNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
