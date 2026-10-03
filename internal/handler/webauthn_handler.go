package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"go-simple-task-api/internal/middleware"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

const webAuthnSessionHeader = "X-WebAuthn-Session"

type WebAuthnHandler struct {
	service *service.WebAuthnService
	auth    *service.AuthService
}

func NewWebAuthnHandler(service *service.WebAuthnService, authService *service.AuthService) *WebAuthnHandler {
	return &WebAuthnHandler{service: service, auth: authService}
}

func (h *WebAuthnHandler) RegistrationBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	result, err := h.service.BeginRegistration(userID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: result})
}

func (h *WebAuthnHandler) RegistrationFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	sessionID := strings.TrimSpace(r.Header.Get(webAuthnSessionHeader))
	credentialID, err := h.service.FinishRegistration(userID, sessionID, r)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{
		Success: true,
		Data:    map[string]string{"credential_id": credentialID},
	})
}

func (h *WebAuthnHandler) LoginBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	defer r.Body.Close()
	var req model.WebAuthnPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	user, err := h.auth.ValidatePassword(req.Email, req.Password)
	if err != nil {
		h.handleError(w, err)
		return
	}
	result, err := h.service.BeginLogin(user.ID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: result})
}

func (h *WebAuthnHandler) LoginFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	sessionID := strings.TrimSpace(r.Header.Get(webAuthnSessionHeader))
	userID, err := h.service.FinishLogin(sessionID, r)
	if err != nil {
		h.handleError(w, err)
		return
	}
	result, err := h.auth.IssueMFASession(userID, sessionMetadata(r))
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: result})
}

func (h *WebAuthnHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidCredentials):
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "invalid email or password"})
	case errors.Is(err, service.ErrWebAuthnUnavailable):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrWebAuthnCeremony):
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "webauthn verification failed"})
	}
}
