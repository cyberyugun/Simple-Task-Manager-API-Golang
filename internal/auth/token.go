package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

type Claims struct {
	Subject     string   `json:"sub"`
	Email       string   `json:"email,omitempty"`
	JTI         string   `json:"jti"`
	Scopes      []string `json:"scope,omitempty"`
	TokenUse    string   `json:"token_use,omitempty"`
	ClientID    string   `json:"client_id,omitempty"`
	WorkspaceID int64    `json:"workspace_id,omitempty"`
	ActorUserID int64    `json:"actor_user_id,omitempty"`
	MFA         bool     `json:"mfa,omitempty"`
	Issued      int64    `json:"iat"`
	Expires     int64    `json:"exp"`
}

type tokenHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

func NewTokenManager(secret string, ttl time.Duration) *TokenManager {
	return &TokenManager{secret: []byte(secret), ttl: ttl}
}

func (m *TokenManager) TTL() time.Duration {
	return m.ttl
}

func (m *TokenManager) Generate(userID int64, email string) (string, error) {
	return m.GenerateUser(userID, email, nil, 0)
}

func (m *TokenManager) GenerateUser(userID int64, email string, scopes []string, workspaceID int64) (string, error) {
	return m.GenerateUserWithMFA(userID, email, scopes, workspaceID, false)
}

func (m *TokenManager) GenerateUserForClient(userID int64, email string, scopes []string, workspaceID int64, clientID string) (string, error) {
	clientID = strings.TrimSpace(clientID)
	if userID <= 0 || strings.TrimSpace(email) == "" || clientID == "" {
		return "", ErrInvalidToken
	}
	return m.generate(Claims{
		Subject:     strconv.FormatInt(userID, 10),
		Email:       strings.TrimSpace(email),
		Scopes:      append([]string(nil), scopes...),
		TokenUse:    "user",
		ClientID:    clientID,
		WorkspaceID: workspaceID,
		ActorUserID: userID,
	})
}

func (m *TokenManager) GenerateUserWithMFA(userID int64, email string, scopes []string, workspaceID int64, mfa bool) (string, error) {
	if userID <= 0 || strings.TrimSpace(email) == "" {
		return "", ErrInvalidToken
	}
	return m.generate(Claims{
		Subject:     strconv.FormatInt(userID, 10),
		Email:       strings.TrimSpace(email),
		Scopes:      append([]string(nil), scopes...),
		TokenUse:    "user",
		WorkspaceID: workspaceID,
		ActorUserID: userID,
		MFA:         mfa,
	})
}

func (m *TokenManager) GenerateService(clientID string, workspaceID int64, scopes []string, actorUserID int64) (string, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" || workspaceID <= 0 || actorUserID <= 0 {
		return "", ErrInvalidToken
	}
	return m.generate(Claims{
		Subject:     "client:" + clientID,
		ClientID:    clientID,
		Scopes:      append([]string(nil), scopes...),
		TokenUse:    "service",
		WorkspaceID: workspaceID,
		ActorUserID: actorUserID,
	})
}

func (m *TokenManager) generate(payload Claims) (string, error) {
	now := time.Now()
	jti, err := randomJTI()
	if err != nil {
		return "", err
	}
	payload.JTI = jti
	payload.Issued = now.Unix()
	payload.Expires = now.Add(m.ttl).Unix()

	header := tokenHeader{Algorithm: "HS256", Type: "JWT"}
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
	payload, err := m.ParseClaims(token)
	if err != nil {
		return 0, "", err
	}
	if payload.TokenUse != "" && payload.TokenUse != "user" {
		return 0, "", ErrInvalidToken
	}
	userID, err := strconv.ParseInt(payload.Subject, 10, 64)
	if err != nil || userID <= 0 || payload.Email == "" {
		return 0, "", ErrInvalidToken
	}
	return userID, payload.Email, nil
}

func (m *TokenManager) ParseClaims(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrInvalidToken
	}

	unsigned := parts[0] + "." + parts[1]
	expected := m.sign(unsigned)
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return Claims{}, ErrInvalidToken
	}

	var header tokenHeader
	if err := decodeJSON(parts[0], &header); err != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return Claims{}, ErrInvalidToken
	}

	var payload Claims
	if err := decodeJSON(parts[1], &payload); err != nil {
		return Claims{}, ErrInvalidToken
	}
	if payload.Subject == "" {
		return Claims{}, ErrInvalidToken
	}
	if payload.Expires <= time.Now().Unix() {
		return Claims{}, ErrExpiredToken
	}
	if payload.JTI == "" {
		// Phase 25 adds jti. Derive a stable identifier for pre-Phase-25 tokens
		// during the short access-token compatibility window.
		sum := sha256.Sum256([]byte(token))
		payload.JTI = "legacy-" + hex.EncodeToString(sum[:16])
	}
	return payload, nil
}

func (m *TokenManager) sign(unsigned string) string {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(unsigned))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func randomJTI() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token id: %w", err)
	}
	return hex.EncodeToString(raw), nil
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
