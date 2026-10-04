package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

type AttachmentHandler struct {
	service *service.AttachmentService
}

func NewAttachmentHandler(service *service.AttachmentService) *AttachmentHandler {
	return &AttachmentHandler{service: service}
}

func (h *AttachmentHandler) Uploads(w http.ResponseWriter, r *http.Request) {
	if !requireTaskScope(w, r) {
		return
	}
	userID, access, ok := requestScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodPost:
		defer r.Body.Close()
		var req model.AttachmentUploadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateUpload(userID, access, req)
		h.write(w, item, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *AttachmentHandler) Attachments(w http.ResponseWriter, r *http.Request) {
	if !requireTaskScope(w, r) {
		return
	}
	_, access, ok := requestScope(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	taskID, err := optionalAttachmentQueryID(r, "task_id")
	if err != nil {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
		return
	}
	commentID, err := optionalAttachmentQueryID(r, "comment_id")
	if err != nil {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
		return
	}
	items, err := h.service.List(access, taskID, commentID)
	h.write(w, items, err, http.StatusOK)
}

func (h *AttachmentHandler) AttachmentByID(w http.ResponseWriter, r *http.Request) {
	if !requireTaskScope(w, r) {
		return
	}
	userID, access, ok := requestScope(w, r)
	if !ok {
		return
	}
	attachmentID, ok := attachmentPathID(w, r.PathValue("attachment_id"))
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodDelete:
		item, err := h.service.Delete(userID, access, attachmentID)
		h.write(w, item, err, http.StatusOK)
	default:
		methodNotAllowed(w)
	}
}

func (h *AttachmentHandler) Complete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !requireTaskScope(w, r) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
		}
		return
	}
	userID, access, ok := requestScope(w, r)
	if !ok {
		return
	}
	attachmentID, ok := attachmentPathID(w, r.PathValue("attachment_id"))
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.CompleteAttachmentUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.CompleteUpload(userID, access, attachmentID, req)
	h.write(w, item, err, http.StatusOK)
}

func (h *AttachmentHandler) Download(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !requireTaskScope(w, r) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
		}
		return
	}
	userID, access, ok := requestScope(w, r)
	if !ok {
		return
	}
	attachmentID, ok := attachmentPathID(w, r.PathValue("attachment_id"))
	if !ok {
		return
	}
	item, err := h.service.Download(userID, access, attachmentID)
	h.write(w, item, err, http.StatusOK)
}

func (h *AttachmentHandler) Governance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch || !requireTaskScope(w, r) {
		if r.Method != http.MethodPatch {
			methodNotAllowed(w)
		}
		return
	}
	userID, access, ok := requestScope(w, r)
	if !ok {
		return
	}
	attachmentID, ok := attachmentPathID(w, r.PathValue("attachment_id"))
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.UpdateAttachmentGovernanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.Governance(userID, access, attachmentID, req)
	h.write(w, item, err, http.StatusOK)
}

func (h *AttachmentHandler) Usage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || !requireTaskScope(w, r) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
		}
		return
	}
	_, access, ok := requestScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.Usage(access)
	h.write(w, item, err, http.StatusOK)
}

func (h *AttachmentHandler) write(w http.ResponseWriter, data any, err error, successStatus int) {
	if err == nil {
		response.JSON(w, successStatus, response.Envelope{Success: true, Data: data})
		return
	}
	switch {
	case errors.Is(err, service.ErrInvalidAttachment):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrAttachmentQuotaExceeded),
		errors.Is(err, service.ErrAttachmentNotReady),
		errors.Is(err, service.ErrAttachmentBlocked),
		errors.Is(err, service.ErrAttachmentGovernanceDenied):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrAttachmentNotFound),
		errors.Is(err, repository.ErrTaskNotFound),
		errors.Is(err, repository.ErrTaskCommentNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}

func attachmentPathID(w http.ResponseWriter, value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid attachment id"})
		return 0, false
	}
	return id, true
}

func optionalAttachmentQueryID(r *http.Request, key string) (*int64, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return nil, nil
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return nil, errors.New("invalid " + key)
	}
	return &id, nil
}
