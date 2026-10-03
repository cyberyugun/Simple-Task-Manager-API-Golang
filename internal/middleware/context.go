package middleware

type contextKey string

const (
	userIDKey          contextKey = "user_id"
	workspaceAccessKey contextKey = "workspace_access"
	requestIDKey       contextKey = "request_id"
)
