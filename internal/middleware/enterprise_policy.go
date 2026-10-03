package middleware

import (
	"errors"
	"net/http"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/pkg/response"
)

func EnterpriseWorkspacePolicy(repo repository.EnterpriseIdentityRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, hasClaims := AuthClaimsFromContext(r.Context())
			if !hasClaims || claims.TokenUse == model.TokenUseService {
				next.ServeHTTP(w, r)
				return
			}
			access, ok := WorkspaceAccessFromContext(r.Context())
			if !ok {
				response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "workspace scope is missing"})
				return
			}

			policy, err := repo.GetSecurityPolicy(access.ID)
			if errors.Is(err, repository.ErrEnterprisePolicy) {
				next.ServeHTTP(w, r)
				return
			}
			if err != nil {
				response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "enterprise policy check failed"})
				return
			}
			if policy.RequireMFA && !claims.MFA {
				response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: "workspace requires mfa-authenticated session"})
				return
			}
			if policy.MaxSessionAgeMinutes > 0 && claims.Issued > 0 {
				maxAge := time.Duration(policy.MaxSessionAgeMinutes) * time.Minute
				if time.Since(time.Unix(claims.Issued, 0)) > maxAge {
					response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "session exceeds workspace maximum age"})
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
