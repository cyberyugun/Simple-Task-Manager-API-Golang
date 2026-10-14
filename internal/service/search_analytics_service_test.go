package service

import (
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestSearchAnalyticsSavedViewsDashboardExportAndSchedule(t *testing.T) {
	tasks := repository.NewInMemoryTaskRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	access, err := workspaces.Create(7, "Analytics", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	due := now.Add(-time.Hour)
	completedAt := now.Add(-2 * time.Hour)
	if _, err := tasks.Create(model.Task{
		WorkspaceID: access.ID, UserID: 7, Title: "Ship search analytics",
		Description: "full text dashboard", Status: model.TaskStatusInProgress,
		Priority: model.TaskPriorityHigh, DueAt: &due,
		CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.Create(model.Task{
		WorkspaceID: access.ID, UserID: 7, Title: "Completed reporting",
		Description: "csv export", Status: model.TaskStatusDone, Completed: true,
		Priority: model.TaskPriorityMedium, CompletedAt: &completedAt,
		CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	repo := repository.NewInMemorySearchAnalyticsRepository(tasks)
	svc := NewSearchAnalyticsService(repo, workspaces)

	search, err := svc.Search(access.ID, model.SearchQuery{Query: "analytics", Page: 1, Limit: 20})
	if err != nil || search.Pagination.Total != 1 || search.Items[0].Task.Title != "Ship search analytics" {
		t.Fatalf("search=%+v err=%v", search, err)
	}
	if search.Items[0].Rank <= 0 {
		t.Fatalf("rank=%f", search.Items[0].Rank)
	}

	view, err := svc.CreateSavedView(7, access.ID, model.CreateSavedSearchViewRequest{
		Name: "High Priority", Query: map[string]any{"priority": "HIGH"}, Shared: true,
	})
	if err != nil || view.ID == 0 {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	views, err := svc.SavedViews(access.ID, 7)
	if err != nil || len(views) != 1 {
		t.Fatalf("views=%+v err=%v", views, err)
	}

	dashboard, err := svc.Dashboard(access.ID, 30)
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Summary.TotalTasks != 2 || dashboard.Summary.CompletedTasks != 1 || dashboard.Summary.OverdueTasks != 1 {
		t.Fatalf("dashboard=%+v", dashboard)
	}
	if len(dashboard.Trend) != 30 {
		t.Fatalf("trend points=%d", len(dashboard.Trend))
	}

	jsonExport, err := svc.Export(access.ID, model.ReportFormatJSON, map[string]any{"priority": "HIGH"})
	if err != nil || jsonExport.RowCount != 1 || !strings.Contains(string(jsonExport.Content), "Ship search analytics") {
		t.Fatalf("json export=%+v err=%v", jsonExport, err)
	}
	csvExport, err := svc.Export(access.ID, model.ReportFormatCSV, map[string]any{})
	if err != nil || csvExport.RowCount != 2 || !strings.Contains(string(csvExport.Content), "id,title,status") {
		t.Fatalf("csv export=%+v err=%v", csvExport, err)
	}

	schedule, err := svc.CreateScheduledReport(7, access.ID, model.CreateScheduledReportRequest{
		Name: "Daily Tasks", Format: "json", Frequency: "daily", Timezone: "UTC",
		Hour: time.Now().UTC().Hour(), RunNow: true,
	})
	if err != nil || schedule.ID == 0 || schedule.NextRunAt.After(time.Now().UTC()) {
		t.Fatalf("schedule=%+v err=%v", schedule, err)
	}
	runs, err := svc.ProcessScheduledReports(10)
	if err != nil || len(runs) != 1 || runs[0].Status != model.ReportRunSucceeded || runs[0].RowCount != 2 {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
	listedRuns, err := svc.ReportRuns(access.ID, 7, 10)
	if err != nil || len(listedRuns) != 1 || listedRuns[0].Status != model.ReportRunSucceeded {
		t.Fatalf("listed runs=%+v err=%v", listedRuns, err)
	}
}

func TestSearchAnalyticsValidation(t *testing.T) {
	tasks := repository.NewInMemoryTaskRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	access, _ := workspaces.Create(9, "Validation", time.Now().UTC())
	svc := NewSearchAnalyticsService(repository.NewInMemorySearchAnalyticsRepository(tasks), workspaces)

	if _, err := svc.Search(access.ID, model.SearchQuery{Status: "NOT_A_STATUS", Page: 1, Limit: 20}); err != ErrInvalidSearchQuery {
		t.Fatalf("invalid search error=%v", err)
	}
	if _, err := svc.Dashboard(access.ID, 2); err != ErrInvalidAnalyticsRequest {
		t.Fatalf("invalid dashboard error=%v", err)
	}
	if _, err := svc.Export(access.ID, "xml", nil); err != ErrInvalidReportRequest {
		t.Fatalf("invalid export error=%v", err)
	}
}

func TestSearchAnalyticsSynonymsAndRankingControls(t *testing.T) {
	t.Run("synonym expands recall", func(t *testing.T) {
		tasks := repository.NewInMemoryTaskRepository()
		workspaces := repository.NewInMemoryWorkspaceRepository()
		access, err := workspaces.Create(21, "Synonyms", time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		if _, err := tasks.Create(model.Task{
			WorkspaceID: access.ID, UserID: 21, Title: "Investigate production defect",
			Description: "checkout regression", Status: model.TaskStatusTodo,
			Priority: model.TaskPriorityHigh, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}

		relevance := model.DefaultSearchRelevanceOptions()
		relevance.Synonyms = map[string][]string{"bug": {"defect", "issue"}}
		svc := NewSearchAnalyticsServiceWithRelevance(
			repository.NewInMemorySearchAnalyticsRepository(tasks),
			workspaces,
			relevance,
		)

		result, err := svc.Search(access.ID, model.SearchQuery{Query: "bug", Page: 1, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if result.Pagination.Total != 1 || len(result.Items) != 1 || result.Items[0].Task.Title != "Investigate production defect" {
			t.Fatalf("unexpected synonym result: %+v", result)
		}
		if result.Items[0].MatchType != "synonym" || result.Items[0].Rank <= 0 {
			t.Fatalf("unexpected synonym match metadata: %+v", result.Items[0])
		}
	})

	t.Run("field weights change ordering", func(t *testing.T) {
		tasks := repository.NewInMemoryTaskRepository()
		workspaces := repository.NewInMemoryWorkspaceRepository()
		access, err := workspaces.Create(22, "Ranking", time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		if _, err := tasks.Create(model.Task{
			WorkspaceID: access.ID, UserID: 22, Title: "Signal outage",
			Description: "generic description", Status: model.TaskStatusTodo,
			Priority: model.TaskPriorityMedium, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tasks.Create(model.Task{
			WorkspaceID: access.ID, UserID: 22, Title: "Investigate network",
			Description: "signal degradation detected", Status: model.TaskStatusTodo,
			Priority: model.TaskPriorityMedium, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}

		relevance := model.DefaultSearchRelevanceOptions()
		relevance.TitleWeight = 0.1
		relevance.DescriptionWeight = 2
		svc := NewSearchAnalyticsServiceWithRelevance(
			repository.NewInMemorySearchAnalyticsRepository(tasks),
			workspaces,
			relevance,
		)

		result, err := svc.Search(access.ID, model.SearchQuery{Query: "signal", Page: 1, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Items) != 2 || result.Items[0].Task.Title != "Investigate network" {
			t.Fatalf("weighted order = %+v", result.Items)
		}
	})
}
