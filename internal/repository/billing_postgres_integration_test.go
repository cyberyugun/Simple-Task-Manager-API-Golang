//go:build integration

package repository_test

import (
	"encoding/json"
	"testing"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestIntegrationPostgresBillingRepository(t *testing.T) {
	databaseURL := getenvRequired(t, "TEST_DATABASE_URL")
	db, err := appdb.OpenPostgres(databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgres() error = %v", err)
	}
	if err := resetDatabase(db); err != nil {
		_ = db.Close()
		t.Fatalf("reset database: %v", err)
	}
	t.Cleanup(func() {
		if err := resetDatabase(db); err != nil {
			t.Errorf("cleanup database: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	applyMigrations(t, db)

	users := repository.NewPostgresUserRepository(db)
	orgs := repository.NewPostgresOrganizationRepository(db)
	billing := repository.NewPostgresBillingRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	owner, err := users.Create(model.User{
		Name: "Billing Owner", Email: "billing-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Billing Org", Status: model.OrganizationStatusActive, OwnerUserID: owner.ID,
		MaxMembers: 1000, MaxWorkspaces: 100, CreatedByUserID: owner.ID,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	plans, err := billing.ListBillingPlans()
	if err != nil || len(plans) < 3 {
		t.Fatalf("plans=%+v err=%v", plans, err)
	}
	pro, err := billing.FindBillingPlanByCode("pro")
	if err != nil {
		t.Fatal(err)
	}

	sub, err := billing.UpsertBillingSubscription(model.BillingSubscription{
		OrganizationID: org.ID, PlanID: pro.ID, PlanCode: pro.Code,
		Status: model.BillingSubscriptionActive, Provider: "stripe",
		ProviderCustomerID: "cus_integration", ProviderSubscriptionID: "sub_integration",
		CurrentPeriodStart: now, CurrentPeriodEnd: now.Add(30 * 24 * time.Hour),
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.PlanCode != "pro" {
		t.Fatalf("subscription=%+v", sub)
	}

	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	if _, err := billing.RecordBillingUsage(org.ID, "api_operations", start, end, 20, now); err != nil {
		t.Fatal(err)
	}
	usage, err := billing.RecordBillingUsage(org.ID, "api_operations", start, end, 5, now.Add(time.Minute))
	if err != nil || usage.Quantity != 25 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}

	subscriptionID := sub.ID
	invoice, err := billing.CreateBillingInvoice(model.BillingInvoice{
		OrganizationID: org.ID, SubscriptionID: &subscriptionID, Provider: "stripe",
		ExternalID: "inv_integration", Status: model.BillingInvoiceOpen, Currency: "USD",
		AmountDueCents: 4900, PeriodStart: start, PeriodEnd: end,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	paid, err := billing.MarkBillingInvoicePaid("stripe", invoice.ExternalID, 4900, now.Add(time.Hour), now.Add(time.Hour))
	if err != nil || paid.Status != model.BillingInvoicePaid || paid.AmountPaidCents != 4900 {
		t.Fatalf("paid=%+v err=%v", paid, err)
	}

	payload, _ := json.Marshal(model.BillingWebhookPayload{Type: "subscription.past_due", ProviderSubscriptionID: "sub_integration"})
	created, err := billing.RecordBillingWebhookEvent(model.BillingWebhookEvent{
		Provider: "stripe", EventID: "evt_integration", EventType: "subscription.past_due",
		Payload: payload, CreatedAt: now,
	})
	if err != nil || !created {
		t.Fatalf("webhook created=%v err=%v", created, err)
	}
	created, err = billing.RecordBillingWebhookEvent(model.BillingWebhookEvent{
		Provider: "stripe", EventID: "evt_integration", EventType: "subscription.past_due",
		Payload: payload, CreatedAt: now,
	})
	if err != nil || created {
		t.Fatalf("duplicate webhook created=%v err=%v", created, err)
	}

	graceUntil := now.Add(7 * 24 * time.Hour)
	updated, err := billing.UpdateBillingSubscriptionStatus(
		"stripe", "sub_integration", model.BillingSubscriptionPastDue, &graceUntil, now.Add(time.Minute),
	)
	if err != nil || updated.Status != model.BillingSubscriptionPastDue || updated.GraceUntil == nil {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
}
