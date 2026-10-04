package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
	"go-simple-task-api/internal/service"
	"go-simple-task-api/pkg/response"
)

type SearchAnalyticsHandler struct {
	service *service.SearchAnalyticsService
}

func NewSearchAnalyticsHandler(service *service.SearchAnalyticsService) *SearchAnalyticsHandler {
	return &SearchAnalyticsHandler{service: service}
}

func (h *SearchAnalyticsHandler) SearchTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	_, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	query, err := parseSearchQuery(r)
	if err != nil {
		h.writeError(w, err)
		return
	}
	result, err := h.service.Search(access.ID, query)
	h.write(w, result, err, http.StatusOK)
}

func (h *SearchAnalyticsHandler) SavedViews(w http.ResponseWriter, r *http.Request) {
	userID, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.SavedViews(access.ID, userID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateSavedSearchViewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateSavedView(userID, access.ID, req)
		h.write(w, item, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *SearchAnalyticsHandler) SavedViewByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	userID, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("view_id"), 10, 64)
	if err != nil || id <= 0 {
		h.writeError(w, service.ErrInvalidSavedSearchView)
		return
	}
	err = h.service.DeleteSavedView(userID, access.ID, id)
	h.write(w, map[string]any{"id": id}, err, http.StatusOK)
}

func (h *SearchAnalyticsHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	_, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	days := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			h.writeError(w, service.ErrInvalidAnalyticsRequest)
			return
		}
		days = value
	}
	item, err := h.service.Dashboard(access.ID, days)
	h.write(w, item, err, http.StatusOK)
}

func (h *SearchAnalyticsHandler) Workload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	_, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	item, err := h.service.Dashboard(access.ID, 30)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item.Workload})
}

func (h *SearchAnalyticsHandler) Trends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	_, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	days := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			h.writeError(w, service.ErrInvalidAnalyticsRequest)
			return
		}
		days = value
	}
	item, err := h.service.Dashboard(access.ID, days)
	if err != nil {
		h.writeError(w, err)
		return
	}
	response.JSON(w, http.StatusOK, response.Envelope{Success: true, Data: item.Trend})
}

func (h *SearchAnalyticsHandler) Export(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	_, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	filters := reportFiltersFromQuery(r)
	result, err := h.service.Export(access.ID, r.URL.Query().Get("format"), filters)
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", result.Filename))
	w.Header().Set("X-Report-Row-Count", strconv.Itoa(result.RowCount))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Content)
}

func (h *SearchAnalyticsHandler) Schedules(w http.ResponseWriter, r *http.Request) {
	userID, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := h.service.ScheduledReports(access.ID, userID)
		h.write(w, items, err, http.StatusOK)
	case http.MethodPost:
		defer r.Body.Close()
		var req model.CreateScheduledReportRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badJSON(w)
			return
		}
		item, err := h.service.CreateScheduledReport(userID, access.ID, req)
		h.write(w, item, err, http.StatusCreated)
	default:
		methodNotAllowed(w)
	}
}

func (h *SearchAnalyticsHandler) ScheduleByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	userID, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("report_id"), 10, 64)
	if err != nil || id <= 0 {
		h.writeError(w, service.ErrInvalidReportRequest)
		return
	}
	err = h.service.DeleteScheduledReport(userID, access.ID, id)
	h.write(w, map[string]any{"id": id}, err, http.StatusOK)
}

func (h *SearchAnalyticsHandler) ReportRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	userID, access, ok := requestScope(w, r)
	if !ok || !requireTaskScope(w, r) {
		return
	}
	limit := int64(50)
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			h.writeError(w, service.ErrInvalidReportRequest)
			return
		}
		limit = value
	}
	items, err := h.service.ReportRuns(access.ID, userID, limit)
	h.write(w, items, err, http.StatusOK)
}

func parseSearchQuery(r *http.Request) (model.SearchQuery, error) {
	values := r.URL.Query()
	query := model.SearchQuery{
		Query: values.Get("q"), Status: values.Get("status"), Priority: values.Get("priority"),
		Page: 1, Limit: 20,
	}
	if raw := values.Get("page"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return model.SearchQuery{}, service.ErrInvalidSearchQuery
		}
		query.Page = value
	}
	if raw := values.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return model.SearchQuery{}, service.ErrInvalidSearchQuery
		}
		query.Limit = value
	}
	for key, target := range map[string]**int64{"project_id": &query.ProjectID, "assignee_id": &query.AssigneeID} {
		if raw := values.Get(key); raw != "" {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || value <= 0 {
				return model.SearchQuery{}, service.ErrInvalidSearchQuery
			}
			*target = &value
		}
	}
	return query, nil
}

func reportFiltersFromQuery(r *http.Request) map[string]any {
	values := r.URL.Query()
	filters := map[string]any{}
	for _, key := range []string{"query", "status", "priority"} {
		if value := strings.TrimSpace(values.Get(key)); value != "" {
			filters[key] = value
		}
	}
	for _, key := range []string{"project_id", "assignee_id"} {
		if raw := strings.TrimSpace(values.Get(key)); raw != "" {
			if value, err := strconv.ParseInt(raw, 10, 64); err == nil && value > 0 {
				filters[key] = value
			}
		}
	}
	return filters
}

func (h *SearchAnalyticsHandler) write(w http.ResponseWriter, data any, err error, status int) {
	if err != nil {
		h.writeError(w, err)
		return
	}
	response.JSON(w, status, response.Envelope{Success: true, Data: data})
}

func (h *SearchAnalyticsHandler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidSearchQuery),
		errors.Is(err, service.ErrInvalidSavedSearchView),
		errors.Is(err, service.ErrInvalidAnalyticsRequest),
		errors.Is(err, service.ErrInvalidReportRequest):
		response.JSON(w, http.StatusBadRequest, response.Envelope{Success: false, Message: err.Error()})
	case errors.Is(err, repository.ErrSavedSearchViewNotFound),
		errors.Is(err, repository.ErrScheduledReportNotFound),
		errors.Is(err, repository.ErrReportRunNotFound):
		response.JSON(w, http.StatusNotFound, response.Envelope{Success: false, Message: err.Error()})
	default:
		response.JSON(w, http.StatusInternalServerError, response.Envelope{Success: false, Message: "internal server error"})
	}
}
