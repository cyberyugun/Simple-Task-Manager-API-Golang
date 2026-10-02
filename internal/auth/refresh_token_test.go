package auth

import "testing"

func TestGenerateRefreshToken(t *testing.T) {
	token, hash, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}
	if token == "" || hash == "" {
		t.Fatal("refresh token or hash is empty")
	}
	if got := HashRefreshToken(token); got != hash {
		t.Fatalf("HashRefreshToken() = %q, want %q", got, hash)
	}

	token2, hash2, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken() second error = %v", err)
	}
	if token == token2 || hash == hash2 {
		t.Fatal("refresh tokens must be unique")
	}
}
