package service

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func webhookTestAccess(role string) model.WorkspaceAccess {
	return model.WorkspaceAccess{
		Workspace: model.Workspace{ID: 10},
		Role:      role,
	}
}

func TestWebhookServiceCreateAndPermissions(t *testing.T) {
	events := repository.NewInMemoryEventRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	service := NewWebhookService(events, workspaces, "12345678901234567890123456789012")

	created, err := service.Create(1, webhookTestAccess(model.WorkspaceRoleOwner), model.CreateWebhookSubscriptionRequest{
		URL:        "https://hooks.example.com/tasks",
		EventTypes: []string{model.EventTaskDeleted, model.EventTaskCreated, model.EventTaskCreated},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.SigningSecret == "" || len(created.EventTypes) != 2 || created.EventTypes[0] != model.EventTaskCreated {
		t.Fatalf("unexpected subscription: %+v", created)
	}

	list, err := service.List(webhookTestAccess(model.WorkspaceRoleAdmin))
	if err != nil || len(list) != 1 || list[0].SigningSecret != "" {
		t.Fatalf("List() = %+v, %v", list, err)
	}

	if _, err := service.List(webhookTestAccess(model.WorkspaceRoleMember)); !errors.Is(err, ErrWorkspaceForbidden) {
		t.Fatalf("member List() error = %v, want ErrWorkspaceForbidden", err)
	}
}

func TestWebhookServiceValidation(t *testing.T) {
	events := repository.NewInMemoryEventRepository()
	service := NewWebhookService(events, repository.NewInMemoryWorkspaceRepository(), "12345678901234567890123456789012")
	owner := webhookTestAccess(model.WorkspaceRoleOwner)

	if _, err := service.Create(1, owner, model.CreateWebhookSubscriptionRequest{
		URL: "http://example.com/hook",
	}); !errors.Is(err, ErrInvalidWebhookURL) {
		t.Fatalf("insecure public URL error = %v, want ErrInvalidWebhookURL", err)
	}

	if _, err := service.Create(1, owner, model.CreateWebhookSubscriptionRequest{
		URL:        "https://example.com/hook",
		EventTypes: []string{"unknown.event"},
	}); !errors.Is(err, ErrInvalidWebhookEvent) {
		t.Fatalf("unknown event error = %v, want ErrInvalidWebhookEvent", err)
	}

	disabled := NewWebhookService(events, repository.NewInMemoryWorkspaceRepository(), "")
	if _, err := disabled.Create(1, owner, model.CreateWebhookSubscriptionRequest{
		URL: "https://example.com/hook",
	}); !errors.Is(err, ErrWebhookDisabled) {
		t.Fatalf("disabled webhook error = %v, want ErrWebhookDisabled", err)
	}

	loopback, err := normalizeWebhookURL("http://127.0.0.1:8081/hook#fragment")
	if err != nil || loopback != "http://127.0.0.1:8081/hook" {
		t.Fatalf("loopback normalization = %q, %v", loopback, err)
	}

	_ = time.Now()
}
