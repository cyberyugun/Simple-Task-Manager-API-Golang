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

type DeveloperPlatformHandler struct {
	service *service.DeveloperPlatformService
}

func NewDeveloperPlatformHandler(service *service.DeveloperPlatformService) *DeveloperPlatformHandler {
	return &DeveloperPlatformHandler{service: service}
}

func (h *DeveloperPlatformHandler) Applications(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := developerScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Applications(userID, workspaceID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateDeveloperApplicationRequest
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

func (h *DeveloperPlatformHandler) ApplicationByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := developerScope(w, r)
	if !ok {
		return
	}
	appID, ok := developerPathID(w, r.PathValue("app_id"), "invalid developer application id")
	if !ok {
		return
	}
	item, err := h.service.Application(userID, workspaceID, appID)
	h.write(w, item, err, http.StatusOK)
}

func (h *DeveloperPlatformHandler) Submit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := developerScope(w, r)
	if !ok {
		return
	}
	appID, ok := developerPathID(w, r.PathValue("app_id"), "invalid developer application id")
	if !ok {
		return
	}
	item, err := h.service.SubmitApplication(userID, workspaceID, appID)
	h.write(w, item, err, http.StatusOK)
}

func (h *DeveloperPlatformHandler) Review(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := developerScope(w, r)
	if !ok {
		return
	}
	appID, ok := developerPathID(w, r.PathValue("app_id"), "invalid developer application id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.ReviewDeveloperApplicationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.ReviewApplication(userID, workspaceID, appID, req)
	h.write(w, item, err, http.StatusOK)
}

func (h *DeveloperPlatformHandler) Credentials(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := developerScope(w, r)
	if !ok {
		return
	}
	appID, ok := developerPathID(w, r.PathValue("app_id"), "invalid developer application id")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Credentials(userID, workspaceID, appID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateDeveloperCredentialRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateCredential(userID, workspaceID, appID, req)
		h.write(w, item, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *DeveloperPlatformHandler) CredentialByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, appID, credentialID, ok := developerCredentialScope(w, r)
	if !ok {
		return
	}
	if err := h.service.RevokeCredential(userID, workspaceID, appID, credentialID); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "developer credential revoked"})
}

func (h *DeveloperPlatformHandler) RotateCredential(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, appID, credentialID, ok := developerCredentialScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.RotateCredential(userID, workspaceID, appID, credentialID)
	h.write(w, item, err, http.StatusCreated)
}

func (h *DeveloperPlatformHandler) Analytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := developerScope(w, r)
	if !ok {
		return
	}
	appID, ok := developerPathID(w, r.PathValue("app_id"), "invalid developer application id")
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
	item, err := h.service.Analytics(userID, workspaceID, appID, days)
	h.write(w, item, err, http.StatusOK)
}

func (h *DeveloperPlatformHandler) WebhookTests(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := developerScope(w, r)
	if !ok {
		return
	}
	appID, ok := developerPathID(w, r.PathValue("app_id"), "invalid developer application id")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.WebhookTests(userID, workspaceID, appID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.DeveloperWebhookTestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.TestWebhook(r.Context(), userID, workspaceID, appID, req)
		h.write(w, item, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *DeveloperPlatformHandler) DocsSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if _, ok := middleware.UserIDFromContext(r.Context()); !ok {
		unauthorized(w)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: h.service.SearchDocs(r.URL.Query().Get("q"))})
}

func (h *DeveloperPlatformHandler) SDKs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	if _, ok := middleware.UserIDFromContext(r.Context()); !ok {
		unauthorized(w)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: h.service.SDKs()})
}

func (h *DeveloperPlatformHandler) SandboxEcho(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.AuthClaimsFromContext(r.Context())
	if !ok || claims.ClientID == "" || claims.WorkspaceID <= 0 {
		unauthorized(w)
		return
	}
	contract, err := h.service.SandboxContext(claims.ClientID, claims.WorkspaceID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: contract})
	case http.MethodPost:
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			badJSON(w)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: map[string]any{"contract": contract, "echo": payload}})
	default:
		methodNotAllowed(w)
	}
}

func developerScope(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
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

func developerCredentialScope(w http.ResponseWriter, r *http.Request) (int64, int64, int64, int64, bool) {
	userID, workspaceID, ok := developerScope(w, r)
	if !ok {
		return 0, 0, 0, 0, false
	}
	appID, ok := developerPathID(w, r.PathValue("app_id"), "invalid developer application id")
	if !ok {
		return 0, 0, 0, 0, false
	}
	credentialID, ok := developerPathID(w, r.PathValue("credential_id"), "invalid developer credential id")
	if !ok {
		return 0, 0, 0, 0, false
	}
	return userID, workspaceID, appID, credentialID, true
}

func developerPathID(w http.ResponseWriter, raw, message string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: message})
		return 0, false
	}
	return value, true
}

func (h *DeveloperPlatformHandler) write(w http.ResponseWriter, data any, err error, status int) {
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, status, response.Envelope{Success: true, Data: data})
}

func (h *DeveloperPlatformHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrDeveloperPlatformForbidden):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidDeveloperApplication),
		errors.Is(err, service.ErrInvalidDeveloperLifecycle),
		errors.Is(err, service.ErrInvalidDeveloperCredential),
		errors.Is(err, service.ErrInvalidDeveloperWebhookTest):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrDeveloperAppApprovalRequired),
		errors.Is(err, service.ErrDeveloperSandboxDisabled),
		errors.Is(err, service.ErrDeveloperSandboxOnly),
		errors.Is(err, service.ErrServiceAccountsBlocked):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrDeveloperAppNotFound),
		errors.Is(err, repository.ErrDeveloperCredentialNotFound),
		errors.Is(err, repository.ErrOAuthClientNotFound),
		errors.Is(err, repository.ErrAPIKeyNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrDeveloperQuotaExceeded):
		response.JSON(w, http.StatusTooManyRequests, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
