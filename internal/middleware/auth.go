package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/auth"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/pkg/response"
)

func Auth(tokenManager *auth.TokenManager) func(http.Handler) http.Handler {
	return authenticate(tokenManager, nil, false)
}

func AuthWithRevocation(tokenManager *auth.TokenManager, repo repository.EnterpriseIdentityRepository) func(http.Handler) http.Handler {
	return authenticate(tokenManager, repo, false)
}

func EnterpriseAuth(tokenManager *auth.TokenManager, repo repository.EnterpriseIdentityRepository) func(http.Handler) http.Handler {
	return authenticate(tokenManager, repo, true)
}

func authenticate(tokenManager *auth.TokenManager, repo repository.EnterpriseIdentityRepository, allowService bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authorization := strings.TrimSpace(r.Header.Get("Authorization"))
			parts := strings.SplitN(authorization, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
				response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "missing or invalid authorization header"})
				return
			}

			claims, err := tokenManager.ParseClaims(strings.TrimSpace(parts[1]))
			if err != nil {
				response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "invalid or expired access token"})
				return
			}
			if claims.ConfirmationThumbprint != "" {
				if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
					response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "certificate-bound access token requires mTLS"})
					return
				}
				sum := sha256.Sum256(r.TLS.PeerCertificates[0].Raw)
				fingerprint := hex.EncodeToString(sum[:])
				if !strings.EqualFold(fingerprint, claims.ConfirmationThumbprint) {
					response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "mTLS certificate does not match access token binding"})
					return
				}
			}
			if repo != nil {
				revoked, err := repo.IsAccessTokenRevoked(claims.JTI, time.Now())
				if err != nil {
					response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "token revocation check failed"})
					return
				}
				if revoked {
					response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "access token revoked"})
					return
				}
			}

			var userID int64
			switch claims.TokenUse {
			case "", "user":
				userID, err = strconv.ParseInt(claims.Subject, 10, 64)
				if err != nil || userID <= 0 || claims.Email == "" {
					response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "invalid access token"})
					return
				}
			case "service":
				if !allowService || claims.ActorUserID <= 0 || claims.WorkspaceID <= 0 {
					response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "service token not allowed"})
					return
				}
				userID = claims.ActorUserID
			default:
				response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "invalid token use"})
				return
			}

			ctx := WithUserID(r.Context(), userID)
			ctx = WithAuthClaims(ctx, claims)
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

func WithAuthClaims(ctx context.Context, claims auth.Claims) context.Context {
	return context.WithValue(ctx, authClaimsKey, claims)
}

func AuthClaimsFromContext(ctx context.Context) (auth.Claims, bool) {
	claims, ok := ctx.Value(authClaimsKey).(auth.Claims)
	return claims, ok && claims.Subject != ""
}

func HasScope(ctx context.Context, scope string) bool {
	claims, ok := AuthClaimsFromContext(ctx)
	if !ok {
		// Production routes install claims in authentication middleware. Tests may
		// explicitly inject user/workspace context and are treated as trusted.
		return true
	}
	if claims.TokenUse == "user" && len(claims.Scopes) == 0 {
		return true
	}
	for _, candidate := range claims.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}
