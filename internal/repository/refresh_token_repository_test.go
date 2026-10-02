package repository

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
)

func TestInMemoryRefreshTokenRotationAndRevocation(t *testing.T) {
	repo := NewInMemoryRefreshTokenRepository()
	now := time.Now()

	if err := repo.Create(model.RefreshSession{
		UserID:    7,
		TokenHash: "old",
		ExpiresAt: now.Add(time.Hour),
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	old, err := repo.Rotate("old", "new", now.Add(2*time.Hour), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Rotate() error = %v", err)
	}
	if old.UserID != 7 {
		t.Fatalf("Rotate() UserID = %d, want 7", old.UserID)
	}

	if _, err := repo.Rotate("old", "replay", now.Add(2*time.Hour), now.Add(2*time.Minute)); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("replayed Rotate() error = %v, want ErrInvalidRefreshToken", err)
	}

	if err := repo.Revoke("new", now.Add(3*time.Minute)); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if _, err := repo.Rotate("new", "after-logout", now.Add(3*time.Hour), now.Add(4*time.Minute)); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("Rotate() after revoke error = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestInMemoryRefreshTokenRejectsExpiredToken(t *testing.T) {
	repo := NewInMemoryRefreshTokenRepository()
	now := time.Now()

	if err := repo.Create(model.RefreshSession{
		UserID:    1,
		TokenHash: "expired",
		ExpiresAt: now.Add(-time.Minute),
		CreatedAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Rotate("expired", "new", now.Add(time.Hour), now); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("Rotate() error = %v, want ErrInvalidRefreshToken", err)
	}
}
