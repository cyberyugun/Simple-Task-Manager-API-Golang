package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/middleware"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
)

func newAuthTestHandler(expose bool) *AuthHandler {
	users := repository.NewInMemoryUserRepository()
	refreshes := repository.NewInMemoryRefreshTokenRepository()
	actions := repository.NewInMemoryAuthActionTokenRepository()
	accessTTL := 15 * time.Minute
	refreshTTL := 24 * time.Hour
	tokens := auth.NewTokenManager("12345678901234567890123456789012", accessTTL)
	return NewAuthHandler(
		service.NewAuthService(
			users,
			refreshes,
			actions,
			tokens,
			accessTTL,
			refreshTTL,
			30*time.Minute,
			24*time.Hour,
		),
		expose,
	)
}

func decodeAuthResult(t *testing.T, recorder *httptest.ResponseRecorder) model.AuthResult {
	t.Helper()
	var envelope struct {
		Success bool             `json:"success"`
		Data    model.AuthResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return envelope.Data
}

func TestAuthHandlerRefreshRotationAndLogout(t *testing.T) {
	h := newAuthTestHandler(true)

	registerReq := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/register",
		strings.NewReader(`{"name":"Test User","email":"test@example.com","password":"password123"}`),
	)
	registerReq.RemoteAddr = "203.0.113.10:1234"
	registerReq.Header.Set("User-Agent", "handler-test")
	registerRes := httptest.NewRecorder()
	h.Register(registerRes, registerReq)
	if registerRes.Code != http.StatusCreated {
		t.Fatalf("register status = %d; body=%s", registerRes.Code, registerRes.Body.String())
	}
	registered := decodeAuthResult(t, registerRes)
	if registered.RefreshToken == "" {
		t.Fatal("register refresh token is empty")
	}

	refreshReq := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/refresh",
		strings.NewReader(`{"refresh_token":"`+registered.RefreshToken+`"}`),
	)
	refreshRes := httptest.NewRecorder()
	h.Refresh(refreshRes, refreshReq)
	if refreshRes.Code != http.StatusOK {
		t.Fatalf("refresh status = %d; body=%s", refreshRes.Code, refreshRes.Body.String())
	}
	refreshed := decodeAuthResult(t, refreshRes)
	if refreshed.RefreshToken == registered.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}

	replayReq := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/refresh",
		strings.NewReader(`{"refresh_token":"`+registered.RefreshToken+`"}`),
	)
	replayRes := httptest.NewRecorder()
	h.Refresh(replayRes, replayReq)
	if replayRes.Code != http.StatusUnauthorized {
		t.Fatalf("replayed refresh status = %d, want %d", replayRes.Code, http.StatusUnauthorized)
	}

	logoutReq := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/logout",
		strings.NewReader(`{"refresh_token":"`+refreshed.RefreshToken+`"}`),
	)
	logoutRes := httptest.NewRecorder()
	h.Logout(logoutRes, logoutReq)
	if logoutRes.Code != http.StatusOK {
		t.Fatalf("logout status = %d; body=%s", logoutRes.Code, logoutRes.Body.String())
	}
}

func TestAuthHandlerForgotResetAndDevelopmentTokenVisibility(t *testing.T) {
	exposed := newAuthTestHandler(true)
	hidden := newAuthTestHandler(false)

	register := func(t *testing.T, h *AuthHandler, email string) {
		t.Helper()
		res := httptest.NewRecorder()
		h.Register(res, httptest.NewRequest(
			http.MethodPost,
			"/api/auth/register",
			strings.NewReader(`{"name":"Test User","email":"`+email+`","password":"password123"}`),
		))
		if res.Code != http.StatusCreated {
			t.Fatalf("register status = %d; body=%s", res.Code, res.Body.String())
		}
	}

	register(t, exposed, "exposed@example.com")
	forgotRes := httptest.NewRecorder()
	exposed.ForgotPassword(forgotRes, httptest.NewRequest(
		http.MethodPost,
		"/api/auth/forgot-password",
		strings.NewReader(`{"email":"exposed@example.com"}`),
	))
	if forgotRes.Code != http.StatusOK {
		t.Fatalf("forgot status = %d", forgotRes.Code)
	}
	var exposedEnvelope struct {
		Data model.ActionTokenResult `json:"data"`
	}
	if err := json.Unmarshal(forgotRes.Body.Bytes(), &exposedEnvelope); err != nil {
		t.Fatal(err)
	}
	if exposedEnvelope.Data.DevelopmentToken == "" {
		t.Fatal("development token should be visible when enabled")
	}

	resetRes := httptest.NewRecorder()
	exposed.ResetPassword(resetRes, httptest.NewRequest(
		http.MethodPost,
		"/api/auth/reset-password",
		strings.NewReader(`{"token":"`+exposedEnvelope.Data.DevelopmentToken+`","new_password":"new-password-456"}`),
	))
	if resetRes.Code != http.StatusOK {
		t.Fatalf("reset status = %d; body=%s", resetRes.Code, resetRes.Body.String())
	}

	register(t, hidden, "hidden@example.com")
	hiddenRes := httptest.NewRecorder()
	hidden.ForgotPassword(hiddenRes, httptest.NewRequest(
		http.MethodPost,
		"/api/auth/forgot-password",
		strings.NewReader(`{"email":"hidden@example.com"}`),
	))
	if strings.Contains(hiddenRes.Body.String(), "development_token") {
		t.Fatalf("development token leaked: %s", hiddenRes.Body.String())
	}
}

func TestAuthHandlerSessionsAndChangePassword(t *testing.T) {
	h := newAuthTestHandler(true)

	registerReq := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/register",
		strings.NewReader(`{"name":"Test User","email":"test@example.com","password":"password123"}`),
	)
	registerRes := httptest.NewRecorder()
	h.Register(registerRes, registerReq)
	registered := decodeAuthResult(t, registerRes)

	sessionsReq := httptest.NewRequest(http.MethodGet, "/api/auth/sessions", nil)
	sessionsReq = sessionsReq.WithContext(middleware.WithUserID(sessionsReq.Context(), registered.User.ID))
	sessionsRes := httptest.NewRecorder()
	h.Sessions(sessionsRes, sessionsReq)
	if sessionsRes.Code != http.StatusOK {
		t.Fatalf("sessions status = %d; body=%s", sessionsRes.Code, sessionsRes.Body.String())
	}

	changeReq := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/change-password",
		strings.NewReader(`{"current_password":"password123","new_password":"new-password-456"}`),
	)
	changeReq = changeReq.WithContext(middleware.WithUserID(changeReq.Context(), registered.User.ID))
	changeRes := httptest.NewRecorder()
	h.ChangePassword(changeRes, changeReq)
	if changeRes.Code != http.StatusOK {
		t.Fatalf("change password status = %d; body=%s", changeRes.Code, changeRes.Body.String())
	}

	afterReq := httptest.NewRequest(http.MethodGet, "/api/auth/sessions", nil)
	afterReq = afterReq.WithContext(middleware.WithUserID(afterReq.Context(), registered.User.ID))
	afterRes := httptest.NewRecorder()
	h.Sessions(afterRes, afterReq)
	if afterRes.Code != http.StatusOK || !strings.Contains(afterRes.Body.String(), `"data":[]`) {
		t.Fatalf("sessions after password change = %d; body=%s", afterRes.Code, afterRes.Body.String())
	}
}
