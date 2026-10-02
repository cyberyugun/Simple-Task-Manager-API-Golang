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
	refreshes := repository.NewInMemoryRefreshTokenRepository()
	accessTTL := 15 * time.Minute
	refreshTTL := 24 * time.Hour
	tokens := auth.NewTokenManager("12345678901234567890123456789012", accessTTL)
	return NewAuthService(users, refreshes, tokens, accessTTL, refreshTTL)
}

func TestAuthServiceRegisterLoginRefreshAndLogout(t *testing.T) {
	service := newTestAuthService()

	registered, err := service.Register(model.RegisterRequest{
		Name:     "Test User",
		Email:    "TEST@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if registered.User.Email != "test@example.com" || registered.AccessToken == "" || registered.RefreshToken == "" {
		t.Fatalf("Register() returned unexpected result: %+v", registered)
	}
	if registered.AccessTokenExpiresIn != 900 || registered.RefreshTokenExpiresIn != 86400 {
		t.Fatalf("unexpected TTL metadata: %+v", registered)
	}
	if registered.User.PasswordHash != "" {
		t.Fatal("password hash should not be exposed")
	}

	loggedIn, err := service.Login(model.LoginRequest{
		Email:    "test@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if loggedIn.AccessToken == "" || loggedIn.RefreshToken == "" {
		t.Fatal("Login() tokens are empty")
	}

	refreshed, err := service.Refresh(model.RefreshRequest{RefreshToken: loggedIn.RefreshToken})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if refreshed.RefreshToken == loggedIn.RefreshToken {
		t.Fatal("Refresh() did not rotate refresh token")
	}

	if _, err := service.Refresh(model.RefreshRequest{RefreshToken: loggedIn.RefreshToken}); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("replayed Refresh() error = %v, want ErrInvalidRefreshToken", err)
	}

	if err := service.Logout(model.LogoutRequest{RefreshToken: refreshed.RefreshToken}); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, err := service.Refresh(model.RefreshRequest{RefreshToken: refreshed.RefreshToken}); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("Refresh() after logout error = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestAuthServiceRejectsDuplicateEmailBadPasswordAndInvalidRefresh(t *testing.T) {
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
	if _, err := service.Refresh(model.RefreshRequest{RefreshToken: "not-a-session"}); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("Refresh() error = %v, want ErrInvalidRefreshToken", err)
	}
}
