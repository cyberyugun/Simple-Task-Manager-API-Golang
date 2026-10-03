package model

import "time"

type TOTPCredential struct {
	UserID           int64      `json:"user_id"`
	SecretCiphertext string     `json:"-"`
	ConfirmedAt      *time.Time `json:"confirmed_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type TOTPEnrollResult struct {
	Secret     string `json:"secret"`
	OTPAuthURI string `json:"otpauth_uri"`
}

type TOTPCodeRequest struct {
	Code string `json:"code"`
}

type MFAStatus struct {
	TOTPEnabled       bool `json:"totp_enabled"`
	WebAuthnAvailable bool `json:"webauthn_available"`
}

type WebAuthnCeremonyResult struct {
	SessionID string `json:"session_id"`
	Options   any    `json:"options"`
}

type WebAuthnPasswordRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
