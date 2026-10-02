package repository

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
)

func TestInMemoryRefreshTokenRotationAndSessionManagement(t *testing.T) {
	repo := NewInMemoryRefreshTokenRepository()
	now := time.Now()

	if err := repo.Create(model.RefreshSession{
		UserID:     7,
		TokenHash:  "old",
		UserAgent:  "browser-a",
		IPAddress:  "203.0.113.10",
		ExpiresAt:  now.Add(time.Hour),
		LastUsedAt: now,
		CreatedAt:  now,
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

	sessions, err := repo.ListActive(7, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].UserAgent != "browser-a" || sessions[0].TokenHash != "" {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}

	if err := repo.RevokeByID(7, sessions[0].ID, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("RevokeByID() error = %v", err)
	}
	sessions, err = repo.ListActive(7, now.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions after revoke = %d", len(sessions))
	}

	if err := repo.Create(model.RefreshSession{
		UserID:     7,
		TokenHash:  "third",
		ExpiresAt:  now.Add(time.Hour),
		LastUsedAt: now,
		CreatedAt:  now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(model.RefreshSession{
		UserID:     7,
		TokenHash:  "fourth",
		ExpiresAt:  now.Add(time.Hour),
		LastUsedAt: now,
		CreatedAt:  now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.RevokeAll(7, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	sessions, err = repo.ListActive(7, now.Add(6*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions after RevokeAll() = %d", len(sessions))
	}
}

func TestInMemoryRefreshTokenRejectsExpiredToken(t *testing.T) {
	repo := NewInMemoryRefreshTokenRepository()
	now := time.Now()

	if err := repo.Create(model.RefreshSession{
		UserID:     1,
		TokenHash:  "expired",
		ExpiresAt:  now.Add(-time.Minute),
		LastUsedAt: now.Add(-time.Hour),
		CreatedAt:  now.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Rotate("expired", "new", now.Add(time.Hour), now); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("Rotate() error = %v, want ErrInvalidRefreshToken", err)
	}
}
