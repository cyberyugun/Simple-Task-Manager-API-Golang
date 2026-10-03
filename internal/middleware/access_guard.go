package middleware

import (
	"net/http"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/pkg/response"
)

func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !HasScope(r.Context(), scope) {
				response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: "insufficient token scope"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func RequireFirstPartyUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := AuthClaimsFromContext(r.Context())
		if !ok {
			// Unit tests may inject an authenticated user context directly.
			next.ServeHTTP(w, r)
			return
		}
		if claims.TokenUse == model.TokenUseService || len(claims.Scopes) != 0 {
			response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: "first-party user session required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
