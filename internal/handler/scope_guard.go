package handler

import (
	"net/http"

	"go-simple-task-api/internal/middleware"
	"go-simple-task-api/pkg/response"
)

func requireScope(w http.ResponseWriter, r *http.Request, scope string) bool {
	if middleware.HasScope(r.Context(), scope) {
		return true
	}
	response.JSON(w, http.StatusForbidden, response.Envelope{Success: false, Message: "insufficient token scope"})
	return false
}
