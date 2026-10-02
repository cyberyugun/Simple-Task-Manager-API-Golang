package repository

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
)

func TestInMemoryAuthActionTokenIsOneTime(t *testing.T) {
	repo := NewInMemoryAuthActionTokenRepository()
	now := time.Now()

	if err := repo.Create(model.AuthActionToken{
		UserID:    5,
		TokenHash: "token-hash",
		Purpose:   model.ActionPasswordReset,
		ExpiresAt: now.Add(time.Hour),
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	token, err := repo.Consume("token-hash", model.ActionPasswordReset, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if token.UserID != 5 || token.ConsumedAt == nil {
		t.Fatalf("unexpected token: %+v", token)
	}

	if _, err := repo.Consume("token-hash", model.ActionPasswordReset, now.Add(2*time.Minute)); !errors.Is(err, ErrInvalidActionToken) {
		t.Fatalf("replayed Consume() error = %v, want ErrInvalidActionToken", err)
	}
}

func TestInMemoryAuthActionTokenRejectsWrongPurposeAndExpiry(t *testing.T) {
	repo := NewInMemoryAuthActionTokenRepository()
	now := time.Now()

	if err := repo.Create(model.AuthActionToken{
		UserID:    1,
		TokenHash: "verify",
		Purpose:   model.ActionEmailVerification,
		ExpiresAt: now.Add(time.Hour),
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Consume("verify", model.ActionPasswordReset, now); !errors.Is(err, ErrInvalidActionToken) {
		t.Fatalf("wrong-purpose Consume() error = %v", err)
	}

	if err := repo.Create(model.AuthActionToken{
		UserID:    1,
		TokenHash: "expired",
		Purpose:   model.ActionPasswordReset,
		ExpiresAt: now.Add(-time.Second),
		CreatedAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Consume("expired", model.ActionPasswordReset, now); !errors.Is(err, ErrInvalidActionToken) {
		t.Fatalf("expired Consume() error = %v", err)
	}
}
