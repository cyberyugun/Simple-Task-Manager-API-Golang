package service

import (
	"errors"
	"testing"

	"go-simple-task-api/internal/model"
)

type fakeMFAVerifier struct {
	enabled bool
	code    string
}

func (f fakeMFAVerifier) Enabled(int64) (bool, error) {
	return f.enabled, nil
}

func (f fakeMFAVerifier) Verify(_ int64, code string) (bool, error) {
	return code == f.code, nil
}

func TestAuthServiceRequiresAndPersistsMFAAssurance(t *testing.T) {
	svc, _ := newTestAuthService()
	registered, err := svc.Register(model.RegisterRequest{
		Name: "MFA User", Email: "mfa@example.com", Password: "password123",
	}, model.SessionMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetMFAVerifier(fakeMFAVerifier{enabled: true, code: "123456"})

	if _, err := svc.Login(model.LoginRequest{
		Email: "mfa@example.com", Password: "password123",
	}, model.SessionMetadata{}); !errors.Is(err, ErrMFARequired) {
		t.Fatalf("Login without MFA error = %v, want ErrMFARequired", err)
	}
	if _, err := svc.Login(model.LoginRequest{
		Email: "mfa@example.com", Password: "password123", MFACode: "000000",
	}, model.SessionMetadata{}); !errors.Is(err, ErrInvalidMFA) {
		t.Fatalf("Login with bad MFA error = %v, want ErrInvalidMFA", err)
	}

	login, err := svc.Login(model.LoginRequest{
		Email: "mfa@example.com", Password: "password123", MFACode: "123456",
	}, model.SessionMetadata{})
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := svc.Sessions(registered.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundMFA := false
	for _, session := range sessions {
		if session.MFAAuthenticated {
			foundMFA = true
			break
		}
	}
	if !foundMFA {
		t.Fatal("mfa-authenticated refresh session was not persisted")
	}

	refreshed, err := svc.Refresh(model.RefreshRequest{RefreshToken: login.RefreshToken})
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.AccessToken == "" {
		t.Fatal("refreshed access token is empty")
	}

	sessions, err = svc.Sessions(registered.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundMFA = false
	for _, session := range sessions {
		if session.MFAAuthenticated {
			foundMFA = true
		}
	}
	if !foundMFA {
		t.Fatal("refresh rotation lost mfa assurance")
	}
}
