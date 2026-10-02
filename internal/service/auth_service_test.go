package service

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func newTestAuthService() (*AuthService, *repository.InMemoryUserRepository) {
	users := repository.NewInMemoryUserRepository()
	refreshes := repository.NewInMemoryRefreshTokenRepository()
	actions := repository.NewInMemoryAuthActionTokenRepository()
	accessTTL := 15 * time.Minute
	refreshTTL := 24 * time.Hour
	tokens := auth.NewTokenManager("12345678901234567890123456789012", accessTTL)
	return NewAuthService(
		users,
		refreshes,
		actions,
		tokens,
		accessTTL,
		refreshTTL,
		30*time.Minute,
		24*time.Hour,
	), users
}

func TestAuthServiceRegisterLoginRefreshAndLogout(t *testing.T) {
	service, _ := newTestAuthService()
	meta := model.SessionMetadata{UserAgent: "test-agent", IPAddress: "203.0.113.10"}

	registered, err := service.Register(model.RegisterRequest{
		Name:     "Test User",
		Email:    "TEST@example.com",
		Password: "password123",
	}, meta)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if registered.User.Email != "test@example.com" || registered.AccessToken == "" || registered.RefreshToken == "" {
		t.Fatalf("Register() returned unexpected result: %+v", registered)
	}
	if registered.AccessTokenExpiresIn != 900 || registered.RefreshTokenExpiresIn != 86400 {
		t.Fatalf("unexpected TTL metadata: %+v", registered)
	}

	sessions, err := service.Sessions(registered.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].UserAgent != meta.UserAgent || sessions[0].IPAddress != meta.IPAddress {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}

	loggedIn, err := service.Login(model.LoginRequest{
		Email:    "test@example.com",
		Password: "password123",
	}, model.SessionMetadata{UserAgent: "second-agent", IPAddress: "203.0.113.11"})
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

func TestAuthServiceChangePasswordRevokesSessions(t *testing.T) {
	service, _ := newTestAuthService()
	meta := model.SessionMetadata{}

	registered, err := service.Register(model.RegisterRequest{
		Name: "Test User", Email: "test@example.com", Password: "password123",
	}, meta)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(model.LoginRequest{
		Email: "test@example.com", Password: "password123",
	}, meta); err != nil {
		t.Fatal(err)
	}

	if err := service.ChangePassword(registered.User.ID, model.ChangePasswordRequest{
		CurrentPassword: "password123",
		NewPassword:     "new-password-456",
	}); err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}

	sessions, err := service.Sessions(registered.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions after password change = %d, want 0", len(sessions))
	}

	if _, err := service.Login(model.LoginRequest{
		Email: "test@example.com", Password: "password123",
	}, meta); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password Login() error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := service.Login(model.LoginRequest{
		Email: "test@example.com", Password: "new-password-456",
	}, meta); err != nil {
		t.Fatalf("new password Login() error = %v", err)
	}
}

func TestAuthServiceForgotAndResetPasswordOneTimeToken(t *testing.T) {
	service, _ := newTestAuthService()
	meta := model.SessionMetadata{}
	_, err := service.Register(model.RegisterRequest{
		Name: "Test User", Email: "test@example.com", Password: "password123",
	}, meta)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.ForgotPassword(model.ForgotPasswordRequest{Email: "test@example.com"})
	if err != nil {
		t.Fatalf("ForgotPassword() error = %v", err)
	}
	if result.DevelopmentToken == "" {
		t.Fatal("reset token is empty")
	}

	if err := service.ResetPassword(model.ResetPasswordRequest{
		Token: result.DevelopmentToken, NewPassword: "reset-password-789",
	}); err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}

	if err := service.ResetPassword(model.ResetPasswordRequest{
		Token: result.DevelopmentToken, NewPassword: "another-password",
	}); !errors.Is(err, ErrInvalidActionToken) {
		t.Fatalf("replayed ResetPassword() error = %v, want ErrInvalidActionToken", err)
	}

	if _, err := service.Login(model.LoginRequest{
		Email: "test@example.com", Password: "reset-password-789",
	}, meta); err != nil {
		t.Fatalf("login with reset password error = %v", err)
	}

	unknown, err := service.ForgotPassword(model.ForgotPasswordRequest{Email: "missing@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if unknown.DevelopmentToken != "" {
		t.Fatal("unknown account must not receive a reset token")
	}
}

func TestAuthServiceEmailVerificationAndSessionRevocation(t *testing.T) {
	service, users := newTestAuthService()
	registered, err := service.Register(model.RegisterRequest{
		Name: "Test User", Email: "test@example.com", Password: "password123",
	}, model.SessionMetadata{})
	if err != nil {
		t.Fatal(err)
	}

	verification, err := service.RequestEmailVerification(registered.User.ID)
	if err != nil {
		t.Fatalf("RequestEmailVerification() error = %v", err)
	}
	if verification.DevelopmentToken == "" {
		t.Fatal("verification token is empty")
	}
	if err := service.VerifyEmail(model.VerifyEmailRequest{Token: verification.DevelopmentToken}); err != nil {
		t.Fatalf("VerifyEmail() error = %v", err)
	}
	if err := service.VerifyEmail(model.VerifyEmailRequest{Token: verification.DevelopmentToken}); !errors.Is(err, ErrInvalidActionToken) {
		t.Fatalf("replayed VerifyEmail() error = %v, want ErrInvalidActionToken", err)
	}

	user, err := users.FindByID(registered.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if user.EmailVerifiedAt == nil {
		t.Fatal("email was not marked verified")
	}

	if _, err := service.Login(model.LoginRequest{
		Email: "test@example.com", Password: "password123",
	}, model.SessionMetadata{}); err != nil {
		t.Fatal(err)
	}
	sessions, err := service.Sessions(registered.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) < 2 {
		t.Fatalf("sessions = %d, want at least 2", len(sessions))
	}

	if err := service.RevokeSession(registered.User.ID, sessions[0].ID); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	remaining, err := service.Sessions(registered.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != len(sessions)-1 {
		t.Fatalf("remaining sessions = %d, want %d", len(remaining), len(sessions)-1)
	}

	if err := service.LogoutAll(registered.User.ID); err != nil {
		t.Fatal(err)
	}
	remaining, err = service.Sessions(registered.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining sessions after logout-all = %d", len(remaining))
	}
}

func TestAuthServiceRejectsDuplicateEmailBadPasswordAndInvalidRefresh(t *testing.T) {
	service, _ := newTestAuthService()
	meta := model.SessionMetadata{}
	req := model.RegisterRequest{Name: "Test User", Email: "test@example.com", Password: "password123"}

	if _, err := service.Register(req, meta); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := service.Register(req, meta); !errors.Is(err, repository.ErrEmailExists) {
		t.Fatalf("duplicate Register() error = %v, want ErrEmailExists", err)
	}
	if _, err := service.Login(model.LoginRequest{Email: req.Email, Password: "wrong-password"}, meta); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := service.Refresh(model.RefreshRequest{RefreshToken: "not-a-session"}); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("Refresh() error = %v, want ErrInvalidRefreshToken", err)
	}
}
