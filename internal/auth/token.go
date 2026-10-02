package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
)

type TokenManager struct {
	secret []byte
	ttl    time.Duration
}

type claims struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
	Issued  int64  `json:"iat"`
	Expires int64  `json:"exp"`
}

type tokenHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

func NewTokenManager(secret string, ttl time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), ttl: ttl}
}

func (m *TokenManager) Generate(userID int64, email string) (string, error) {
	now := time.Now()
	header := tokenHeader{Algorithm: "HS256", Type: "JWT"}
	payload := claims{
		Subject: strconv.FormatInt(userID, 10),
		Email:   email,
		Issued:  now.Unix(),
		Expires: now.Add(m.ttl).Unix(),
	}

	headerPart, err := encodeJSON(header)
	if err != nil {
		return "", err
	}
	payloadPart, err := encodeJSON(payload)
	if err != nil {
		return "", err
	}

	unsigned := headerPart + "." + payloadPart
	signature := m.sign(unsigned)
	return unsigned + "." + signature, nil
}

func (m *TokenManager) Parse(token string) (int64, string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 0, "", ErrInvalidToken
	}

	unsigned := parts[0] + "." + parts[1]
	expected := m.sign(unsigned)
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return 0, "", ErrInvalidToken
	}

	var header tokenHeader
	if err := decodeJSON(parts[0], &header); err != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return 0, "", ErrInvalidToken
	}

	var payload claims
	if err := decodeJSON(parts[1], &payload); err != nil {
		return 0, "", ErrInvalidToken
	}
	if payload.Expires <= time.Now().Unix() {
		return 0, "", ErrExpiredToken
	}

	userID, err := strconv.ParseInt(payload.Subject, 10, 64)
	if err != nil || userID <= 0 || payload.Email == "" {
		return 0, "", ErrInvalidToken
	}

	return userID, payload.Email, nil
}

func (m *TokenManager) sign(unsigned string) string {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(unsigned))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func encodeJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeJSON(part string, target any) error {
	data, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
