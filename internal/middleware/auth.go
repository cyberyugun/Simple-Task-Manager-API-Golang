package middleware

import (
	"context"
	"net/http"
	"strings"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/pkg/response"
)

func Auth(tokenManager *auth.TokenManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authorization := strings.TrimSpace(r.Header.Get("Authorization"))
			parts := strings.SplitN(authorization, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
				response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "missing or invalid authorization header"})
				return
			}

			userID, _, err := tokenManager.Parse(strings.TrimSpace(parts[1]))
			if err != nil {
				response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "invalid or expired access token"})
				return
			}

			ctx := WithUserID(r.Context(), userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

func UserIDFromContext(ctx context.Context) (int64, bool) {
	userID, ok := ctx.Value(userIDKey).(int64)
	return userID, ok && userID > 0
}
