package auth

import (
	"errors"
	"testing"
	"time"
)

func TestTokenManagerGenerateAndParse(t *testing.T) {
	manager := NewTokenManager("12345678901234567890123456789012", time.Hour)
	token, err := manager.Generate(42, "user@example.com")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	userID, email, err := manager.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if userID != 42 || email != "user@example.com" {
		t.Fatalf("Parse() = (%d, %q), want (42, user@example.com)", userID, email)
	}
}

func TestTokenManagerRejectsTampering(t *testing.T) {
	manager := NewTokenManager("12345678901234567890123456789012", time.Hour)
	token, _ := manager.Generate(1, "user@example.com")
	token += "tampered"

	if _, _, err := manager.Parse(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Parse() error = %v, want ErrInvalidToken", err)
	}
}
