package handler

import (
	"crypto/sha256"
	"encoding/hex"
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

type ZeroTrustHandler struct {
	service *service.ZeroTrustService
}

func NewZeroTrustHandler(service *service.ZeroTrustService) *ZeroTrustHandler {
	return &ZeroTrustHandler{service: service}
}

func (h *ZeroTrustHandler) Policy(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := h.service.Policy(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	case http.MethodPut:
		defer r.Body.Close()
		var req model.UpdateZeroTrustPolicyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.UpdatePolicy(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *ZeroTrustHandler) Workloads(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Workloads(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateWorkloadIdentityRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateWorkload(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *ZeroTrustHandler) WorkloadByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	workloadID, ok := pathInt64(w, r.PathValue("workload_id"), "invalid workload identity id")
	if !ok {
		return
	}
	if err := h.service.RevokeWorkload(userID, organizationID, workloadID); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "workload identity revoked"})
}

func (h *ZeroTrustHandler) WorkloadCertificates(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	workloadID, ok := pathInt64(w, r.PathValue("workload_id"), "invalid workload identity id")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Certificates(userID, organizationID, workloadID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.RegisterWorkloadCertificateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.RegisterCertificate(userID, organizationID, workloadID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *ZeroTrustHandler) WorkloadToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "mTLS client certificate is required"})
		return
	}
	sum := sha256.Sum256(r.TLS.PeerCertificates[0].Raw)
	result, err := h.service.ExchangeWorkloadToken(hex.EncodeToString(sum[:]))
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: result})
}

func (h *ZeroTrustHandler) Devices(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Devices(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.TrustDeviceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.TrustDevice(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *ZeroTrustHandler) DeviceByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	deviceID, ok := pathInt64(w, r.PathValue("device_id"), "invalid device trust id")
	if !ok {
		return
	}
	if err := h.service.RevokeDevice(userID, organizationID, deviceID); err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: "device trust revoked"})
}

func (h *ZeroTrustHandler) EvaluateRisk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.RiskEvaluationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	if strings.TrimSpace(req.SourceIP) == "" {
		req.SourceIP = requestIP(r)
	}
	claims, _ := middleware.AuthClaimsFromContext(r.Context())
	item, err := h.service.EvaluateRisk(userID, organizationID, claims.MFA, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *ZeroTrustHandler) Events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	afterID, err := queryInt64(r, "after_id", 0)
	if err != nil || afterID < 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "after_id must be zero or a positive integer"})
		return
	}
	limit, err := queryInt64(r, "limit", 100)
	if err != nil || limit < 1 || limit > 500 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "limit must be between 1 and 500"})
		return
	}
	items, err := h.service.Events(userID, organizationID, afterID, limit)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *ZeroTrustHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.Dashboard(userID, organizationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *ZeroTrustHandler) Checkpoints(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Checkpoints(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		item, err := h.service.CreateCheckpoint(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *ZeroTrustHandler) WORMExports(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.CreateWORMExport(userID, organizationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
}

func (h *ZeroTrustHandler) SIEM(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.SIEMDestinations(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateSIEMDestinationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateSIEMDestination(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *ZeroTrustHandler) SIEMFeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	afterID, err := queryInt64(r, "after_id", 0)
	if err != nil || afterID < 0 {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "after_id must be zero or a positive integer"})
		return
	}
	items, err := h.service.SIEMFeed(userID, organizationID, afterID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func queryInt64(r *http.Request, key string, defaultValue int64) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return defaultValue, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

func (h *ZeroTrustHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrZeroTrustForbidden), errors.Is(err, service.ErrZeroTrustMemberRequired):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidWorkloadCert):
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrInvalidZeroTrustPolicy),
		errors.Is(err, service.ErrInvalidWorkloadIdentity),
		errors.Is(err, service.ErrInvalidDeviceTrust),
		errors.Is(err, service.ErrInvalidRiskEvaluation),
		errors.Is(err, service.ErrInvalidSIEMDestination):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrNoSecurityEvents), errors.Is(err, service.ErrSIEMFederationDisabled):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrWorkloadIdentityNotFound),
		errors.Is(err, repository.ErrWorkloadCertificateNotFound),
		errors.Is(err, repository.ErrDeviceTrustNotFound),
		errors.Is(err, repository.ErrAuditCheckpointNotFound),
		errors.Is(err, repository.ErrSIEMDestinationNotFound),
		errors.Is(err, repository.ErrOrganizationNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
