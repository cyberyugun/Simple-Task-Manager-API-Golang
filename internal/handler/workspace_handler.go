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

type WorkspaceHandler struct {
	service *service.WorkspaceService
}

func NewWorkspaceHandler(service *service.WorkspaceService) *WorkspaceHandler {
	return &WorkspaceHandler{service: service}
}

func (h *WorkspaceHandler) Workspaces(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !requireScope(w, r, model.ScopeWorkspaceRead) {
			return
		}
		items, err := h.service.List(userID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		if !requireScope(w, r, model.ScopeWorkspaceAdmin) {
			return
		}
		defer r.Body.Close()
		var req model.CreateWorkspaceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.Create(userID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *WorkspaceHandler) WorkspaceByID(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}

	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/workspaces/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "workspace not found"})
		return
	}
	workspaceID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || workspaceID <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid workspace id"})
		return
	}

	if len(parts) == 1 {
		h.workspace(w, r, userID, workspaceID)
		return
	}
	switch parts[1] {
	case "members":
		h.members(w, r, userID, workspaceID, parts[2:])
	case "audit":
		if len(parts) != 2 || r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if !requireScope(w, r, model.ScopeAuditRead) {
			return
		}
		limit := 100
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 || parsed > 500 {
				response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "limit must be between 1 and 500"})
				return
			}
			limit = parsed
		}
		items, err := h.service.Audit(userID, workspaceID, limit)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	default:
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "route not found"})
	}
}

func (h *WorkspaceHandler) workspace(w http.ResponseWriter, r *http.Request, userID, workspaceID int64) {
	switch r.Method {
	case http.MethodGet:
		if !requireScope(w, r, model.ScopeWorkspaceRead) {
			return
		}
		item, err := h.service.Access(userID, workspaceID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	case http.MethodPut:
		if !requireScope(w, r, model.ScopeWorkspaceAdmin) {
			return
		}
		defer r.Body.Close()
		var req model.RenameWorkspaceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.Rename(userID, workspaceID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	case http.MethodDelete:
		if !requireScope(w, r, model.ScopeWorkspaceAdmin) {
			return
		}
		if err := h.service.Delete(userID, workspaceID); err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "workspace deleted"})
	default:
		methodNotAllowed(w)
	}
}

func (h *WorkspaceHandler) members(w http.ResponseWriter, r *http.Request, userID, workspaceID int64, rest []string) {
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			if !requireScope(w, r, model.ScopeWorkspaceRead) {
				return
			}
			items, err := h.service.ListMembers(userID, workspaceID)
			if err != nil {
				h.handleError(w, err)
				return
			}
			response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
		case http.MethodPost:
			if !requireScope(w, r, model.ScopeWorkspaceAdmin) {
				return
			}
			defer r.Body.Close()
			var req model.AddWorkspaceMemberRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				badJSON(w)
				return
			}
			item, err := h.service.AddMember(userID, workspaceID, req)
			if err != nil {
				h.handleError(w, err)
				return
			}
			response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
		default:
			methodNotAllowed(w)
		}
		return
	}
	if len(rest) != 1 {
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "route not found"})
		return
	}
	targetUserID, err := strconv.ParseInt(rest[0], 10, 64)
	if err != nil || targetUserID <= 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid member user id"})
		return
	}

	switch r.Method {
	case http.MethodPut:
		if !requireScope(w, r, model.ScopeWorkspaceAdmin) {
			return
		}
		defer r.Body.Close()
		var req model.UpdateWorkspaceMemberRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.UpdateMemberRole(userID, workspaceID, targetUserID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	case http.MethodDelete:
		if !requireScope(w, r, model.ScopeWorkspaceAdmin) {
			return
		}
		if err := h.service.RemoveMember(userID, workspaceID, targetUserID); err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "workspace member removed"})
	default:
		methodNotAllowed(w)
	}
}

func (h *WorkspaceHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrWorkspaceNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "workspace not found"})
	case errors.Is(err, repository.ErrUserNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "user not found"})
	case errors.Is(err, repository.ErrWorkspaceMemberNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "workspace member not found"})
	case errors.Is(err, repository.ErrWorkspaceMemberExists):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrWorkspaceForbidden), errors.Is(err, service.ErrOwnerMembership):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrWorkspaceLegalHold):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidWorkspaceName), errors.Is(err, service.ErrInvalidWorkspaceRole), errors.Is(err, service.ErrPersonalWorkspace):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
