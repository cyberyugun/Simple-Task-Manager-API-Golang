package middleware

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/pkg/response"
)

const workspaceHeader = "X-Workspace-ID"

func WorkspaceScope(repo repository.WorkspaceRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := UserIDFromContext(r.Context())
			if !ok {
				response.JSON(w, http.StatusUnauthorized, response.Envelope{Success: false, Message: "authentication required"})
				return
			}

			var workspaceID int64
			raw := strings.TrimSpace(r.Header.Get(workspaceHeader))
			if raw != "" {
				parsed, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || parsed <= 0 {
					response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: "X-Workspace-ID must be a positive integer"})
					return
				}
				workspaceID = parsed
			}

			if claims, hasClaims := AuthClaimsFromContext(r.Context()); hasClaims && claims.TokenUse == model.TokenUseService {
				if claims.WorkspaceID <= 0 || (workspaceID > 0 && workspaceID != claims.WorkspaceID) {
					response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "workspace not found"})
					return
				}
				access := model.WorkspaceAccess{
					Workspace: model.Workspace{ID: claims.WorkspaceID, CreatedByUserID: userID},
					Role:      "service",
				}
				w.Header().Set("X-Workspace-ID", strconv.FormatInt(access.ID, 10))
				w.Header().Set("X-Workspace-Role", access.Role)
				next.ServeHTTP(w, r.WithContext(WithWorkspaceAccess(r.Context(), access)))
				return
			}

			access, err := repo.ResolveAccess(userID, workspaceID, time.Now())
			if err != nil {
				if errors.Is(err, repository.ErrWorkspaceNotFound) {
					response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: "workspace not found"})
					return
				}
				response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "workspace authorization failed"})
				return
			}

			w.Header().Set("X-Workspace-ID", strconv.FormatInt(access.ID, 10))
			w.Header().Set("X-Workspace-Role", access.Role)
			next.ServeHTTP(w, r.WithContext(WithWorkspaceAccess(r.Context(), access)))
		})
	}
}

func WithWorkspaceAccess(ctx context.Context, access model.WorkspaceAccess) context.Context {
	return context.WithValue(ctx, workspaceAccessKey, access)
}

func WorkspaceAccessFromContext(ctx context.Context) (model.WorkspaceAccess, bool) {
	access, ok := ctx.Value(workspaceAccessKey).(model.WorkspaceAccess)
	return access, ok && access.ID > 0 && access.Role != ""
}
