package service

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestWebAuthnBeginRegistrationAndRejectLoginWithoutCredential(t *testing.T) {
	users := repository.NewInMemoryUserRepository()
	user, err := users.Create(model.User{
		Name: "Passkey User", Email: "passkey@example.com", PasswordHash: "unused",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewInMemoryWebAuthnRepository()
	svc, err := NewWebAuthnService(
		repo,
		users,
		"localhost",
		[]string{"http://localhost:8080"},
		"Simple Task Manager",
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.BeginRegistration(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionID == "" || result.Options == nil {
		t.Fatalf("unexpected registration result: %+v", result)
	}

	if _, err := svc.BeginLogin(user.ID); !errors.Is(err, ErrWebAuthnUnavailable) {
		t.Fatalf("BeginLogin() error = %v, want ErrWebAuthnUnavailable", err)
	}
}
