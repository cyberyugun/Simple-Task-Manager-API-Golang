package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const opaqueTokenBytes = 32

func GenerateOpaqueToken() (string, string, error) {
	raw := make([]byte, opaqueTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate opaque token: %w", err)
	}

	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, HashOpaqueToken(token), nil
}

func HashOpaqueToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func GenerateRefreshToken() (string, string, error) {
	return GenerateOpaqueToken()
}

func HashRefreshToken(token string) string {
	return HashOpaqueToken(token)
}
