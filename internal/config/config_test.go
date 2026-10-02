package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("JWT_SECRET", "12345678901234567890123456789012")
	t.Setenv("SHUTDOWN_TIMEOUT", "")

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
}

func TestLoadCustomValues(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("JWT_SECRET", "12345678901234567890123456789012")
	t.Setenv("SHUTDOWN_TIMEOUT", "25s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != "9090" || cfg.DatabaseURL != "postgres://example" || cfg.ShutdownTimeout != 25*time.Second {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name     string
		port     string
		secret   string
		shutdown string
	}{
		{name: "invalid port", port: "abc", secret: "12345678901234567890123456789012"},
		{name: "port out of range", port: "70000", secret: "12345678901234567890123456789012"},
		{name: "short secret", port: "8080", secret: "short"},
		{name: "invalid shutdown", port: "8080", secret: "12345678901234567890123456789012", shutdown: "zero"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PORT", tt.port)
			t.Setenv("JWT_SECRET", tt.secret)
			t.Setenv("SHUTDOWN_TIMEOUT", tt.shutdown)
			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
		})
	}
}
