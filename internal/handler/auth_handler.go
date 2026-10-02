package handler

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"go-simple-task-api/internal/middleware"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

type AuthHandler struct {
	service          *service.AuthService
	exposeAuthTokens bool
}

func NewAuthHandler(service *service.AuthService, exposeAuthTokens bool) *AuthHandler {
	return &AuthHandler{service: service, exposeAuthTokens: exposeAuthTokens}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	defer r.Body.Close()

	var req model.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}

	result, err := h.service.Register(req, sessionMetadata(r))
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: result})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	defer r.Body.Close()

	var req model.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}

	result, err := h.service.Login(req, sessionMetadata(r))
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: result})
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	defer r.Body.Close()

	var req model.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}

	result, err := h.service.Refresh(req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: result})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	defer r.Body.Close()

	var req model.LogoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}

	if err := h.service.Logout(req); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "logged out"})
}

func (h *AuthHandler) Sessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}

	sessions, err := h.service.Sessions(userID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: sessions})
}

func (h *AuthHandler) SessionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}

	rawID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/auth/sessions/"), "/")
	sessionID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || sessionID <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid session id"})
		return
	}

	if err := h.service.RevokeSession(userID, sessionID); err != nil {
		if errors.Is(err, service.ErrInvalidRefreshToken) {
			response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "session not found"})
			return
		}
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "session revoked"})
}

func (h *AuthHandler) LogoutAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}

	if err := h.service.LogoutAll(userID); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "all refresh sessions revoked"})
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
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

	var req model.ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}

	if err := h.service.ChangePassword(userID, req); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "password changed; refresh sessions revoked"})
}

func (h *AuthHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	defer r.Body.Close()

	var req model.ForgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}

	result, err := h.service.ForgotPassword(req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	h.hideDevelopmentToken(&result)
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: result})
}

func (h *AuthHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	defer r.Body.Close()

	var req model.ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}

	if err := h.service.ResetPassword(req); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "password reset successfully"})
}

func (h *AuthHandler) RequestEmailVerification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}

	result, err := h.service.RequestEmailVerification(userID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	h.hideDevelopmentToken(&result)
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: result})
}

func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	defer r.Body.Close()

	var req model.VerifyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}

	if err := h.service.VerifyEmail(req); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "email verified"})
}

func (h *AuthHandler) hideDevelopmentToken(result *model.ActionTokenResult) {
	if !h.exposeAuthTokens {
		result.DevelopmentToken = ""
	}
}

func (h *AuthHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidUser),
		errors.Is(err, service.ErrInvalidNewPassword):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrEmailExists):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidCredentials),
		errors.Is(err, service.ErrInvalidRefreshToken),
		errors.Is(err, service.ErrInvalidCurrentPassword),
		errors.Is(err, service.ErrInvalidActionToken):
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrUserNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "user not found"})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}

func sessionMetadata(r *http.Request) model.SessionMetadata {
	return model.SessionMetadata{
		UserAgent: r.UserAgent(),
		IPAddress: requestIP(r),
	}
}

func requestIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func badJSON(w http.ResponseWriter) {
	response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid JSON body"})
}

func methodNotAllowed(w http.ResponseWriter) {
	response.JSON(w, http.StatusMethodNotAllowed, response.Envelope{Success: false, Message: "method not allowed"})
}

func unauthorized(w http.ResponseWriter) {
	response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "authentication required"})
}
