package repository

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrSavedSearchViewNotFound = errors.New("saved search view not found")
	ErrScheduledReportNotFound = errors.New("scheduled report not found")
	ErrReportRunNotFound       = errors.New("report run not found")
)

type SearchAnalyticsRepository interface {
	SearchTasks(workspaceID int64, query model.SearchQuery) (model.SearchPage, error)
	Analytics(workspaceID int64, projectID *int64, days int, now time.Time) (model.AnalyticsDashboard, error)
	ExportRows(workspaceID int64, filters map[string]any, limit int) ([]model.ReportTaskRow, error)

	CreateSavedView(item model.SavedSearchView) (model.SavedSearchView, error)
	ListSavedViews(workspaceID, userID int64) ([]model.SavedSearchView, error)
	DeleteSavedView(workspaceID, userID, viewID int64) error

	CreateScheduledReport(item model.ScheduledReport) (model.ScheduledReport, error)
	ListScheduledReports(workspaceID, userID int64) ([]model.ScheduledReport, error)
	DeleteScheduledReport(workspaceID, userID, reportID int64) error
	ClaimDueReports(limit int, now time.Time) ([]model.ScheduledReport, error)
	UpdateScheduledReport(item model.ScheduledReport) (model.ScheduledReport, error)

	CreateReportRun(item model.ReportRun) (model.ReportRun, error)
	CompleteReportRun(item model.ReportRun) (model.ReportRun, error)
	ListReportRuns(workspaceID, userID int64, limit int) ([]model.ReportRunSummary, error)
}

type InMemorySearchAnalyticsRepository struct {
	mu sync.Mutex

	tasks TaskRepository

	savedViews map[int64]model.SavedSearchView
	schedules  map[int64]model.ScheduledReport
	runs       map[int64]model.ReportRun

	nextViewID     int64
	nextScheduleID int64
	nextRunID      int64
}

func NewInMemorySearchAnalyticsRepository(tasks TaskRepository) *InMemorySearchAnalyticsRepository {
	return &InMemorySearchAnalyticsRepository{
		tasks:          tasks,
		savedViews:     map[int64]model.SavedSearchView{},
		schedules:      map[int64]model.ScheduledReport{},
		runs:           map[int64]model.ReportRun{},
		nextViewID:     1,
		nextScheduleID: 1,
		nextRunID:      1,
	}
}

func (r *InMemorySearchAnalyticsRepository) SearchTasks(workspaceID int64, query model.SearchQuery) (model.SearchPage, error) {
	page, err := r.tasks.FindAll(workspaceID, model.TaskQuery{
		Page: query.Page, Limit: query.Limit, Search: query.Query,
		Status: query.Status, Priority: query.Priority, ProjectID: query.ProjectID,
		AssigneeID: query.AssigneeID, Sort: "updated_at", Order: "desc",
	})
	if err != nil {
		return model.SearchPage{}, err
	}
	hits := make([]model.SearchHit, 0, len(page.Items))
	needle := strings.ToLower(strings.TrimSpace(query.Query))
	for _, task := range page.Items {
		rank := 0.0
		matchType := "filter"
		if needle != "" {
			title := strings.ToLower(task.Title)
			description := strings.ToLower(task.Description)
			if strings.Contains(title, needle) {
				rank += 2
				matchType = "title"
			}
			if strings.Contains(description, needle) {
				rank++
				if matchType == "filter" {
					matchType = "description"
				}
			}
		}
		hits = append(hits, model.SearchHit{Task: task, Rank: rank, Headline: task.Description, MatchType: matchType})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Rank == hits[j].Rank {
			return hits[i].Task.UpdatedAt.After(hits[j].Task.UpdatedAt)
		}
		return hits[i].Rank > hits[j].Rank
	})
	return model.SearchPage{Items: hits, Pagination: page.Pagination}, nil
}

func (r *InMemorySearchAnalyticsRepository) Analytics(workspaceID int64, projectID *int64, days int, now time.Time) (model.AnalyticsDashboard, error) {
	page, err := r.tasks.FindAll(workspaceID, model.TaskQuery{Page: 1, Limit: 100000, ProjectID: projectID, Sort: "created_at", Order: "asc"})
	if err != nil {
		return model.AnalyticsDashboard{}, err
	}
	dashboard := model.AnalyticsDashboard{
		ByStatus: map[string]int64{}, ByPriority: map[string]int64{},
		Workload: []model.WorkloadMetric{}, Trend: []model.TrendMetric{}, Days: days,
	}
	var cycleHours, leadHours float64
	var cycleCount, leadCount int64
	cutoff := now.AddDate(0, 0, -days+1)
	trends := map[string]*model.TrendMetric{}
	for i := 0; i < days; i++ {
		date := cutoff.AddDate(0, 0, i).Format("2006-01-02")
		trends[date] = &model.TrendMetric{Date: date}
	}
	for _, task := range page.Items {
		dashboard.Summary.TotalTasks++
		dashboard.ByStatus[task.Status]++
		dashboard.ByPriority[task.Priority]++
		if task.Status == model.TaskStatusDone || task.Completed {
			dashboard.Summary.CompletedTasks++
		} else {
			dashboard.Summary.OpenTasks++
		}
		if task.DueAt != nil && task.DueAt.Before(now) && task.Status != model.TaskStatusDone && !task.Completed {
			dashboard.Summary.OverdueTasks++
		}
		if task.CompletedAt != nil {
			cycleHours += task.CompletedAt.Sub(task.CreatedAt).Hours()
			cycleCount++
			if task.StartAt != nil {
				leadHours += task.CompletedAt.Sub(*task.StartAt).Hours()
				leadCount++
			}
		}
		if point := trends[task.CreatedAt.Format("2006-01-02")]; point != nil {
			point.Created++
		}
		if task.CompletedAt != nil {
			if point := trends[task.CompletedAt.Format("2006-01-02")]; point != nil {
				point.Completed++
			}
		}
	}
	if dashboard.Summary.TotalTasks > 0 {
		dashboard.Summary.CompletionRate = float64(dashboard.Summary.CompletedTasks) / float64(dashboard.Summary.TotalTasks)
	}
	if cycleCount > 0 {
		dashboard.Summary.AverageCycleHours = cycleHours / float64(cycleCount)
	}
	if leadCount > 0 {
		dashboard.Summary.AverageLeadTimeHours = leadHours / float64(leadCount)
	}
	for i := 0; i < days; i++ {
		date := cutoff.AddDate(0, 0, i).Format("2006-01-02")
		dashboard.Trend = append(dashboard.Trend, *trends[date])
	}
	return dashboard, nil
}

func (r *InMemorySearchAnalyticsRepository) ExportRows(workspaceID int64, filters map[string]any, limit int) ([]model.ReportTaskRow, error) {
	query := model.TaskQuery{Page: 1, Limit: limit, Sort: "created_at", Order: "asc"}
	if value, ok := filters["status"].(string); ok {
		query.Status = value
	}
	if value, ok := filters["priority"].(string); ok {
		query.Priority = value
	}
	if value, ok := filters["query"].(string); ok {
		query.Search = value
	}
	page, err := r.tasks.FindAll(workspaceID, query)
	if err != nil {
		return nil, err
	}
	rows := make([]model.ReportTaskRow, 0, len(page.Items))
	for _, task := range page.Items {
		rows = append(rows, reportTaskRow(task))
	}
	return rows, nil
}

func reportTaskRow(task model.Task) model.ReportTaskRow {
	return model.ReportTaskRow{
		ID: task.ID, Title: task.Title, Status: task.Status, Priority: task.Priority,
		ProjectID: task.ProjectID, DueAt: task.DueAt, CompletedAt: task.CompletedAt,
		EstimatedMinutes: task.EstimatedMinutes, ActualMinutes: task.ActualMinutes,
		CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
	}
}

func (r *InMemorySearchAnalyticsRepository) CreateSavedView(item model.SavedSearchView) (model.SavedSearchView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextViewID
	r.nextViewID++
	item.Query = cloneAnalyticsMap(item.Query)
	r.savedViews[item.ID] = item
	return cloneSavedView(item), nil
}

func (r *InMemorySearchAnalyticsRepository) ListSavedViews(workspaceID, userID int64) ([]model.SavedSearchView, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.SavedSearchView{}
	for _, item := range r.savedViews {
		if item.WorkspaceID == workspaceID && (item.UserID == userID || item.Shared) {
			items = append(items, cloneSavedView(item))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].ID < items[j].ID
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items, nil
}

func (r *InMemorySearchAnalyticsRepository) DeleteSavedView(workspaceID, userID, viewID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.savedViews[viewID]
	if !ok || item.WorkspaceID != workspaceID || item.UserID != userID {
		return ErrSavedSearchViewNotFound
	}
	delete(r.savedViews, viewID)
	return nil
}

func (r *InMemorySearchAnalyticsRepository) CreateScheduledReport(item model.ScheduledReport) (model.ScheduledReport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextScheduleID
	r.nextScheduleID++
	item.Filters = cloneAnalyticsMap(item.Filters)
	r.schedules[item.ID] = item
	return cloneScheduledReport(item), nil
}

func (r *InMemorySearchAnalyticsRepository) ListScheduledReports(workspaceID, userID int64) ([]model.ScheduledReport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.ScheduledReport{}
	for _, item := range r.schedules {
		if item.WorkspaceID == workspaceID && item.UserID == userID {
			items = append(items, cloneScheduledReport(item))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func (r *InMemorySearchAnalyticsRepository) DeleteScheduledReport(workspaceID, userID, reportID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.schedules[reportID]
	if !ok || item.WorkspaceID != workspaceID || item.UserID != userID {
		return ErrScheduledReportNotFound
	}
	delete(r.schedules, reportID)
	return nil
}

func (r *InMemorySearchAnalyticsRepository) ClaimDueReports(limit int, now time.Time) ([]model.ScheduledReport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]int64, 0)
	for id, item := range r.schedules {
		if item.Active && !item.NextRunAt.After(now) && item.LastStatus != model.ReportRunRunning {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) > limit {
		ids = ids[:limit]
	}
	items := make([]model.ScheduledReport, 0, len(ids))
	for _, id := range ids {
		item := r.schedules[id]
		item.LastStatus = model.ReportRunRunning
		item.UpdatedAt = now
		r.schedules[id] = item
		items = append(items, cloneScheduledReport(item))
	}
	return items, nil
}

func (r *InMemorySearchAnalyticsRepository) UpdateScheduledReport(item model.ScheduledReport) (model.ScheduledReport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.schedules[item.ID]; !ok {
		return model.ScheduledReport{}, ErrScheduledReportNotFound
	}
	item.Filters = cloneAnalyticsMap(item.Filters)
	r.schedules[item.ID] = item
	return cloneScheduledReport(item), nil
}

func (r *InMemorySearchAnalyticsRepository) CreateReportRun(item model.ReportRun) (model.ReportRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.ID = r.nextRunID
	r.nextRunID++
	item.Payload = append([]byte(nil), item.Payload...)
	r.runs[item.ID] = item
	return item, nil
}

func (r *InMemorySearchAnalyticsRepository) CompleteReportRun(item model.ReportRun) (model.ReportRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.runs[item.ID]; !ok {
		return model.ReportRun{}, ErrReportRunNotFound
	}
	item.Payload = append([]byte(nil), item.Payload...)
	r.runs[item.ID] = item
	return item, nil
}

func (r *InMemorySearchAnalyticsRepository) ListReportRuns(workspaceID, userID int64, limit int) ([]model.ReportRunSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []model.ReportRunSummary{}
	for _, run := range r.runs {
		schedule, ok := r.schedules[run.ScheduleID]
		if !ok || run.WorkspaceID != workspaceID || schedule.UserID != userID {
			continue
		}
		items = append(items, reportRunSummary(run))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func cloneAnalyticsMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	raw, _ := json.Marshal(value)
	var cloned map[string]any
	_ = json.Unmarshal(raw, &cloned)
	return cloned
}

func cloneSavedView(item model.SavedSearchView) model.SavedSearchView {
	item.Query = cloneAnalyticsMap(item.Query)
	return item
}

func cloneScheduledReport(item model.ScheduledReport) model.ScheduledReport {
	item.Filters = cloneAnalyticsMap(item.Filters)
	return item
}

func reportRunSummary(run model.ReportRun) model.ReportRunSummary {
	return model.ReportRunSummary{
		ID: run.ID, ScheduleID: run.ScheduleID, WorkspaceID: run.WorkspaceID,
		Status: run.Status, Format: run.Format, RowCount: run.RowCount, Error: run.Error,
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, CreatedAt: run.CreatedAt,
	}
}
