package model

import "time"

type SessionMetadata struct {
	UserAgent string
	IPAddress string
}

type RefreshSession struct {
	ID               int64      `json:"id"`
	UserID           int64      `json:"-"`
	TokenHash        string     `json:"-"`
	UserAgent        string     `json:"user_agent,omitempty"`
	IPAddress        string     `json:"ip_address,omitempty"`
	MFAAuthenticated bool       `json:"mfa_authenticated"`
	ExpiresAt        time.Time  `json:"expires_at"`
	LastUsedAt       time.Time  `json:"last_used_at"`
	RevokedAt        *time.Time `json:"-"`
	CreatedAt        time.Time  `json:"created_at"`
}

type SessionRiskAssessment struct {
	Session    RefreshSession `json:"session"`
	Risk       string         `json:"risk"`
	Indicators []string       `json:"indicators"`
}
