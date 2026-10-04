package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

type GlobalRegionHandler struct{ service *service.GlobalRegionService }

func NewGlobalRegionHandler(service *service.GlobalRegionService) *GlobalRegionHandler {
	return &GlobalRegionHandler{service: service}
}

func (h *GlobalRegionHandler) Regions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: h.service.Regions()})
}
func (h *GlobalRegionHandler) Policy(w http.ResponseWriter, r *http.Request) {
	userID, orgID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := h.service.Policy(userID, orgID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	case http.MethodPut:
		defer r.Body.Close()
		var req model.UpdateOrganizationRegionPolicyRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			badJSON(w)
			return
		}
		item, err := h.service.UpdatePolicy(userID, orgID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}
func (h *GlobalRegionHandler) Placements(w http.ResponseWriter, r *http.Request) {
	userID, orgID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Placements(userID, orgID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.UpsertRegionalPlacementRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			badJSON(w)
			return
		}
		item, err := h.service.UpsertPlacement(userID, orgID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}
func (h *GlobalRegionHandler) Migrations(w http.ResponseWriter, r *http.Request) {
	userID, orgID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Migrations(userID, orgID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateRegionMigrationRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateMigration(userID, orgID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}
func (h *GlobalRegionHandler) MigrationDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, orgID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	id, ok := pathInt64(w, r.PathValue("migration_id"), "invalid region migration id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.DecideRegionMigrationRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		badJSON(w)
		return
	}
	item, err := h.service.DecideMigration(userID, orgID, id, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}
func (h *GlobalRegionHandler) MigrationComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, orgID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	id, ok := pathInt64(w, r.PathValue("migration_id"), "invalid region migration id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.CompleteRegionMigrationRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		badJSON(w)
		return
	}
	item, err := h.service.CompleteMigration(userID, orgID, id, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}
func (h *GlobalRegionHandler) Transfers(w http.ResponseWriter, r *http.Request) {
	userID, orgID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Transfers(userID, orgID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateCrossRegionTransferRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateTransfer(userID, orgID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}
func (h *GlobalRegionHandler) TransferDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, orgID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	id, ok := pathInt64(w, r.PathValue("transfer_id"), "invalid cross-region transfer id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.DecideCrossRegionTransferRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		badJSON(w)
		return
	}
	item, err := h.service.DecideTransfer(userID, orgID, id, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}
func (h *GlobalRegionHandler) TransferComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, orgID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	id, ok := pathInt64(w, r.PathValue("transfer_id"), "invalid cross-region transfer id")
	if !ok {
		return
	}
	item, err := h.service.CompleteTransfer(userID, orgID, id)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}
func (h *GlobalRegionHandler) FailoverExercises(w http.ResponseWriter, r *http.Request) {
	userID, orgID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.FailoverExercises(userID, orgID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateFailoverExerciseRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateFailoverExercise(userID, orgID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}
func (h *GlobalRegionHandler) FailoverComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, orgID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	id, ok := pathInt64(w, r.PathValue("exercise_id"), "invalid failover exercise id")
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.CompleteFailoverExerciseRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		badJSON(w)
		return
	}
	item, err := h.service.CompleteFailoverExercise(userID, orgID, id, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}
func (h *GlobalRegionHandler) Route(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, orgID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.Route(userID, orgID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}
func (h *GlobalRegionHandler) Report(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, orgID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.Report(userID, orgID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}
func (h *GlobalRegionHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrRegionForbidden):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidRegionPolicy), errors.Is(err, service.ErrInvalidRegionalPlacement), errors.Is(err, service.ErrInvalidRegionMigration), errors.Is(err, service.ErrInvalidCrossRegionTransfer), errors.Is(err, service.ErrInvalidFailoverExercise):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrRegionResidencyViolation), errors.Is(err, service.ErrRegionDecisionConflict):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrRegionMigrationNotFound), errors.Is(err, repository.ErrCrossRegionTransferNotFound), errors.Is(err, repository.ErrFailoverExerciseNotFound), errors.Is(err, repository.ErrOrganizationNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
