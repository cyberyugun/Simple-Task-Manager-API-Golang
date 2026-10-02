package config

import (
	"testing"
	"time"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("JWT_SECRET", "12345678901234567890123456789012")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("ACCESS_TOKEN_TTL", "")
	t.Setenv("REFRESH_TOKEN_TTL", "")
	t.Setenv("PASSWORD_RESET_TTL", "")
	t.Setenv("EMAIL_VERIFICATION_TTL", "")
	t.Setenv("AUTH_RATE_LIMIT_REQUESTS", "")
	t.Setenv("AUTH_RATE_LIMIT_WINDOW", "")
	t.Setenv("EXPOSE_AUTH_TOKENS", "")
}

func TestLoadDefaults(t *testing.T) {
	setValidEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != "8080" {
		t.Fatalf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("ShutdownTimeout = %v, want 10s", cfg.ShutdownTimeout)
	}
	if cfg.AccessTokenTTL != 15*time.Minute {
		t.Fatalf("AccessTokenTTL = %v, want 15m", cfg.AccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != 30*24*time.Hour {
		t.Fatalf("RefreshTokenTTL = %v, want 720h", cfg.RefreshTokenTTL)
	}
	if cfg.PasswordResetTTL != 30*time.Minute || cfg.EmailVerificationTTL != 24*time.Hour {
		t.Fatalf("unexpected action token TTLs: %+v", cfg)
	}
	if cfg.AuthRateLimitRequests != 20 || cfg.AuthRateLimitWindow != time.Minute {
		t.Fatalf("unexpected rate limit defaults: %+v", cfg)
	}
	if cfg.ExposeAuthTokens {
		t.Fatal("ExposeAuthTokens should default to false")
	}
}

func TestLoadCustomValues(t *testing.T) {
	setValidEnv(t)
	t.Setenv("PORT", "9090")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("SHUTDOWN_TIMEOUT", "25s")
	t.Setenv("ACCESS_TOKEN_TTL", "20m")
	t.Setenv("REFRESH_TOKEN_TTL", "48h")
	t.Setenv("PASSWORD_RESET_TTL", "45m")
	t.Setenv("EMAIL_VERIFICATION_TTL", "12h")
	t.Setenv("AUTH_RATE_LIMIT_REQUESTS", "50")
	t.Setenv("AUTH_RATE_LIMIT_WINDOW", "2m")
	t.Setenv("EXPOSE_AUTH_TOKENS", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != "9090" || cfg.DatabaseURL != "postgres://example" || cfg.ShutdownTimeout != 25*time.Second {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.AccessTokenTTL != 20*time.Minute || cfg.RefreshTokenTTL != 48*time.Hour {
		t.Fatalf("unexpected auth TTL config: %+v", cfg)
	}
	if cfg.PasswordResetTTL != 45*time.Minute || cfg.EmailVerificationTTL != 12*time.Hour {
		t.Fatalf("unexpected action TTL config: %+v", cfg)
	}
	if cfg.AuthRateLimitRequests != 50 || cfg.AuthRateLimitWindow != 2*time.Minute || !cfg.ExposeAuthTokens {
		t.Fatalf("unexpected security config: %+v", cfg)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name       string
		port       string
		secret     string
		shutdown   string
		accessTTL  string
		refreshTTL string
		resetTTL   string
		verifyTTL  string
		rateCount  string
		rateWindow string
		expose     string
	}{
		{name: "invalid port", port: "abc", secret: "12345678901234567890123456789012"},
		{name: "port out of range", port: "70000", secret: "12345678901234567890123456789012"},
		{name: "short secret", port: "8080", secret: "short"},
		{name: "invalid shutdown", port: "8080", secret: "12345678901234567890123456789012", shutdown: "zero"},
		{name: "invalid access ttl", port: "8080", secret: "12345678901234567890123456789012", accessTTL: "zero"},
		{name: "invalid refresh ttl", port: "8080", secret: "12345678901234567890123456789012", refreshTTL: "-1h"},
		{name: "refresh not longer", port: "8080", secret: "12345678901234567890123456789012", accessTTL: "24h", refreshTTL: "1h"},
		{name: "invalid reset ttl", port: "8080", secret: "12345678901234567890123456789012", resetTTL: "nope"},
		{name: "invalid verify ttl", port: "8080", secret: "12345678901234567890123456789012", verifyTTL: "0s"},
		{name: "invalid rate count", port: "8080", secret: "12345678901234567890123456789012", rateCount: "0"},
		{name: "invalid rate window", port: "8080", secret: "12345678901234567890123456789012", rateWindow: "-1m"},
		{name: "invalid expose flag", port: "8080", secret: "12345678901234567890123456789012", expose: "sometimes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv("PORT", tt.port)
			t.Setenv("JWT_SECRET", tt.secret)
			t.Setenv("SHUTDOWN_TIMEOUT", tt.shutdown)
			t.Setenv("ACCESS_TOKEN_TTL", tt.accessTTL)
			t.Setenv("REFRESH_TOKEN_TTL", tt.refreshTTL)
			t.Setenv("PASSWORD_RESET_TTL", tt.resetTTL)
			t.Setenv("EMAIL_VERIFICATION_TTL", tt.verifyTTL)
			t.Setenv("AUTH_RATE_LIMIT_REQUESTS", tt.rateCount)
			t.Setenv("AUTH_RATE_LIMIT_WINDOW", tt.rateWindow)
			t.Setenv("EXPOSE_AUTH_TOKENS", tt.expose)
			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
		})
	}
}
