package middleware

type contextKey string

const (
	userIDKey    contextKey = "user_id"
	requestIDKey contextKey = "request_id"
)
