package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPort                  = "8080"
	defaultShutdownTimeout       = 10 * time.Second
	defaultAccessTokenTTL        = 15 * time.Minute
	defaultRefreshTokenTTL       = 30 * 24 * time.Hour
	defaultPasswordResetTTL      = 30 * time.Minute
	defaultEmailVerificationTTL  = 24 * time.Hour
	defaultAuthRateLimitRequests = 20
	defaultAuthRateLimitWindow   = time.Minute
)

type Config struct {
	Port                  string
	DatabaseURL           string
	JWTSecret             string
	ShutdownTimeout       time.Duration
	AccessTokenTTL        time.Duration
	RefreshTokenTTL       time.Duration
	PasswordResetTTL      time.Duration
	EmailVerificationTTL  time.Duration
	AuthRateLimitRequests int
	AuthRateLimitWindow   time.Duration
	ExposeAuthTokens      bool
}

func Load() (Config, error) {
	cfg := Config{
		Port:                  strings.TrimSpace(os.Getenv("PORT")),
		DatabaseURL:           strings.TrimSpace(os.Getenv("DATABASE_URL")),
		JWTSecret:             os.Getenv("JWT_SECRET"),
		ShutdownTimeout:       defaultShutdownTimeout,
		AccessTokenTTL:        defaultAccessTokenTTL,
		RefreshTokenTTL:       defaultRefreshTokenTTL,
		PasswordResetTTL:      defaultPasswordResetTTL,
		EmailVerificationTTL:  defaultEmailVerificationTTL,
		AuthRateLimitRequests: defaultAuthRateLimitRequests,
		AuthRateLimitWindow:   defaultAuthRateLimitWindow,
	}

	if cfg.Port == "" {
		cfg.Port = defaultPort
	}
	port, err := strconv.Atoi(cfg.Port)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("PORT must be an integer between 1 and 65535")
	}

	if len(cfg.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("JWT_SECRET is required and must be at least 32 characters")
	}

	if cfg.ShutdownTimeout, err = parsePositiveDuration("SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	if cfg.AccessTokenTTL, err = parsePositiveDuration("ACCESS_TOKEN_TTL", cfg.AccessTokenTTL); err != nil {
		return Config{}, err
	}
	if cfg.RefreshTokenTTL, err = parsePositiveDuration("REFRESH_TOKEN_TTL", cfg.RefreshTokenTTL); err != nil {
		return Config{}, err
	}
	if cfg.PasswordResetTTL, err = parsePositiveDuration("PASSWORD_RESET_TTL", cfg.PasswordResetTTL); err != nil {
		return Config{}, err
	}
	if cfg.EmailVerificationTTL, err = parsePositiveDuration("EMAIL_VERIFICATION_TTL", cfg.EmailVerificationTTL); err != nil {
		return Config{}, err
	}
	if cfg.AuthRateLimitWindow, err = parsePositiveDuration("AUTH_RATE_LIMIT_WINDOW", cfg.AuthRateLimitWindow); err != nil {
		return Config{}, err
	}
	if cfg.RefreshTokenTTL <= cfg.AccessTokenTTL {
		return Config{}, fmt.Errorf("REFRESH_TOKEN_TTL must be greater than ACCESS_TOKEN_TTL")
	}

	if value := strings.TrimSpace(os.Getenv("AUTH_RATE_LIMIT_REQUESTS")); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 {
			return Config{}, fmt.Errorf("AUTH_RATE_LIMIT_REQUESTS must be a positive integer")
		}
		cfg.AuthRateLimitRequests = limit
	}

	if value := strings.TrimSpace(os.Getenv("EXPOSE_AUTH_TOKENS")); value != "" {
		expose, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("EXPOSE_AUTH_TOKENS must be true or false")
		}
		cfg.ExposeAuthTokens = expose
	}

	return cfg, nil
}

func parsePositiveDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return duration, nil
}
