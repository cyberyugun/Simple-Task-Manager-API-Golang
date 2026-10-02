package model

import "time"

const (
	ActionPasswordReset		= "password_reset"
	ActionEmailVerification	= "email_verification"
)

type AuthActionToken struct {
	ID         int64
	UserID     int64
	TokenHash  string
	Purpose    string
	ExpiresAt  time.Time
	ConsumedAt *time.Time
	CreatedAt  time.Time
}
