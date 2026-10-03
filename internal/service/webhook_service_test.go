package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestWebhookServiceRBACSecretAndEventValidation(t *testing.T) {
	workspaces := repository.NewInMemoryWorkspaceRepository()
	eventsRepo := repository.NewInMemoryEventRepository()
	now := time.Now()
	ownerWorkspace, err := workspaces.Create(1, "Team", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.AddMember(ownerWorkspace.ID, 2, model.WorkspaceRoleAdmin, now); err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.AddMember(ownerWorkspace.ID, 3, model.WorkspaceRoleMember, now); err != nil {
		t.Fatal(err)
	}

	svc := NewWebhookService(workspaces, eventsRepo, false)
	created, err := svc.Create(1, ownerWorkspace.ID, model.CreateWebhookSubscriptionRequest{
		URL:        "https://hooks.example.com/task",
		EventTypes: []string{model.EventTaskUpdated, model.EventTaskCreated, model.EventTaskCreated},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !strings.HasPrefix(created.SigningSecret, "whsec_") || len(created.SigningSecret) < 40 {
		t.Fatalf("unexpected signing secret: %q", created.SigningSecret)
	}
	if len(created.EventTypes) != 2 || created.EventTypes[0] != model.EventTaskCreated {
		t.Fatalf("event types were not normalized: %+v", created.EventTypes)
	}

	listed, err := svc.List(2, ownerWorkspace.ID)
	if err != nil {
		t.Fatalf("admin List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].SigningSecret != "" {
		t.Fatalf("list leaked signing secret: %+v", listed)
	}

	if _, err := svc.Create(3, ownerWorkspace.ID, model.CreateWebhookSubscriptionRequest{
		URL: "https://hooks.example.com/member", EventTypes: []string{"*"},
	}); !errors.Is(err, ErrWorkspaceForbidden) {
		t.Fatalf("member Create() error = %v, want ErrWorkspaceForbidden", err)
	}
	if _, err := svc.Create(1, ownerWorkspace.ID, model.CreateWebhookSubscriptionRequest{
		URL: "http://127.0.0.1:8081/hook", EventTypes: []string{"*"},
	}); !errors.Is(err, ErrInvalidWebhookURL) {
		t.Fatalf("insecure Create() error = %v, want ErrInvalidWebhookURL", err)
	}
	if _, err := svc.Create(1, ownerWorkspace.ID, model.CreateWebhookSubscriptionRequest{
		URL: "https://hooks.example.com/task", EventTypes: []string{"unknown.event"},
	}); !errors.Is(err, ErrInvalidWebhookEvents) {
		t.Fatalf("invalid event Create() error = %v, want ErrInvalidWebhookEvents", err)
	}

	if err := svc.Delete(2, ownerWorkspace.ID, created.ID); err != nil {
		t.Fatalf("admin Delete() error = %v", err)
	}
}
