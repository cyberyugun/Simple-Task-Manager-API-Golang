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

type EnterpriseIdentityHandler struct {
	service *service.EnterpriseIdentityService
}

func NewEnterpriseIdentityHandler(service *service.EnterpriseIdentityService) *EnterpriseIdentityHandler {
	return &EnterpriseIdentityHandler{service: service}
}

func (h *EnterpriseIdentityHandler) OAuthToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	defer r.Body.Close()
	var req model.OAuthTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	result, err := h.service.ExchangeToken(req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: result})
}

func (h *EnterpriseIdentityHandler) APIKeyExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	defer r.Body.Close()
	var req struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	result, err := h.service.ExchangeAPIKey(req.APIKey)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: result})
}

func (h *EnterpriseIdentityHandler) OAuthClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.CreateOAuthClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	result, err := h.service.CreateOAuthClient(userID, workspaceID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: result})
}

func (h *EnterpriseIdentityHandler) OAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.OAuthAuthorizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	result, err := h.service.AuthorizeCode(userID, workspaceID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: result})
}

func (h *EnterpriseIdentityHandler) APIKeys(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.ListAPIKeys(userID, workspaceID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateAPIKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		result, err := h.service.CreateAPIKey(userID, workspaceID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: result})
	default:
		methodNotAllowed(w)
	}
}

func (h *EnterpriseIdentityHandler) APIKeyByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	keyID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("key_id")), 10, 64)
	if err != nil || keyID <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid api key id"})
		return
	}
	if err := h.service.RevokeAPIKey(userID, workspaceID, keyID); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "api key revoked"})
}

func (h *EnterpriseIdentityHandler) Policy(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		policy, err := h.service.GetPolicy(userID, workspaceID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: policy})
	case http.MethodPut:
		defer r.Body.Close()
		var req model.UpdateEnterprisePolicyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		policy, err := h.service.UpdatePolicy(userID, workspaceID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: policy})
	default:
		methodNotAllowed(w)
	}
}

func (h *EnterpriseIdentityHandler) OIDC(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		connection, err := h.service.GetOIDC(userID, workspaceID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: connection})
	case http.MethodPut:
		defer r.Body.Close()
		var req model.ConfigureOIDCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		connection, err := h.service.ConfigureOIDC(userID, workspaceID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: connection})
	default:
		methodNotAllowed(w)
	}
}

func (h *EnterpriseIdentityHandler) SCIMUsers(w http.ResponseWriter, r *http.Request) {
	userID, workspaceID, ok := identityScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		users, err := h.service.ListSCIMUsers(userID, workspaceID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: users})
	case http.MethodPost, http.MethodPut:
		defer r.Body.Close()
		var req model.SCIMUpsertUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		user, err := h.service.UpsertSCIMUser(userID, workspaceID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: user})
	default:
		methodNotAllowed(w)
	}
}

func (h *EnterpriseIdentityHandler) Introspect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if _, ok := middleware.UserIDFromContext(r.Context()); !ok {
		unauthorized(w)
		return
	}
	defer r.Body.Close()
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	result, err := h.service.Introspect(req.Token)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: result})
}

func (h *EnterpriseIdentityHandler) RevokeToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	defer r.Body.Close()
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	if err := h.service.RevokeAccessToken(userID, req.Token); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "access token revoked"})
}

func identityScope(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
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

func (h *EnterpriseIdentityHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrEnterpriseAccessDenied):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidOAuthSecret):
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidOAuthClient),
		errors.Is(err, service.ErrInvalidOAuthRequest),
		errors.Is(err, service.ErrInvalidScope),
		errors.Is(err, service.ErrInvalidEnterprisePolicy),
		errors.Is(err, service.ErrInvalidOIDCConnection),
		errors.Is(err, service.ErrInvalidSCIMUser):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrServiceAccountsBlocked):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrAPIKeyNotFound),
		errors.Is(err, repository.ErrOAuthClientNotFound),
		errors.Is(err, repository.ErrOIDCConnection),
		errors.Is(err, repository.ErrSCIMUserNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
