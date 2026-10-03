package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

type BillingHandler struct {
	service       *service.BillingService
	webhookSecret []byte
}

func NewBillingHandler(service *service.BillingService, webhookSecret string) *BillingHandler {
	return &BillingHandler{service: service, webhookSecret: []byte(webhookSecret)}
}

func (h *BillingHandler) Plans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	items, err := h.service.ListPlans()
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *BillingHandler) Subscription(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := h.service.GetSubscription(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	case http.MethodPut:
		defer r.Body.Close()
		var req model.ChangeBillingSubscriptionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.ChangeSubscription(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *BillingHandler) CancelSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	defer r.Body.Close()
	var req model.CancelBillingSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badJSON(w)
		return
	}
	item, err := h.service.CancelSubscription(userID, organizationID, req)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *BillingHandler) Entitlements(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	item, err := h.service.Entitlements(userID, organizationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item})
}

func (h *BillingHandler) Usage(w http.ResponseWriter, r *http.Request) {
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.Usage(userID, organizationID)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
	case http.MethodPost:
		defer r.Body.Close()
		var req model.RecordBillingUsageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.RecordUsage(userID, organizationID, req)
		if err != nil {
			h.handleError(w, err)
			return
		}
		response.JSON(w, http.StatusCreated, response.Envelope{Success: true, Data: item})
	default:
		methodNotAllowed(w)
	}
}

func (h *BillingHandler) Invoices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, organizationID, ok := organizationScope(w, r)
	if !ok {
		return
	}
	items, err := h.service.Invoices(userID, organizationID)
	if err != nil {
		h.handleError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: items})
}

func (h *BillingHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
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

func (h *BillingHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if len(h.webhookSecret) < 32 {
		response.JSON(w, http.StatusServiceUnavailable, response.Envelope{Success: false, Message: "billing webhook is not configured"})
		return
	}
	provider := strings.ToLower(strings.TrimSpace(r.PathValue("provider")))
	eventID := strings.TrimSpace(r.Header.Get("X-Billing-Event-ID"))
	signature := strings.TrimSpace(r.Header.Get("X-Billing-Signature"))
	if provider == "" || eventID == "" || signature == "" {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "missing billing webhook metadata"})
		return
	}
	defer r.Body.Close()
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "invalid billing webhook body"})
		return
	}
	supplied, err := hex.DecodeString(signature)
	if err != nil {
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "invalid billing webhook signature"})
		return
	}
	mac := hmac.New(sha256.New, h.webhookSecret)
	_, _ = mac.Write(raw)
	if !hmac.Equal(supplied, mac.Sum(nil)) {
		response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "invalid billing webhook signature"})
		return
	}
	var payload model.BillingWebhookPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		badJSON(w)
		return
	}
	created, err := h.service.ProcessWebhook(provider, eventID, payload, raw)
	if err != nil {
		h.handleError(w, err)
		return
	}
	message := "billing webhook processed"
	if !created {
		message = "billing webhook already processed"
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Message: message})
}

func (h *BillingHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrBillingPlanNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrBillingSubscriptionNotFound),
		errors.Is(err, repository.ErrBillingInvoiceNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrBillingForbidden):
		response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrBillingPlanLimit):
		response.JSON(w, http.StatusConflict, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, service.ErrBillingInvalidRequest),
		errors.Is(err, service.ErrBillingWebhookInvalid),
		errors.Is(err, service.ErrBillingUsageMetric):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrOrganizationNotFound),
		errors.Is(err, repository.ErrOrganizationMemberNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
