package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
)

func newAuthTestHandler() *AuthHandler {
	users := repository.NewInMemoryUserRepository()
	refreshes := repository.NewInMemoryRefreshTokenRepository()
	accessTTL := 15 * time.Minute
	refreshTTL := 24 * time.Hour
	tokens := auth.NewTokenManager("12345678901234567890123456789012", accessTTL)
	return NewAuthHandler(service.NewAuthService(users, refreshes, tokens, accessTTL, refreshTTL))
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
	h := newAuthTestHandler()

	registerReq := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/register",
		strings.NewReader(`{"name":"Test User","email":"test@example.com","password":"password123"}`),
	)
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

	afterLogoutReq := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/refresh",
		strings.NewReader(`{"refresh_token":"`+refreshed.RefreshToken+`"}`),
	)
	afterLogoutRes := httptest.NewRecorder()
	h.Refresh(afterLogoutRes, afterLogoutReq)
	if afterLogoutRes.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout status = %d, want %d", afterLogoutRes.Code, http.StatusUnauthorized)
	}
}
