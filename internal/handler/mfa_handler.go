package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"go-simple-task-api/internal/middleware"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

type MFAHandler struct {
	service *service.MFAService
}

func NewMFAHandler(service *service.MFAService) *MFAHandler {
	return &MFAHandler{service: service}
}

func (h *MFAHandler) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	status, err := h.service.Status(userID)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "mfa status failed"})
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: status})
}

func (h *MFAHandler) EnrollTOTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	result, err := h.service.BeginTOTP(userID)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "mfa enrollment failed"})
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: result})
}

func (h *MFAHandler) ConfirmTOTP(w http.ResponseWriter, r *http.Request) {
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
	var req model.TOTPCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	if err := h.service.ConfirmTOTP(userID, req.Code); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "totp mfa enabled"})
}

func (h *MFAHandler) DisableTOTP(w http.ResponseWriter, r *http.Request) {
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
	var req model.TOTPCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	if err := h.service.DisableTOTP(userID, req.Code); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "totp mfa disabled"})
}

func (h *MFAHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrMFAInvalidCode):
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrMFANotEnrolled):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "mfa operation failed"})
	}
}
