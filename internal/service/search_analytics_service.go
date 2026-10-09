package service

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

var (
	ErrInvalidSearchQuery      = errors.New("invalid search query")
	ErrInvalidSavedSearchView  = errors.New("invalid saved search view")
	ErrInvalidAnalyticsRequest = errors.New("invalid analytics request")
	ErrInvalidReportRequest    = errors.New("invalid report request")
)

type SearchAnalyticsService struct {
	repo       repository.SearchAnalyticsRepository
	workspaces repository.WorkspaceRepository
}

type ReportExportResult struct {
	Content     []byte
	ContentType string
	Filename    string
	RowCount    int
}

func NewSearchAnalyticsService(repo repository.SearchAnalyticsRepository, workspaces repository.WorkspaceRepository) *SearchAnalyticsService {
	return &SearchAnalyticsService{repo: repo, workspaces: workspaces}
}

func (s *SearchAnalyticsService) Search(workspaceID int64, query model.SearchQuery) (model.SearchPage, error) {
	query.Query = strings.TrimSpace(query.Query)
	query.Status = strings.ToUpper(strings.TrimSpace(query.Status))
	query.Priority = strings.ToUpper(strings.TrimSpace(query.Priority))
	if len(query.Query) > 500 {
		return model.SearchPage{}, ErrInvalidSearchQuery
	}
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Limit > 100 {
		return model.SearchPage{}, ErrInvalidSearchQuery
	}
	if query.Status != "" && !validSearchStatus(query.Status) {
		return model.SearchPage{}, ErrInvalidSearchQuery
	}
	if query.Priority != "" && !validSearchPriority(query.Priority) {
		return model.SearchPage{}, ErrInvalidSearchQuery
	}
	if query.ProjectID != nil && *query.ProjectID <= 0 {
		return model.SearchPage{}, ErrInvalidSearchQuery
	}
	if query.AssigneeID != nil && *query.AssigneeID <= 0 {
		return model.SearchPage{}, ErrInvalidSearchQuery
	}
	return s.repo.SearchTasks(workspaceID, query)
}

func (s *SearchAnalyticsService) CreateSavedView(actorUserID, workspaceID int64, req model.CreateSavedSearchViewRequest) (model.SavedSearchView, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 160 || req.Query == nil {
		return model.SavedSearchView{}, ErrInvalidSavedSearchView
	}
	raw, err := json.Marshal(req.Query)
	if err != nil || len(raw) > 16*1024 {
		return model.SavedSearchView{}, ErrInvalidSavedSearchView
	}
	now := time.Now().UTC()
	item, err := s.repo.CreateSavedView(model.SavedSearchView{
		WorkspaceID: workspaceID, UserID: actorUserID, Name: name,
		Query: req.Query, Shared: req.Shared, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return model.SavedSearchView{}, err
	}
	if err := s.audit(workspaceID, actorUserID, "search.saved_view.created", "saved_search_view", strconv.FormatInt(item.ID, 10), map[string]any{"shared": item.Shared}); err != nil {
		return model.SavedSearchView{}, err
	}
	return item, nil
}

func (s *SearchAnalyticsService) SavedViews(workspaceID, userID int64) ([]model.SavedSearchView, error) {
	return s.repo.ListSavedViews(workspaceID, userID)
}

func (s *SearchAnalyticsService) DeleteSavedView(actorUserID, workspaceID, viewID int64) error {
	if viewID <= 0 {
		return ErrInvalidSavedSearchView
	}
	if err := s.repo.DeleteSavedView(workspaceID, actorUserID, viewID); err != nil {
		return err
	}
	return s.audit(workspaceID, actorUserID, "search.saved_view.deleted", "saved_search_view", strconv.FormatInt(viewID, 10), nil)
}

func (s *SearchAnalyticsService) Dashboard(workspaceID int64, projectID *int64, days int) (model.AnalyticsDashboard, error) {
	if projectID != nil && *projectID <= 0 {
		return model.AnalyticsDashboard{}, ErrInvalidAnalyticsRequest
	}
	if days == 0 {
		days = 30
	}
	if days < 7 || days > 365 {
		return model.AnalyticsDashboard{}, ErrInvalidAnalyticsRequest
	}
	return s.repo.Analytics(workspaceID, projectID, days, time.Now().UTC())
}

func (s *SearchAnalyticsService) Export(workspaceID int64, format string, filters map[string]any) (ReportExportResult, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = model.ReportFormatJSON
	}
	if format != model.ReportFormatJSON && format != model.ReportFormatCSV {
		return ReportExportResult{}, ErrInvalidReportRequest
	}
	if filters == nil {
		filters = map[string]any{}
	}
	raw, err := json.Marshal(filters)
	if err != nil || len(raw) > 16*1024 {
		return ReportExportResult{}, ErrInvalidReportRequest
	}
	if status, ok := filters["status"].(string); ok && strings.TrimSpace(status) != "" && !validSearchStatus(strings.ToUpper(strings.TrimSpace(status))) {
		return ReportExportResult{}, ErrInvalidReportRequest
	}
	if priority, ok := filters["priority"].(string); ok && strings.TrimSpace(priority) != "" && !validSearchPriority(strings.ToUpper(strings.TrimSpace(priority))) {
		return ReportExportResult{}, ErrInvalidReportRequest
	}
	rows, err := s.repo.ExportRows(workspaceID, normalizeReportFilters(filters), 10000)
	if err != nil {
		return ReportExportResult{}, err
	}
	now := time.Now().UTC().Format("20060102T150405Z")
	if format == model.ReportFormatJSON {
		content, err := json.MarshalIndent(map[string]any{"generated_at": time.Now().UTC(), "rows": rows}, "", "  ")
		if err != nil {
			return ReportExportResult{}, err
		}
		return ReportExportResult{
			Content: content, ContentType: "application/json",
			Filename: "task-report-" + now + ".json", RowCount: len(rows),
		}, nil
	}

	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	if err := writer.Write([]string{
		"id", "title", "status", "priority", "project_id", "due_at", "completed_at",
		"estimated_minutes", "actual_minutes", "created_at", "updated_at",
	}); err != nil {
		return ReportExportResult{}, err
	}
	for _, row := range rows {
		record := []string{
			strconv.FormatInt(row.ID, 10),
			row.Title,
			row.Status,
			row.Priority,
			optionalInt64(row.ProjectID),
			optionalTime(row.DueAt),
			optionalTime(row.CompletedAt),
			optionalInt(row.EstimatedMinutes),
			optionalInt(row.ActualMinutes),
			row.CreatedAt.UTC().Format(time.RFC3339),
			row.UpdatedAt.UTC().Format(time.RFC3339),
		}
		if err := writer.Write(record); err != nil {
			return ReportExportResult{}, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return ReportExportResult{}, err
	}
	return ReportExportResult{
		Content: buffer.Bytes(), ContentType: "text/csv; charset=utf-8",
		Filename: "task-report-" + now + ".csv", RowCount: len(rows),
	}, nil
}

func (s *SearchAnalyticsService) CreateScheduledReport(actorUserID, workspaceID int64, req model.CreateScheduledReportRequest) (model.ScheduledReport, error) {
	name := strings.TrimSpace(req.Name)
	format := strings.ToLower(strings.TrimSpace(req.Format))
	frequency := strings.ToLower(strings.TrimSpace(req.Frequency))
	timezone := strings.TrimSpace(req.Timezone)
	if format == "" {
		format = model.ReportFormatJSON
	}
	if frequency == "" {
		frequency = model.ReportFrequencyDaily
	}
	if timezone == "" {
		timezone = "UTC"
	}
	if name == "" || len(name) > 160 ||
		(format != model.ReportFormatJSON && format != model.ReportFormatCSV) ||
		(frequency != model.ReportFrequencyDaily && frequency != model.ReportFrequencyWeekly) ||
		req.Hour < 0 || req.Hour > 23 {
		return model.ScheduledReport{}, ErrInvalidReportRequest
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return model.ScheduledReport{}, ErrInvalidReportRequest
	}
	if req.Filters == nil {
		req.Filters = map[string]any{}
	}
	raw, err := json.Marshal(req.Filters)
	if err != nil || len(raw) > 16*1024 {
		return model.ScheduledReport{}, ErrInvalidReportRequest
	}
	now := time.Now().UTC()
	next := nextReportRun(now, frequency, location, req.Hour)
	if req.RunNow {
		next = now
	}
	item, err := s.repo.CreateScheduledReport(model.ScheduledReport{
		WorkspaceID: workspaceID, UserID: actorUserID, Name: name,
		Format: format, Frequency: frequency, Timezone: timezone, Hour: req.Hour,
		Filters: normalizeReportFilters(req.Filters), Active: true, NextRunAt: next,
		LastStatus: model.ReportRunPending, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return model.ScheduledReport{}, err
	}
	if err := s.audit(workspaceID, actorUserID, "report.schedule.created", "scheduled_report", strconv.FormatInt(item.ID, 10), map[string]any{"frequency": frequency, "format": format}); err != nil {
		return model.ScheduledReport{}, err
	}
	return item, nil
}

func (s *SearchAnalyticsService) ScheduledReports(workspaceID, userID int64) ([]model.ScheduledReport, error) {
	return s.repo.ListScheduledReports(workspaceID, userID)
}

func (s *SearchAnalyticsService) DeleteScheduledReport(actorUserID, workspaceID, reportID int64) error {
	if reportID <= 0 {
		return ErrInvalidReportRequest
	}
	if err := s.repo.DeleteScheduledReport(workspaceID, actorUserID, reportID); err != nil {
		return err
	}
	return s.audit(workspaceID, actorUserID, "report.schedule.deleted", "scheduled_report", strconv.FormatInt(reportID, 10), nil)
}

func (s *SearchAnalyticsService) ReportRuns(workspaceID, userID, limit int64) ([]model.ReportRunSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		return nil, ErrInvalidReportRequest
	}
	return s.repo.ListReportRuns(workspaceID, userID, int(limit))
}

func (s *SearchAnalyticsService) ProcessScheduledReports(limit int) ([]model.ReportRunSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	now := time.Now().UTC()
	schedules, err := s.repo.ClaimDueReports(limit, now)
	if err != nil {
		return nil, err
	}
	results := make([]model.ReportRunSummary, 0, len(schedules))
	for _, schedule := range schedules {
		run := model.ReportRun{
			ScheduleID: schedule.ID, WorkspaceID: schedule.WorkspaceID,
			Status: model.ReportRunRunning, Format: schedule.Format,
			StartedAt: now, CreatedAt: now,
		}
		run, err = s.repo.CreateReportRun(run)
		if err != nil {
			return results, err
		}

		exported, exportErr := s.Export(schedule.WorkspaceID, schedule.Format, schedule.Filters)
		finished := time.Now().UTC()
		run.FinishedAt = &finished
		schedule.LastRunAt = &finished
		schedule.UpdatedAt = finished
		location, locErr := time.LoadLocation(schedule.Timezone)
		if locErr != nil {
			location = time.UTC
		}
		schedule.NextRunAt = nextReportRun(finished, schedule.Frequency, location, schedule.Hour)
		if exportErr != nil {
			run.Status = model.ReportRunFailed
			run.Error = exportErr.Error()
			schedule.LastStatus = model.ReportRunFailed
		} else {
			run.Status = model.ReportRunSucceeded
			run.Payload = exported.Content
			run.RowCount = exported.RowCount
			schedule.LastStatus = model.ReportRunSucceeded
		}
		if _, err := s.repo.CompleteReportRun(run); err != nil {
			return results, err
		}
		if _, err := s.repo.UpdateScheduledReport(schedule); err != nil {
			return results, err
		}
		results = append(results, model.ReportRunSummary{
			ID: run.ID, ScheduleID: run.ScheduleID, WorkspaceID: run.WorkspaceID,
			Status: run.Status, Format: run.Format, RowCount: run.RowCount, Error: run.Error,
			StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, CreatedAt: run.CreatedAt,
		})
	}
	return results, nil
}

func nextReportRun(after time.Time, frequency string, location *time.Location, hour int) time.Time {
	local := after.In(location)
	next := time.Date(local.Year(), local.Month(), local.Day(), hour, 0, 0, 0, location)
	if !next.After(local) {
		next = next.AddDate(0, 0, 1)
	}
	if frequency == model.ReportFrequencyWeekly {
		for next.Weekday() != local.Weekday() {
			next = next.AddDate(0, 0, 1)
		}
		if !next.After(local) {
			next = next.AddDate(0, 0, 7)
		}
	}
	return next.UTC()
}

func normalizeReportFilters(filters map[string]any) map[string]any {
	normalized := make(map[string]any, len(filters))
	for key, value := range filters {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "query":
			if text, ok := value.(string); ok {
				normalized["query"] = strings.TrimSpace(text)
			}
		case "status":
			if text, ok := value.(string); ok {
				normalized["status"] = strings.ToUpper(strings.TrimSpace(text))
			}
		case "priority":
			if text, ok := value.(string); ok {
				normalized["priority"] = strings.ToUpper(strings.TrimSpace(text))
			}
		case "project_id", "assignee_id":
			normalized[strings.ToLower(strings.TrimSpace(key))] = value
		}
	}
	return normalized
}

func validSearchStatus(value string) bool {
	switch value {
	case model.TaskStatusBacklog, model.TaskStatusTodo, model.TaskStatusInProgress,
		model.TaskStatusBlocked, model.TaskStatusInReview, model.TaskStatusDone, model.TaskStatusArchived:
		return true
	default:
		return false
	}
}

func validSearchPriority(value string) bool {
	switch value {
	case model.TaskPriorityLow, model.TaskPriorityMedium, model.TaskPriorityHigh, model.TaskPriorityUrgent:
		return true
	default:
		return false
	}
}

func optionalInt64(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func optionalInt(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func optionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func (s *SearchAnalyticsService) audit(workspaceID, actorUserID int64, action, resourceType, resourceID string, metadata map[string]any) error {
	workspace := workspaceID
	actor := actorUserID
	return s.workspaces.RecordAudit(model.AuditEvent{
		WorkspaceID: &workspace, ActorUserID: &actor, Action: action,
		ResourceType: resourceType, ResourceID: resourceID, Metadata: metadata,
		CreatedAt: time.Now().UTC(),
	})
}

func (r ReportExportResult) String() string {
	return fmt.Sprintf("%s (%d rows)", r.Filename, r.RowCount)
}
