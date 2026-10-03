package config

import (
	"testing"
	"time"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"PORT",
		"DATABASE_URL",
		"REDIS_URL",
		"LOG_LEVEL",
		"SHUTDOWN_TIMEOUT",
		"READINESS_TIMEOUT",
		"ACCESS_TOKEN_TTL",
		"REFRESH_TOKEN_TTL",
		"PASSWORD_RESET_TTL",
		"EMAIL_VERIFICATION_TTL",
		"AUTH_RATE_LIMIT_REQUESTS",
		"AUTH_RATE_LIMIT_WINDOW",
		"RATE_LIMIT_FAIL_OPEN",
		"EXPOSE_AUTH_TOKENS",
		"APP_ENV",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_SERVICE_NAME",
		"DB_MAX_OPEN_CONNS",
		"DB_MAX_IDLE_CONNS",
		"DB_CONN_MAX_IDLE_TIME",
		"DB_CONN_MAX_LIFETIME",
		"REDIS_POOL_SIZE",
		"REDIS_MIN_IDLE_CONNS",
		"REDIS_POOL_TIMEOUT",
		"REDIS_DIAL_TIMEOUT",
		"REDIS_READ_TIMEOUT",
		"REDIS_WRITE_TIMEOUT",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("JWT_SECRET", "12345678901234567890123456789012")
}

func TestLoadDefaults(t *testing.T) {
	setValidEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != "8080" || cfg.LogLevel != "info" {
		t.Fatalf("unexpected basic defaults: %+v", cfg)
	}
	if cfg.ShutdownTimeout != 10*time.Second || cfg.ReadinessTimeout != 2*time.Second {
		t.Fatalf("unexpected operational timeouts: %+v", cfg)
	}
	if cfg.AccessTokenTTL != 15*time.Minute || cfg.RefreshTokenTTL != 30*24*time.Hour {
		t.Fatalf("unexpected auth TTL defaults: %+v", cfg)
	}
	if cfg.PasswordResetTTL != 30*time.Minute || cfg.EmailVerificationTTL != 24*time.Hour {
		t.Fatalf("unexpected action token TTLs: %+v", cfg)
	}
	if cfg.AuthRateLimitRequests != 20 || cfg.AuthRateLimitWindow != time.Minute {
		t.Fatalf("unexpected rate limit defaults: %+v", cfg)
	}
	if !cfg.RateLimitFailOpen {
		t.Fatal("RateLimitFailOpen should default to true")
	}
	if cfg.ExposeAuthTokens {
		t.Fatal("ExposeAuthTokens should default to false")
	}
	if cfg.AppEnv != "development" || cfg.OTELServiceName != "task-api" || cfg.OTELExporterEndpoint != "" {
		t.Fatalf("unexpected observability defaults: %+v", cfg)
	}
	if cfg.DBMaxOpenConns != 10 || cfg.DBMaxIdleConns != 5 || cfg.DBConnMaxIdleTime != 5*time.Minute || cfg.DBConnMaxLifetime != 30*time.Minute {
		t.Fatalf("unexpected database pool defaults: %+v", cfg)
	}
	if cfg.RedisPoolSize != 20 || cfg.RedisMinIdleConns != 5 || cfg.RedisPoolTimeout != 4*time.Second {
		t.Fatalf("unexpected redis pool defaults: %+v", cfg)
	}
}

func TestLoadCustomValues(t *testing.T) {
	setValidEnv(t)
	t.Setenv("PORT", "9090")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("REDIS_URL", "redis://example:6379/0")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("SHUTDOWN_TIMEOUT", "25s")
	t.Setenv("READINESS_TIMEOUT", "3s")
	t.Setenv("ACCESS_TOKEN_TTL", "20m")
	t.Setenv("REFRESH_TOKEN_TTL", "48h")
	t.Setenv("PASSWORD_RESET_TTL", "45m")
	t.Setenv("EMAIL_VERIFICATION_TTL", "12h")
	t.Setenv("AUTH_RATE_LIMIT_REQUESTS", "50")
	t.Setenv("AUTH_RATE_LIMIT_WINDOW", "2m")
	t.Setenv("RATE_LIMIT_FAIL_OPEN", "false")
	t.Setenv("EXPOSE_AUTH_TOKENS", "true")
	t.Setenv("APP_ENV", "production")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector.monitoring.svc.cluster.local:4318")
	t.Setenv("OTEL_SERVICE_NAME", "task-api-prod")
	t.Setenv("DB_MAX_OPEN_CONNS", "24")
	t.Setenv("DB_MAX_IDLE_CONNS", "12")
	t.Setenv("DB_CONN_MAX_IDLE_TIME", "4m")
	t.Setenv("DB_CONN_MAX_LIFETIME", "20m")
	t.Setenv("REDIS_POOL_SIZE", "40")
	t.Setenv("REDIS_MIN_IDLE_CONNS", "10")
	t.Setenv("REDIS_POOL_TIMEOUT", "5s")
	t.Setenv("REDIS_DIAL_TIMEOUT", "6s")
	t.Setenv("REDIS_READ_TIMEOUT", "4s")
	t.Setenv("REDIS_WRITE_TIMEOUT", "4s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != "9090" || cfg.DatabaseURL != "postgres://example" || cfg.RedisURL != "redis://example:6379/0" {
		t.Fatalf("unexpected connection config: %+v", cfg)
	}
	if cfg.LogLevel != "debug" || cfg.ShutdownTimeout != 25*time.Second || cfg.ReadinessTimeout != 3*time.Second {
		t.Fatalf("unexpected operational config: %+v", cfg)
	}
	if cfg.AccessTokenTTL != 20*time.Minute || cfg.RefreshTokenTTL != 48*time.Hour {
		t.Fatalf("unexpected auth TTL config: %+v", cfg)
	}
	if cfg.PasswordResetTTL != 45*time.Minute || cfg.EmailVerificationTTL != 12*time.Hour {
		t.Fatalf("unexpected action TTL config: %+v", cfg)
	}
	if cfg.AuthRateLimitRequests != 50 || cfg.AuthRateLimitWindow != 2*time.Minute {
		t.Fatalf("unexpected rate limit config: %+v", cfg)
	}
	if cfg.RateLimitFailOpen || !cfg.ExposeAuthTokens {
		t.Fatalf("unexpected boolean config: %+v", cfg)
	}
	if cfg.AppEnv != "production" || cfg.OTELServiceName != "task-api-prod" || cfg.OTELExporterEndpoint == "" {
		t.Fatalf("unexpected observability config: %+v", cfg)
	}
	if cfg.DBMaxOpenConns != 24 || cfg.DBMaxIdleConns != 12 || cfg.RedisPoolSize != 40 || cfg.RedisMinIdleConns != 10 {
		t.Fatalf("unexpected pool config: %+v", cfg)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		envName string
		value   string
	}{
		{name: "invalid port", envName: "PORT", value: "abc"},
		{name: "port out of range", envName: "PORT", value: "70000"},
		{name: "invalid log level", envName: "LOG_LEVEL", value: "trace"},
		{name: "invalid shutdown", envName: "SHUTDOWN_TIMEOUT", value: "zero"},
		{name: "invalid readiness", envName: "READINESS_TIMEOUT", value: "0s"},
		{name: "invalid access ttl", envName: "ACCESS_TOKEN_TTL", value: "zero"},
		{name: "invalid refresh ttl", envName: "REFRESH_TOKEN_TTL", value: "-1h"},
		{name: "invalid reset ttl", envName: "PASSWORD_RESET_TTL", value: "nope"},
		{name: "invalid verify ttl", envName: "EMAIL_VERIFICATION_TTL", value: "0s"},
		{name: "invalid rate count", envName: "AUTH_RATE_LIMIT_REQUESTS", value: "0"},
		{name: "invalid rate window", envName: "AUTH_RATE_LIMIT_WINDOW", value: "-1m"},
		{name: "invalid fail open", envName: "RATE_LIMIT_FAIL_OPEN", value: "sometimes"},
		{name: "invalid expose flag", envName: "EXPOSE_AUTH_TOKENS", value: "sometimes"},
		{name: "invalid db max open", envName: "DB_MAX_OPEN_CONNS", value: "0"},
		{name: "invalid db max idle", envName: "DB_MAX_IDLE_CONNS", value: "-1"},
		{name: "invalid db idle duration", envName: "DB_CONN_MAX_IDLE_TIME", value: "0s"},
		{name: "invalid redis pool", envName: "REDIS_POOL_SIZE", value: "0"},
		{name: "invalid redis min idle", envName: "REDIS_MIN_IDLE_CONNS", value: "-1"},
		{name: "invalid redis timeout", envName: "REDIS_POOL_TIMEOUT", value: "0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv(tt.envName, tt.value)
			if _, err := Load(); err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
		})
	}

	t.Run("short secret", func(t *testing.T) {
		setValidEnv(t)
		t.Setenv("JWT_SECRET", "short")
		if _, err := Load(); err == nil {
			t.Fatal("Load() error = nil, want validation error")
		}
	})

	t.Run("db idle exceeds open", func(t *testing.T) {
		setValidEnv(t)
		t.Setenv("DB_MAX_OPEN_CONNS", "4")
		t.Setenv("DB_MAX_IDLE_CONNS", "5")
		if _, err := Load(); err == nil {
			t.Fatal("Load() error = nil, want validation error")
		}
	})

	t.Run("redis idle exceeds pool", func(t *testing.T) {
		setValidEnv(t)
		t.Setenv("REDIS_POOL_SIZE", "4")
		t.Setenv("REDIS_MIN_IDLE_CONNS", "5")
		if _, err := Load(); err == nil {
			t.Fatal("Load() error = nil, want validation error")
		}
	})

	t.Run("refresh not longer than access", func(t *testing.T) {
		setValidEnv(t)
		t.Setenv("ACCESS_TOKEN_TTL", "24h")
		t.Setenv("REFRESH_TOKEN_TTL", "1h")
		if _, err := Load(); err == nil {
			t.Fatal("Load() error = nil, want validation error")
		}
	})
}
