package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPort            = "8080"
	defaultShutdownTimeout = 10 * time.Second
)

type Config struct {
	Port            string
	DatabaseURL     string
	JWTSecret       string
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		Port:            strings.TrimSpace(os.Getenv("PORT")),
		DatabaseURL:     strings.TrimSpace(os.Getenv("DATABASE_URL")),
		JWTSecret:       os.Getenv("JWT_SECRET"),
		ShutdownTimeout: defaultShutdownTimeout,
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

	if value := strings.TrimSpace(os.Getenv("SHUTDOWN_TIMEOUT")); value != "" {
		timeout, err := time.ParseDuration(value)
		if err != nil || timeout <= 0 {
			return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT must be a positive duration")
		}
		cfg.ShutdownTimeout = timeout
	}

	return cfg, nil
}
