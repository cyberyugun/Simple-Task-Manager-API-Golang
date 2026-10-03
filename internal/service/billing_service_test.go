package service

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestBillingLifecycleEntitlementsUsageAndWebhook(t *testing.T) {
	orgs := repository.NewInMemoryOrganizationRepository()
	billing := repository.NewInMemoryBillingRepository()
	svc := NewBillingService(billing, orgs)
	now := time.Now().UTC()

	org, err := orgs.CreateOrganization(model.Organization{
		Name: "Acme", Status: model.OrganizationStatusActive, OwnerUserID: 10,
		MaxMembers: 1000, MaxWorkspaces: 100, CreatedByUserID: 10,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	free, err := svc.EffectiveLimits(org.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if free.PlanCode != "free" || free.MaxMembers != 100 || free.MaxWorkspaces != 20 {
		t.Fatalf("unexpected free limits: %+v", free)
	}

	sub, err := svc.ChangeSubscription(10, org.ID, model.ChangeBillingSubscriptionRequest{
		PlanCode: "pro", Provider: "stripe", ProviderCustomerID: "cus_123", ProviderSubscriptionID: "sub_123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.PlanCode != "pro" || sub.Status != model.BillingSubscriptionActive {
		t.Fatalf("unexpected subscription: %+v", sub)
	}

	limits, err := svc.EffectiveLimits(org.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if limits.PlanCode != "pro" || !limits.Features["sso"] {
		t.Fatalf("unexpected pro limits: %+v", limits)
	}

	usage, err := svc.RecordUsage(10, org.ID, model.RecordBillingUsageRequest{Metric: "api_operations", Quantity: 25})
	if err != nil {
		t.Fatal(err)
	}
	if usage.Quantity != 25 {
		t.Fatalf("usage=%d want=25", usage.Quantity)
	}

	invoices, err := svc.Invoices(10, org.ID)
	if err != nil || len(invoices) != 1 {
		t.Fatalf("invoices=%+v err=%v", invoices, err)
	}

	pastDue := model.BillingWebhookPayload{Type: "subscription.past_due", ProviderSubscriptionID: "sub_123"}
	raw, _ := json.Marshal(pastDue)
	created, err := svc.ProcessWebhook("stripe", "evt_1", pastDue, raw)
	if err != nil || !created {
		t.Fatalf("past due webhook created=%v err=%v", created, err)
	}
	created, err = svc.ProcessWebhook("stripe", "evt_1", pastDue, raw)
	if err != nil || created {
		t.Fatalf("duplicate webhook created=%v err=%v", created, err)
	}
	graceLimits, err := svc.EffectiveLimits(org.ID, time.Now().UTC())
	if err != nil || graceLimits.PlanCode != "pro" {
		t.Fatalf("grace limits=%+v err=%v", graceLimits, err)
	}

	paid := model.BillingWebhookPayload{
		Type: "invoice.paid", InvoiceExternalID: invoices[0].ExternalID, AmountPaidCents: invoices[0].AmountDueCents,
	}
	paidRaw, _ := json.Marshal(paid)
	if _, err := svc.ProcessWebhook("stripe", "evt_2", paid, paidRaw); err != nil {
		t.Fatal(err)
	}
	invoices, _ = svc.Invoices(10, org.ID)
	if invoices[0].Status != model.BillingInvoicePaid {
		t.Fatalf("invoice status=%s", invoices[0].Status)
	}

	if _, err := svc.CancelSubscription(10, org.ID, model.CancelBillingSubscriptionRequest{Immediate: true}); err != nil {
		t.Fatal(err)
	}
	fallback, err := svc.EffectiveLimits(org.ID, time.Now().UTC())
	if err != nil || fallback.PlanCode != "free" {
		t.Fatalf("fallback=%+v err=%v", fallback, err)
	}
}

func TestBillingDowngradeRejectsCurrentUsage(t *testing.T) {
	orgs := repository.NewInMemoryOrganizationRepository()
	billing := repository.NewInMemoryBillingRepository()
	svc := NewBillingService(billing, orgs)
	now := time.Now().UTC()
	org, _ := orgs.CreateOrganization(model.Organization{
		Name: "Large", Status: model.OrganizationStatusActive, OwnerUserID: 1,
		MaxMembers: 1000, MaxWorkspaces: 100, CreatedByUserID: 1, CreatedAt: now, UpdatedAt: now,
	})
	for userID := int64(2); userID <= 102; userID++ {
		_, _ = orgs.UpsertMember(model.OrganizationMember{
			OrganizationID: org.ID, UserID: userID, Role: model.OrganizationRoleMember,
			CreatedAt: now, UpdatedAt: now,
		})
	}
	if _, err := svc.ChangeSubscription(1, org.ID, model.ChangeBillingSubscriptionRequest{PlanCode: "free"}); !errors.Is(err, ErrBillingPlanLimit) {
		t.Fatalf("downgrade error=%v want ErrBillingPlanLimit", err)
	}
}
