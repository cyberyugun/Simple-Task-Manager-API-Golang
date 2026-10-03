package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrBillingForbidden        = errors.New("billing action is forbidden")
	ErrBillingInvalidRequest   = errors.New("invalid billing request")
	ErrBillingPlanLimit        = errors.New("current organization usage exceeds target plan limits")
	ErrBillingWebhookInvalid   = errors.New("invalid billing webhook event")
	ErrBillingUsageMetric      = errors.New("unsupported billing usage metric")
	ErrBillingSubscriptionGone = errors.New("billing subscription is canceled")
)

type BillingService struct {
	repo     repository.BillingRepository
	orgs     repository.OrganizationRepository
	notifier NotificationEmitter
}

func NewBillingService(repo repository.BillingRepository, orgs repository.OrganizationRepository) *BillingService {
	return &BillingService{repo: repo, orgs: orgs}
}

func (s *BillingService) SetNotificationEmitter(notifier NotificationEmitter) {
	s.notifier = notifier
}

func (s *BillingService) ListPlans() ([]model.BillingPlan, error) {
	return s.repo.ListBillingPlans()
}

func (s *BillingService) GetSubscription(actorUserID, organizationID int64) (*model.BillingSubscription, error) {
	if _, err := s.requireBillingAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	item, err := s.repo.GetBillingSubscription(organizationID)
	if errors.Is(err, repository.ErrBillingSubscriptionNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *BillingService) ChangeSubscription(actorUserID, organizationID int64, req model.ChangeBillingSubscriptionRequest) (model.BillingSubscription, error) {
	if _, err := s.requireOwner(actorUserID, organizationID); err != nil {
		return model.BillingSubscription{}, err
	}
	planCode := strings.ToLower(strings.TrimSpace(req.PlanCode))
	if planCode == "" {
		return model.BillingSubscription{}, ErrBillingInvalidRequest
	}
	plan, err := s.repo.FindBillingPlanByCode(planCode)
	if err != nil {
		return model.BillingSubscription{}, err
	}
	memberCount, err := s.orgs.CountMembers(organizationID)
	if err != nil {
		return model.BillingSubscription{}, err
	}
	workspaceCount, err := s.orgs.CountWorkspaces(organizationID)
	if err != nil {
		return model.BillingSubscription{}, err
	}
	if memberCount > plan.MaxMembers || workspaceCount > plan.MaxWorkspaces {
		return model.BillingSubscription{}, ErrBillingPlanLimit
	}

	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		provider = "internal"
	}
	if len(provider) > 64 || len(req.ProviderCustomerID) > 255 || len(req.ProviderSubscriptionID) > 255 {
		return model.BillingSubscription{}, ErrBillingInvalidRequest
	}

	existing, existingErr := s.repo.GetBillingSubscription(organizationID)
	if existingErr == nil && existing.PlanID == plan.ID && existing.Status != model.BillingSubscriptionCanceled {
		return existing, nil
	}
	if existingErr != nil && !errors.Is(existingErr, repository.ErrBillingSubscriptionNotFound) {
		return model.BillingSubscription{}, existingErr
	}

	now := time.Now().UTC()
	periodEnd := now.Add(30 * 24 * time.Hour)
	subscription := model.BillingSubscription{
		OrganizationID:         organizationID,
		PlanID:                 plan.ID,
		PlanCode:               plan.Code,
		Status:                 model.BillingSubscriptionActive,
		Provider:               provider,
		ProviderCustomerID:     strings.TrimSpace(req.ProviderCustomerID),
		ProviderSubscriptionID: strings.TrimSpace(req.ProviderSubscriptionID),
		CurrentPeriodStart:     now,
		CurrentPeriodEnd:       periodEnd,
		CancelAtPeriodEnd:      false,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if existingErr == nil {
		subscription.ID = existing.ID
		subscription.CreatedAt = existing.CreatedAt
	}
	created, err := s.repo.UpsertBillingSubscription(subscription)
	if err != nil {
		return model.BillingSubscription{}, err
	}

	externalID, err := newBillingExternalID("inv")
	if err != nil {
		return model.BillingSubscription{}, err
	}
	status := model.BillingInvoiceOpen
	amountPaid := int64(0)
	var paidAt *time.Time
	dueAt := now.Add(7 * 24 * time.Hour)
	if plan.MonthlyPriceCents == 0 {
		status = model.BillingInvoicePaid
		paidAt = &now
		dueAt = now
	}
	subscriptionID := created.ID
	_, err = s.repo.CreateBillingInvoice(model.BillingInvoice{
		OrganizationID:  organizationID,
		SubscriptionID:  &subscriptionID,
		Provider:        provider,
		ExternalID:      externalID,
		Status:          status,
		Currency:        plan.Currency,
		AmountDueCents:  plan.MonthlyPriceCents,
		AmountPaidCents: amountPaid,
		PeriodStart:     now,
		PeriodEnd:       periodEnd,
		DueAt:           &dueAt,
		PaidAt:          paidAt,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		return model.BillingSubscription{}, err
	}
	s.audit(organizationID, actorUserID, "billing.subscription.changed", "billing_subscription", fmt.Sprint(created.ID), map[string]any{
		"plan_code": plan.Code, "provider": provider,
	})
	return created, nil
}

func (s *BillingService) CancelSubscription(actorUserID, organizationID int64, req model.CancelBillingSubscriptionRequest) (model.BillingSubscription, error) {
	if _, err := s.requireOwner(actorUserID, organizationID); err != nil {
		return model.BillingSubscription{}, err
	}
	item, err := s.repo.GetBillingSubscription(organizationID)
	if err != nil {
		return model.BillingSubscription{}, err
	}
	now := time.Now().UTC()
	if req.Immediate {
		item.Status = model.BillingSubscriptionCanceled
		item.CancelAtPeriodEnd = false
		item.CanceledAt = &now
	} else {
		item.CancelAtPeriodEnd = true
	}
	item.UpdatedAt = now
	updated, err := s.repo.UpsertBillingSubscription(item)
	if err == nil {
		s.audit(organizationID, actorUserID, "billing.subscription.cancel_requested", "billing_subscription", fmt.Sprint(item.ID), map[string]any{
			"immediate": req.Immediate,
		})
	}
	return updated, err
}

func (s *BillingService) EffectiveLimits(organizationID int64, at time.Time) (model.BillingLimits, error) {
	plan, _, err := s.effectivePlan(organizationID, at)
	if err != nil {
		return model.BillingLimits{}, err
	}
	return limitsFromBillingPlan(plan), nil
}

func (s *BillingService) Entitlements(actorUserID, organizationID int64) (model.BillingLimits, error) {
	if _, err := s.requireBillingAdmin(actorUserID, organizationID); err != nil {
		return model.BillingLimits{}, err
	}
	return s.EffectiveLimits(organizationID, time.Now().UTC())
}

func (s *BillingService) RecordUsage(actorUserID, organizationID int64, req model.RecordBillingUsageRequest) (model.BillingUsage, error) {
	if _, err := s.requireBillingAdmin(actorUserID, organizationID); err != nil {
		return model.BillingUsage{}, err
	}
	metric := strings.ToLower(strings.TrimSpace(req.Metric))
	if !supportedBillingMetric(metric) {
		return model.BillingUsage{}, ErrBillingUsageMetric
	}
	if req.Quantity <= 0 || req.Quantity > 1000000000000 {
		return model.BillingUsage{}, ErrBillingInvalidRequest
	}
	now := time.Now().UTC()
	start, end := billingMonth(now)
	item, err := s.repo.RecordBillingUsage(organizationID, metric, start, end, req.Quantity, now)
	if err == nil {
		s.audit(organizationID, actorUserID, "billing.usage.recorded", "billing_usage", metric, map[string]any{
			"quantity": req.Quantity, "period_start": start, "period_end": end,
		})
	}
	return item, err
}

func (s *BillingService) Usage(actorUserID, organizationID int64) ([]model.BillingUsage, error) {
	if _, err := s.requireBillingAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	start, end := billingMonth(now)
	return s.repo.ListBillingUsage(organizationID, start, end)
}

func (s *BillingService) Invoices(actorUserID, organizationID int64) ([]model.BillingInvoice, error) {
	if _, err := s.requireBillingAdmin(actorUserID, organizationID); err != nil {
		return nil, err
	}
	return s.repo.ListBillingInvoices(organizationID, 100)
}

func (s *BillingService) Dashboard(actorUserID, organizationID int64) (model.BillingDashboard, error) {
	if _, err := s.requireBillingAdmin(actorUserID, organizationID); err != nil {
		return model.BillingDashboard{}, err
	}
	now := time.Now().UTC()
	plan, subscription, err := s.effectivePlan(organizationID, now)
	if err != nil {
		return model.BillingDashboard{}, err
	}
	limits := limitsFromBillingPlan(plan)
	memberCount, err := s.orgs.CountMembers(organizationID)
	if err != nil {
		return model.BillingDashboard{}, err
	}
	workspaceCount, err := s.orgs.CountWorkspaces(organizationID)
	if err != nil {
		return model.BillingDashboard{}, err
	}
	start, end := billingMonth(now)
	usage, err := s.repo.ListBillingUsage(organizationID, start, end)
	if err != nil {
		return model.BillingDashboard{}, err
	}
	invoices, err := s.repo.ListBillingInvoices(organizationID, 10)
	if err != nil {
		return model.BillingDashboard{}, err
	}
	return model.BillingDashboard{
		Plan:               plan,
		Subscription:       subscription,
		Limits:             limits,
		MemberUsage:        memberCount,
		WorkspaceUsage:     workspaceCount,
		Usage:              usage,
		RecentInvoices:     invoices,
		MemberRemaining:    nonNegative(limits.MaxMembers - memberCount),
		WorkspaceRemaining: nonNegative(limits.MaxWorkspaces - workspaceCount),
		GeneratedAt:        now,
	}, nil
}

func (s *BillingService) ProcessWebhook(provider, eventID string, payload model.BillingWebhookPayload, raw json.RawMessage) (bool, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	eventID = strings.TrimSpace(eventID)
	payload.Type = strings.ToLower(strings.TrimSpace(payload.Type))
	if provider == "" || eventID == "" || payload.Type == "" || len(provider) > 64 || len(eventID) > 255 {
		return false, ErrBillingWebhookInvalid
	}
	now := time.Now().UTC()
	created, err := s.repo.RecordBillingWebhookEvent(model.BillingWebhookEvent{
		Provider: provider, EventID: eventID, EventType: payload.Type,
		Payload: append(json.RawMessage(nil), raw...), CreatedAt: now,
	})
	if err != nil || !created {
		return created, err
	}

	switch payload.Type {
	case "subscription.active", "subscription.trialing":
		if payload.ProviderSubscriptionID == "" {
			return true, ErrBillingWebhookInvalid
		}
		status := model.BillingSubscriptionActive
		if payload.Type == "subscription.trialing" {
			status = model.BillingSubscriptionTrialing
		}
		_, err = s.repo.UpdateBillingSubscriptionStatus(provider, payload.ProviderSubscriptionID, status, nil, now)
	case "subscription.past_due":
		if payload.ProviderSubscriptionID == "" {
			return true, ErrBillingWebhookInvalid
		}
		graceUntil := now.Add(7 * 24 * time.Hour)
		var subscription model.BillingSubscription
		subscription, err = s.repo.UpdateBillingSubscriptionStatus(provider, payload.ProviderSubscriptionID, model.BillingSubscriptionPastDue, &graceUntil, now)
		if err == nil && s.notifier != nil {
			_ = s.notifier.EmitOrganizationAdminsSignal(subscription.OrganizationID, model.NotificationEventBilling,
				"Subscription payment is past due",
				"Your subscription entered past-due status and is in a grace period.",
				"billing:past_due:"+eventID,
				map[string]any{"subscription_id": subscription.ID, "grace_until": graceUntil, "provider": provider})
		}
	case "subscription.canceled":
		if payload.ProviderSubscriptionID == "" {
			return true, ErrBillingWebhookInvalid
		}
		var subscription model.BillingSubscription
		subscription, err = s.repo.UpdateBillingSubscriptionStatus(provider, payload.ProviderSubscriptionID, model.BillingSubscriptionCanceled, nil, now)
		if err == nil && s.notifier != nil {
			_ = s.notifier.EmitOrganizationAdminsSignal(subscription.OrganizationID, model.NotificationEventBilling,
				"Subscription canceled", "Your organization subscription was canceled.",
				"billing:canceled:"+eventID,
				map[string]any{"subscription_id": subscription.ID, "provider": provider})
		}
	case "invoice.paid":
		if payload.InvoiceExternalID == "" || payload.AmountPaidCents < 0 {
			return true, ErrBillingWebhookInvalid
		}
		var invoice model.BillingInvoice
		invoice, err = s.repo.MarkBillingInvoicePaid(provider, payload.InvoiceExternalID, payload.AmountPaidCents, now, now)
		if err == nil && s.notifier != nil {
			_ = s.notifier.EmitOrganizationAdminsSignal(invoice.OrganizationID, model.NotificationEventBilling,
				"Invoice paid", "A billing invoice was paid successfully.",
				"billing:invoice_paid:"+eventID,
				map[string]any{"invoice_id": invoice.ID, "amount_paid_cents": invoice.AmountPaidCents, "provider": provider})
		}
	}
	return true, err
}

func (s *BillingService) effectivePlan(organizationID int64, at time.Time) (model.BillingPlan, *model.BillingSubscription, error) {
	subscription, err := s.repo.GetBillingSubscription(organizationID)
	if errors.Is(err, repository.ErrBillingSubscriptionNotFound) {
		plan, planErr := s.repo.FindBillingPlanByCode("free")
		return plan, nil, planErr
	}
	if err != nil {
		return model.BillingPlan{}, nil, err
	}
	useSubscription := false
	switch subscription.Status {
	case model.BillingSubscriptionActive, model.BillingSubscriptionTrialing:
		useSubscription = true
	case model.BillingSubscriptionPastDue, model.BillingSubscriptionGrace:
		useSubscription = subscription.GraceUntil != nil && at.Before(*subscription.GraceUntil)
	}
	if subscription.CancelAtPeriodEnd && !at.Before(subscription.CurrentPeriodEnd) {
		useSubscription = false
	}
	if useSubscription {
		plan, planErr := s.repo.FindBillingPlanByCode(subscription.PlanCode)
		if planErr != nil {
			return model.BillingPlan{}, nil, planErr
		}
		return plan, &subscription, nil
	}
	plan, planErr := s.repo.FindBillingPlanByCode("free")
	if planErr != nil {
		return model.BillingPlan{}, nil, planErr
	}
	return plan, &subscription, nil
}

func (s *BillingService) requireOwner(userID, organizationID int64) (model.Organization, error) {
	org, err := s.orgs.GetOrganization(organizationID)
	if err != nil {
		return model.Organization{}, err
	}
	if org.OwnerUserID != userID {
		return model.Organization{}, ErrBillingForbidden
	}
	return org, nil
}

func (s *BillingService) requireBillingAdmin(userID, organizationID int64) (model.OrganizationMember, error) {
	member, err := s.orgs.GetMember(organizationID, userID)
	if err != nil {
		return model.OrganizationMember{}, ErrBillingForbidden
	}
	switch member.Role {
	case model.OrganizationRoleOwner, model.OrganizationRoleAdmin, model.OrganizationRoleDelegatedAdmin:
		return member, nil
	default:
		return model.OrganizationMember{}, ErrBillingForbidden
	}
}

func (s *BillingService) audit(organizationID, actorUserID int64, action, resourceType, resourceID string, metadata map[string]any) {
	actor := actorUserID
	_ = s.orgs.RecordAudit(model.OrganizationAuditEvent{
		OrganizationID: organizationID,
		ActorUserID:    &actor,
		Action:         action,
		ResourceType:   resourceType,
		ResourceID:     resourceID,
		Metadata:       metadata,
		CreatedAt:      time.Now().UTC(),
	})
}

func limitsFromBillingPlan(plan model.BillingPlan) model.BillingLimits {
	features := make(map[string]bool, len(plan.Features))
	for key, value := range plan.Features {
		features[key] = value
	}
	return model.BillingLimits{
		PlanCode:             plan.Code,
		MaxMembers:           plan.MaxMembers,
		MaxWorkspaces:        plan.MaxWorkspaces,
		MonthlyAPIOperations: plan.MonthlyAPIOperations,
		Features:             features,
	}
}

func billingMonth(at time.Time) (time.Time, time.Time) {
	at = at.UTC()
	start := time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}

func supportedBillingMetric(metric string) bool {
	switch metric {
	case "api_operations", "automation_runs", "storage_bytes":
		return true
	default:
		return false
	}
}

func newBillingExternalID(prefix string) (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(raw), nil
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
