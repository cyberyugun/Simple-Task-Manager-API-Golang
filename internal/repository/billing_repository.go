package repository

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrBillingPlanNotFound         = errors.New("billing plan not found")
	ErrBillingSubscriptionNotFound = errors.New("billing subscription not found")
	ErrBillingInvoiceNotFound      = errors.New("billing invoice not found")
)

type BillingRepository interface {
	ListBillingPlans() ([]model.BillingPlan, error)
	FindBillingPlanByCode(code string) (model.BillingPlan, error)

	GetBillingSubscription(organizationID int64) (model.BillingSubscription, error)
	UpsertBillingSubscription(subscription model.BillingSubscription) (model.BillingSubscription, error)
	UpdateBillingSubscriptionStatus(provider, providerSubscriptionID, status string, graceUntil *time.Time, now time.Time) (model.BillingSubscription, error)

	RecordBillingUsage(organizationID int64, metric string, periodStart, periodEnd time.Time, delta int64, now time.Time) (model.BillingUsage, error)
	ListBillingUsage(organizationID int64, periodStart, periodEnd time.Time) ([]model.BillingUsage, error)

	CreateBillingInvoice(invoice model.BillingInvoice) (model.BillingInvoice, error)
	ListBillingInvoices(organizationID int64, limit int) ([]model.BillingInvoice, error)
	MarkBillingInvoicePaid(provider, externalID string, amountPaid int64, paidAt, now time.Time) (model.BillingInvoice, error)

	RecordBillingWebhookEvent(event model.BillingWebhookEvent) (bool, error)
}

type InMemoryBillingRepository struct {
	mu            sync.Mutex
	plans         map[string]model.BillingPlan
	subscriptions map[int64]model.BillingSubscription
	usage         map[string]model.BillingUsage
	invoices      map[string]model.BillingInvoice
	webhooks      map[string]model.BillingWebhookEvent
	nextSubID     int64
	nextInvoiceID int64
	nextWebhookID int64
}

func NewInMemoryBillingRepository() *InMemoryBillingRepository {
	now := time.Now().UTC()
	return &InMemoryBillingRepository{
		plans: map[string]model.BillingPlan{
			"free": {
				ID: 1, Code: "free", Name: "Free", Currency: "USD", MonthlyPriceCents: 0,
				MaxMembers: 100, MaxWorkspaces: 20, MonthlyAPIOperations: 10000,
				Features: map[string]bool{"advanced_governance": false, "sso": false, "audit_export": false, "priority_support": false},
				Active: true, CreatedAt: now, UpdatedAt: now,
			},
			"pro": {
				ID: 2, Code: "pro", Name: "Pro", Currency: "USD", MonthlyPriceCents: 4900,
				MaxMembers: 1000, MaxWorkspaces: 100, MonthlyAPIOperations: 1000000,
				Features: map[string]bool{"advanced_governance": true, "sso": true, "audit_export": true, "priority_support": false},
				Active: true, CreatedAt: now, UpdatedAt: now,
			},
			"enterprise": {
				ID: 3, Code: "enterprise", Name: "Enterprise", Currency: "USD", MonthlyPriceCents: 24900,
				MaxMembers: 100000, MaxWorkspaces: 10000, MonthlyAPIOperations: 1000000000,
				Features: map[string]bool{"advanced_governance": true, "sso": true, "audit_export": true, "priority_support": true},
				Active: true, CreatedAt: now, UpdatedAt: now,
			},
		},
		subscriptions: make(map[int64]model.BillingSubscription),
		usage:         make(map[string]model.BillingUsage),
		invoices:      make(map[string]model.BillingInvoice),
		webhooks:      make(map[string]model.BillingWebhookEvent),
		nextSubID:     1,
		nextInvoiceID: 1,
		nextWebhookID: 1,
	}
}

func (r *InMemoryBillingRepository) ListBillingPlans() ([]model.BillingPlan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.BillingPlan, 0, len(r.plans))
	for _, plan := range r.plans {
		items = append(items, cloneBillingPlan(plan))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemoryBillingRepository) FindBillingPlanByCode(code string) (model.BillingPlan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	plan, ok := r.plans[strings.ToLower(strings.TrimSpace(code))]
	if !ok || !plan.Active {
		return model.BillingPlan{}, ErrBillingPlanNotFound
	}
	return cloneBillingPlan(plan), nil
}

func (r *InMemoryBillingRepository) GetBillingSubscription(organizationID int64) (model.BillingSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.subscriptions[organizationID]
	if !ok {
		return model.BillingSubscription{}, ErrBillingSubscriptionNotFound
	}
	return item, nil
}

func (r *InMemoryBillingRepository) UpsertBillingSubscription(subscription model.BillingSubscription) (model.BillingSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.subscriptions[subscription.OrganizationID]; ok {
		subscription.ID = existing.ID
		subscription.CreatedAt = existing.CreatedAt
	} else {
		subscription.ID = r.nextSubID
		r.nextSubID++
	}
	r.subscriptions[subscription.OrganizationID] = subscription
	return subscription, nil
}

func (r *InMemoryBillingRepository) UpdateBillingSubscriptionStatus(provider, providerSubscriptionID, status string, graceUntil *time.Time, now time.Time) (model.BillingSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for orgID, item := range r.subscriptions {
		if item.Provider == provider && item.ProviderSubscriptionID == providerSubscriptionID && providerSubscriptionID != "" {
			item.Status = status
			item.GraceUntil = graceUntil
			item.UpdatedAt = now
			r.subscriptions[orgID] = item
			return item, nil
		}
	}
	return model.BillingSubscription{}, ErrBillingSubscriptionNotFound
}

func (r *InMemoryBillingRepository) RecordBillingUsage(organizationID int64, metric string, periodStart, periodEnd time.Time, delta int64, now time.Time) (model.BillingUsage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := billingUsageKey(organizationID, metric, periodStart, periodEnd)
	item := r.usage[key]
	item.OrganizationID = organizationID
	item.Metric = metric
	item.PeriodStart = periodStart
	item.PeriodEnd = periodEnd
	item.Quantity += delta
	item.UpdatedAt = now
	r.usage[key] = item
	return item, nil
}

func (r *InMemoryBillingRepository) ListBillingUsage(organizationID int64, periodStart, periodEnd time.Time) ([]model.BillingUsage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.BillingUsage, 0)
	for _, item := range r.usage {
		if item.OrganizationID == organizationID && item.PeriodStart.Equal(periodStart) && item.PeriodEnd.Equal(periodEnd) {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Metric < items[j].Metric })
	return items, nil
}

func (r *InMemoryBillingRepository) CreateBillingInvoice(invoice model.BillingInvoice) (model.BillingInvoice, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := billingExternalKey(invoice.Provider, invoice.ExternalID)
	if _, exists := r.invoices[key]; exists {
		return model.BillingInvoice{}, fmt.Errorf("billing invoice already exists")
	}
	invoice.ID = r.nextInvoiceID
	r.nextInvoiceID++
	r.invoices[key] = invoice
	return invoice, nil
}

func (r *InMemoryBillingRepository) ListBillingInvoices(organizationID int64, limit int) ([]model.BillingInvoice, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]model.BillingInvoice, 0)
	for _, item := range r.invoices {
		if item.OrganizationID == organizationID {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r *InMemoryBillingRepository) MarkBillingInvoicePaid(provider, externalID string, amountPaid int64, paidAt, now time.Time) (model.BillingInvoice, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := billingExternalKey(provider, externalID)
	item, ok := r.invoices[key]
	if !ok {
		return model.BillingInvoice{}, ErrBillingInvoiceNotFound
	}
	item.Status = model.BillingInvoicePaid
	item.AmountPaidCents = amountPaid
	item.PaidAt = &paidAt
	item.UpdatedAt = now
	r.invoices[key] = item
	return item, nil
}

func (r *InMemoryBillingRepository) RecordBillingWebhookEvent(event model.BillingWebhookEvent) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := billingExternalKey(event.Provider, event.EventID)
	if _, exists := r.webhooks[key]; exists {
		return false, nil
	}
	event.ID = r.nextWebhookID
	r.nextWebhookID++
	r.webhooks[key] = event
	return true, nil
}

func billingUsageKey(organizationID int64, metric string, periodStart, periodEnd time.Time) string {
	return fmt.Sprintf("%d|%s|%d|%d", organizationID, metric, periodStart.Unix(), periodEnd.Unix())
}

func billingExternalKey(provider, externalID string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + "|" + strings.TrimSpace(externalID)
}

func cloneBillingPlan(plan model.BillingPlan) model.BillingPlan {
	clone := plan
	clone.Features = make(map[string]bool, len(plan.Features))
	for key, value := range plan.Features {
		clone.Features[key] = value
	}
	return clone
}
