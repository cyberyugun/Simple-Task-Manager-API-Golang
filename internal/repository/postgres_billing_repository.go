package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"go-simple-task-api/internal/model"
)

type PostgresBillingRepository struct {
	db *sql.DB
}

func NewPostgresBillingRepository(db *sql.DB) *PostgresBillingRepository {
	return &PostgresBillingRepository{db: db}
}

func (r *PostgresBillingRepository) ListBillingPlans() ([]model.BillingPlan, error) {
	rows, err := r.db.Query(`
		SELECT id, code, name, currency, monthly_price_cents, max_members, max_workspaces,
			monthly_api_operations, features, active, created_at, updated_at
		FROM billing_plans
		WHERE active = TRUE
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.BillingPlan, 0)
	for rows.Next() {
		item, err := scanBillingPlan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresBillingRepository) FindBillingPlanByCode(code string) (model.BillingPlan, error) {
	row := r.db.QueryRow(`
		SELECT id, code, name, currency, monthly_price_cents, max_members, max_workspaces,
			monthly_api_operations, features, active, created_at, updated_at
		FROM billing_plans
		WHERE code = $1 AND active = TRUE
	`, code)
	item, err := scanBillingPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.BillingPlan{}, ErrBillingPlanNotFound
	}
	return item, err
}

func (r *PostgresBillingRepository) GetBillingSubscription(organizationID int64) (model.BillingSubscription, error) {
	var item model.BillingSubscription
	err := r.db.QueryRow(`
		SELECT s.id, s.organization_id, s.plan_id, p.code, s.status, s.provider,
			s.provider_customer_id, s.provider_subscription_id,
			s.current_period_start, s.current_period_end, s.grace_until,
			s.cancel_at_period_end, s.canceled_at, s.created_at, s.updated_at
		FROM billing_subscriptions s
		JOIN billing_plans p ON p.id = s.plan_id
		WHERE s.organization_id = $1
	`, organizationID).Scan(
		&item.ID, &item.OrganizationID, &item.PlanID, &item.PlanCode, &item.Status, &item.Provider,
		&item.ProviderCustomerID, &item.ProviderSubscriptionID,
		&item.CurrentPeriodStart, &item.CurrentPeriodEnd, &item.GraceUntil,
		&item.CancelAtPeriodEnd, &item.CanceledAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.BillingSubscription{}, ErrBillingSubscriptionNotFound
	}
	return item, err
}

func (r *PostgresBillingRepository) UpsertBillingSubscription(subscription model.BillingSubscription) (model.BillingSubscription, error) {
	_, err := r.db.Exec(`
		INSERT INTO billing_subscriptions (
			organization_id, plan_id, status, provider, provider_customer_id, provider_subscription_id,
			current_period_start, current_period_end, grace_until, cancel_at_period_end,
			canceled_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (organization_id) DO UPDATE SET
			plan_id = EXCLUDED.plan_id,
			status = EXCLUDED.status,
			provider = EXCLUDED.provider,
			provider_customer_id = EXCLUDED.provider_customer_id,
			provider_subscription_id = EXCLUDED.provider_subscription_id,
			current_period_start = EXCLUDED.current_period_start,
			current_period_end = EXCLUDED.current_period_end,
			grace_until = EXCLUDED.grace_until,
			cancel_at_period_end = EXCLUDED.cancel_at_period_end,
			canceled_at = EXCLUDED.canceled_at,
			updated_at = EXCLUDED.updated_at
	`,
		subscription.OrganizationID, subscription.PlanID, subscription.Status, subscription.Provider,
		subscription.ProviderCustomerID, subscription.ProviderSubscriptionID,
		subscription.CurrentPeriodStart, subscription.CurrentPeriodEnd, subscription.GraceUntil,
		subscription.CancelAtPeriodEnd, subscription.CanceledAt, subscription.CreatedAt, subscription.UpdatedAt,
	)
	if err != nil {
		return model.BillingSubscription{}, err
	}
	return r.GetBillingSubscription(subscription.OrganizationID)
}

func (r *PostgresBillingRepository) UpdateBillingSubscriptionStatus(provider, providerSubscriptionID, status string, graceUntil *time.Time, now time.Time) (model.BillingSubscription, error) {
	var organizationID int64
	err := r.db.QueryRow(`
		UPDATE billing_subscriptions
		SET status = $3, grace_until = $4, updated_at = $5
		WHERE provider = $1 AND provider_subscription_id = $2 AND provider_subscription_id <> ''
		RETURNING organization_id
	`, provider, providerSubscriptionID, status, graceUntil, now).Scan(&organizationID)
	if errors.Is(err, sql.ErrNoRows) {
		return model.BillingSubscription{}, ErrBillingSubscriptionNotFound
	}
	if err != nil {
		return model.BillingSubscription{}, err
	}
	return r.GetBillingSubscription(organizationID)
}

func (r *PostgresBillingRepository) RecordBillingUsage(organizationID int64, metric string, periodStart, periodEnd time.Time, delta int64, now time.Time) (model.BillingUsage, error) {
	var item model.BillingUsage
	err := r.db.QueryRow(`
		INSERT INTO billing_usage (
			organization_id, metric, period_start, period_end, quantity, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (organization_id, metric, period_start, period_end) DO UPDATE SET
			quantity = billing_usage.quantity + EXCLUDED.quantity,
			updated_at = EXCLUDED.updated_at
		RETURNING organization_id, metric, period_start, period_end, quantity, updated_at
	`, organizationID, metric, periodStart, periodEnd, delta, now).Scan(
		&item.OrganizationID, &item.Metric, &item.PeriodStart, &item.PeriodEnd, &item.Quantity, &item.UpdatedAt,
	)
	return item, err
}

func (r *PostgresBillingRepository) ListBillingUsage(organizationID int64, periodStart, periodEnd time.Time) ([]model.BillingUsage, error) {
	rows, err := r.db.Query(`
		SELECT organization_id, metric, period_start, period_end, quantity, updated_at
		FROM billing_usage
		WHERE organization_id = $1 AND period_start = $2 AND period_end = $3
		ORDER BY metric
	`, organizationID, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.BillingUsage, 0)
	for rows.Next() {
		var item model.BillingUsage
		if err := rows.Scan(
			&item.OrganizationID, &item.Metric, &item.PeriodStart,
			&item.PeriodEnd, &item.Quantity, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresBillingRepository) CreateBillingInvoice(invoice model.BillingInvoice) (model.BillingInvoice, error) {
	var item model.BillingInvoice
	err := r.db.QueryRow(`
		INSERT INTO billing_invoices (
			organization_id, subscription_id, provider, external_id, status, currency,
			amount_due_cents, amount_paid_cents, period_start, period_end, due_at,
			paid_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id, organization_id, subscription_id, provider, external_id, status, currency,
			amount_due_cents, amount_paid_cents, period_start, period_end, due_at,
			paid_at, created_at, updated_at
	`,
		invoice.OrganizationID, invoice.SubscriptionID, invoice.Provider, invoice.ExternalID,
		invoice.Status, invoice.Currency, invoice.AmountDueCents, invoice.AmountPaidCents,
		invoice.PeriodStart, invoice.PeriodEnd, invoice.DueAt, invoice.PaidAt,
		invoice.CreatedAt, invoice.UpdatedAt,
	).Scan(
		&item.ID, &item.OrganizationID, &item.SubscriptionID, &item.Provider, &item.ExternalID,
		&item.Status, &item.Currency, &item.AmountDueCents, &item.AmountPaidCents,
		&item.PeriodStart, &item.PeriodEnd, &item.DueAt, &item.PaidAt, &item.CreatedAt, &item.UpdatedAt,
	)
	return item, err
}

func (r *PostgresBillingRepository) ListBillingInvoices(organizationID int64, limit int) ([]model.BillingInvoice, error) {
	rows, err := r.db.Query(`
		SELECT id, organization_id, subscription_id, provider, external_id, status, currency,
			amount_due_cents, amount_paid_cents, period_start, period_end, due_at,
			paid_at, created_at, updated_at
		FROM billing_invoices
		WHERE organization_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, organizationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.BillingInvoice, 0)
	for rows.Next() {
		var item model.BillingInvoice
		if err := rows.Scan(
			&item.ID, &item.OrganizationID, &item.SubscriptionID, &item.Provider, &item.ExternalID,
			&item.Status, &item.Currency, &item.AmountDueCents, &item.AmountPaidCents,
			&item.PeriodStart, &item.PeriodEnd, &item.DueAt, &item.PaidAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresBillingRepository) MarkBillingInvoicePaid(provider, externalID string, amountPaid int64, paidAt, now time.Time) (model.BillingInvoice, error) {
	var item model.BillingInvoice
	err := r.db.QueryRow(`
		UPDATE billing_invoices
		SET status = 'paid', amount_paid_cents = $3, paid_at = $4, updated_at = $5
		WHERE provider = $1 AND external_id = $2
		RETURNING id, organization_id, subscription_id, provider, external_id, status, currency,
			amount_due_cents, amount_paid_cents, period_start, period_end, due_at,
			paid_at, created_at, updated_at
	`, provider, externalID, amountPaid, paidAt, now).Scan(
		&item.ID, &item.OrganizationID, &item.SubscriptionID, &item.Provider, &item.ExternalID,
		&item.Status, &item.Currency, &item.AmountDueCents, &item.AmountPaidCents,
		&item.PeriodStart, &item.PeriodEnd, &item.DueAt, &item.PaidAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return model.BillingInvoice{}, ErrBillingInvoiceNotFound
	}
	return item, err
}

func (r *PostgresBillingRepository) RecordBillingWebhookEvent(event model.BillingWebhookEvent) (bool, error) {
	result, err := r.db.Exec(`
		INSERT INTO billing_webhook_events (provider, event_id, event_type, payload, created_at)
		VALUES ($1,$2,$3,$4::jsonb,$5)
		ON CONFLICT (provider, event_id) DO NOTHING
	`, event.Provider, event.EventID, event.EventType, string(event.Payload), event.CreatedAt)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return count == 1, nil
}

type billingPlanScanner interface {
	Scan(dest ...any) error
}

func scanBillingPlan(scanner billingPlanScanner) (model.BillingPlan, error) {
	var item model.BillingPlan
	var features []byte
	err := scanner.Scan(
		&item.ID, &item.Code, &item.Name, &item.Currency, &item.MonthlyPriceCents,
		&item.MaxMembers, &item.MaxWorkspaces, &item.MonthlyAPIOperations,
		&features, &item.Active, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.BillingPlan{}, err
	}
	item.Features = make(map[string]bool)
	if len(features) > 0 {
		if err := json.Unmarshal(features, &item.Features); err != nil {
			return model.BillingPlan{}, err
		}
	}
	return item, nil
}
