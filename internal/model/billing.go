package model

import (
	"encoding/json"
	"time"
)

const (
	BillingSubscriptionActive   = "active"
	BillingSubscriptionTrialing = "trialing"
	BillingSubscriptionPastDue  = "past_due"
	BillingSubscriptionGrace    = "grace"
	BillingSubscriptionCanceled = "canceled"

	BillingInvoiceDraft         = "draft"
	BillingInvoiceOpen          = "open"
	BillingInvoicePaid          = "paid"
	BillingInvoiceVoid          = "void"
	BillingInvoiceUncollectible = "uncollectible"
)

type BillingPlan struct {
	ID                   int64           `json:"id"`
	Code                 string          `json:"code"`
	Name                 string          `json:"name"`
	Currency             string          `json:"currency"`
	MonthlyPriceCents    int64           `json:"monthly_price_cents"`
	MaxMembers           int             `json:"max_members"`
	MaxWorkspaces        int             `json:"max_workspaces"`
	MonthlyAPIOperations int64           `json:"monthly_api_operations"`
	Features             map[string]bool `json:"features"`
	Active               bool            `json:"active"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

type BillingSubscription struct {
	ID                     int64      `json:"id"`
	OrganizationID         int64      `json:"organization_id"`
	PlanID                 int64      `json:"plan_id"`
	PlanCode               string     `json:"plan_code"`
	Status                 string     `json:"status"`
	Provider               string     `json:"provider"`
	ProviderCustomerID     string     `json:"provider_customer_id,omitempty"`
	ProviderSubscriptionID string     `json:"provider_subscription_id,omitempty"`
	CurrentPeriodStart     time.Time  `json:"current_period_start"`
	CurrentPeriodEnd       time.Time  `json:"current_period_end"`
	GraceUntil             *time.Time `json:"grace_until,omitempty"`
	CancelAtPeriodEnd      bool       `json:"cancel_at_period_end"`
	CanceledAt             *time.Time `json:"canceled_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type BillingUsage struct {
	OrganizationID int64     `json:"organization_id"`
	Metric         string    `json:"metric"`
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
	Quantity       int64     `json:"quantity"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type BillingInvoice struct {
	ID               int64      `json:"id"`
	OrganizationID   int64      `json:"organization_id"`
	SubscriptionID   *int64     `json:"subscription_id,omitempty"`
	Provider         string     `json:"provider"`
	ExternalID       string     `json:"external_id"`
	Status           string     `json:"status"`
	Currency         string     `json:"currency"`
	AmountDueCents   int64      `json:"amount_due_cents"`
	AmountPaidCents  int64      `json:"amount_paid_cents"`
	PeriodStart      time.Time  `json:"period_start"`
	PeriodEnd        time.Time  `json:"period_end"`
	DueAt            *time.Time `json:"due_at,omitempty"`
	PaidAt           *time.Time `json:"paid_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type BillingWebhookEvent struct {
	ID        int64           `json:"id"`
	Provider  string          `json:"provider"`
	EventID   string          `json:"event_id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type BillingWebhookPayload struct {
	Type                   string `json:"type"`
	OrganizationID         int64  `json:"organization_id,omitempty"`
	ProviderSubscriptionID string `json:"provider_subscription_id,omitempty"`
	InvoiceExternalID      string `json:"invoice_external_id,omitempty"`
	Status                 string `json:"status,omitempty"`
	AmountPaidCents        int64  `json:"amount_paid_cents,omitempty"`
}

type BillingLimits struct {
	PlanCode             string          `json:"plan_code"`
	MaxMembers           int             `json:"max_members"`
	MaxWorkspaces        int             `json:"max_workspaces"`
	MonthlyAPIOperations int64           `json:"monthly_api_operations"`
	Features             map[string]bool `json:"features"`
}

type BillingDashboard struct {
	Plan               BillingPlan          `json:"plan"`
	Subscription       *BillingSubscription `json:"subscription,omitempty"`
	Limits             BillingLimits        `json:"limits"`
	MemberUsage        int                  `json:"member_usage"`
	WorkspaceUsage     int                  `json:"workspace_usage"`
	Usage              []BillingUsage       `json:"usage"`
	RecentInvoices     []BillingInvoice     `json:"recent_invoices"`
	MemberRemaining    int                  `json:"member_remaining"`
	WorkspaceRemaining int                  `json:"workspace_remaining"`
	GeneratedAt        time.Time            `json:"generated_at"`
}

type ChangeBillingSubscriptionRequest struct {
	PlanCode          string `json:"plan_code"`
	Provider          string `json:"provider,omitempty"`
	ProviderCustomerID string `json:"provider_customer_id,omitempty"`
	ProviderSubscriptionID string `json:"provider_subscription_id,omitempty"`
}

type CancelBillingSubscriptionRequest struct {
	Immediate bool `json:"immediate"`
}

type RecordBillingUsageRequest struct {
	Metric   string `json:"metric"`
	Quantity int64  `json:"quantity"`
}
