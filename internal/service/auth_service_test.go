package service

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func newTestAuthService() *AuthService {
	users := repository.NewInMemoryUserRepository()
	tokens := auth.NewTokenManager("12345678901234567890123456789012", time.Hour)
	return NewAuthService(users, tokens)
}

func TestAuthServiceRegisterAndLogin(t *testing.T) {
	service := newTestAuthService()

	registered, err := service.Register(model.RegisterRequest{
		Name:     "Test User",
		Email:    "TEST@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if registered.User.Email != "test@example.com" || registered.AccessToken == "" {
		t.Fatalf("Register() returned unexpected result: %+v", registered)
	}
	if registered.User.PasswordHash != "" {
		t.Fatal("password hash should not be exposed through JSON-facing user data")
	}

	loggedIn, err := service.Login(model.LoginRequest{
		Email:    "test@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if loggedIn.AccessToken == "" {
		t.Fatal("Login() access token is empty")
	}
}

func TestAuthServiceRejectsDuplicateEmailAndBadPassword(t *testing.T) {
	service := newTestAuthService()
	req := model.RegisterRequest{Name: "Test User", Email: "test@example.com", Password: "password123"}

	if _, err := service.Register(req); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := service.Register(req); !errors.Is(err, repository.ErrEmailExists) {
		t.Fatalf("duplicate Register() error = %v, want ErrEmailExists", err)
	}
	if _, err := service.Login(model.LoginRequest{Email: req.Email, Password: "wrong-password"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
	}
}
