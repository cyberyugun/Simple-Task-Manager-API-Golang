//go:build integration

package repository_test

import (
	"testing"
	"time"

	appdb "go-simple-task-api/internal/database"
	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestIntegrationPostgresSearchReportingAnalytics(t *testing.T) {
	databaseURL := getenvRequired(t, "TEST_DATABASE_URL")
	db, err := appdb.OpenPostgres(databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgres() error = %v", err)
	}
	if err := resetDatabase(db); err != nil {
		_ = db.Close()
		t.Fatalf("reset database: %v", err)
	}
	t.Cleanup(func() {
		if err := resetDatabase(db); err != nil {
			t.Errorf("cleanup database: %v", err)
		}
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	applyMigrations(t, db)

	users := repository.NewPostgresUserRepository(db)
	workspaces := repository.NewPostgresWorkspaceRepository(db)
	tasks := repository.NewPostgresTaskRepository(db)
	collab := repository.NewPostgresTaskCollaborationRepository(db)
	analytics := repository.NewPostgresSearchAnalyticsRepository(db)
	now := time.Now().UTC().Truncate(time.Microsecond)

	owner, err := users.Create(model.User{
		Name: "Analytics Owner", Email: "analytics-owner@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	member, err := users.Create(model.User{
		Name: "Analytics Member", Email: "analytics-member@example.com", PasswordHash: "hash",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	access, err := workspaces.Create(owner.ID, "Analytics Workspace", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.AddMember(access.ID, member.ID, model.WorkspaceRoleMember, now); err != nil {
		t.Fatal(err)
	}

	project, err := collab.CreateProject(model.TaskProject{
		WorkspaceID: access.ID, Name: "Analytics Project", Description: "Project dashboard scope",
		CreatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	project, err := collab.CreateProject(model.TaskProject{
		WorkspaceID: access.ID, Name: "Analytics Project", Description: "Project metrics",
		CreatedByUserID: owner.ID, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	due := now.Add(-time.Hour)
	first, err := tasks.Create(model.Task{
		WorkspaceID: access.ID, UserID: owner.ID, Title: "Advanced analytics dashboard",
		Description: "search reporting workload", Status: model.TaskStatusInProgress,
		Priority: model.TaskPriorityHigh, ProjectID: &project.ID, DueAt: &due,
		CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	completedAt := now.Add(-time.Hour)
	if _, err := tasks.Create(model.Task{
		WorkspaceID: access.ID, UserID: owner.ID, Title: "Finished export",
		Description: "csv json", Status: model.TaskStatusDone, Completed: true,
		Priority: model.TaskPriorityMedium, CompletedAt: &completedAt,
		CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		"INSERT INTO task_assignees (task_id, user_id, assigned_by_user_id, created_at) VALUES ($1,$2,$3,$4)",
		first.ID, member.ID, owner.ID, now,
	); err != nil {
		t.Fatal(err)
	}

	search, err := analytics.SearchTasks(access.ID, model.SearchQuery{Query: "analytics", Page: 1, Limit: 20})
	if err != nil || search.Pagination.Total != 1 || search.Items[0].Task.ID != first.ID || search.Items[0].Rank <= 0 {
		t.Fatalf("search=%+v err=%v", search, err)
	}

	dashboard, err := analytics.Analytics(access.ID, nil, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Summary.TotalTasks != 2 || dashboard.Summary.CompletedTasks != 1 || dashboard.Summary.OverdueTasks != 1 {
		t.Fatalf("dashboard=%+v", dashboard)
	}
	if len(dashboard.Workload) != 2 || len(dashboard.Trend) != 30 {
		t.Fatalf("workload=%+v trend=%d", dashboard.Workload, len(dashboard.Trend))
	}
	projectDashboard, err := analytics.Analytics(access.ID, &project.ID, 30, now)
	if err != nil || projectDashboard.Summary.TotalTasks != 1 || projectDashboard.ByStatus[model.TaskStatusInProgress] != 1 {
		t.Fatalf("project dashboard=%+v err=%v", projectDashboard, err)
	}
	projectDashboard, err := analytics.Analytics(access.ID, &project.ID, 30, now)
	if err != nil || projectDashboard.Summary.TotalTasks != 1 || projectDashboard.ByStatus[model.TaskStatusInProgress] != 1 {
		t.Fatalf("project dashboard=%+v err=%v", projectDashboard, err)
	}

	view, err := analytics.CreateSavedView(model.SavedSearchView{
		WorkspaceID: access.ID, UserID: owner.ID, Name: "Analytics",
		Query: map[string]any{"query": "analytics"}, Shared: true,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || view.ID == 0 {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	views, err := analytics.ListSavedViews(access.ID, member.ID)
	if err != nil || len(views) != 1 || views[0].ID != view.ID {
		t.Fatalf("views=%+v err=%v", views, err)
	}

	exportRows, err := analytics.ExportRows(access.ID, map[string]any{"priority": "HIGH"}, 100)
	if err != nil || len(exportRows) != 1 || exportRows[0].ID != first.ID {
		t.Fatalf("export=%+v err=%v", exportRows, err)
	}

	schedule, err := analytics.CreateScheduledReport(model.ScheduledReport{
		WorkspaceID: access.ID, UserID: owner.ID, Name: "Daily",
		Format: model.ReportFormatJSON, Frequency: model.ReportFrequencyDaily,
		Timezone: "UTC", Hour: 8, Filters: map[string]any{"priority": "HIGH"},
		Active: true, NextRunAt: now.Add(-time.Minute), LastStatus: model.ReportRunPending,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || schedule.ID == 0 {
		t.Fatalf("schedule=%+v err=%v", schedule, err)
	}
	claimed, err := analytics.ClaimDueReports(10, now)
	if err != nil || len(claimed) != 1 || claimed[0].LastStatus != model.ReportRunRunning {
		t.Fatalf("claimed=%+v err=%v", claimed, err)
	}

	run, err := analytics.CreateReportRun(model.ReportRun{
		ScheduleID: schedule.ID, WorkspaceID: access.ID, Status: model.ReportRunRunning,
		Format: model.ReportFormatJSON, StartedAt: now, CreatedAt: now,
	})
	if err != nil || run.ID == 0 {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	finished := now.Add(time.Second)
	run.Status = model.ReportRunSucceeded
	run.RowCount = 1
	run.Payload = []byte(`{"rows":1}`)
	run.FinishedAt = &finished
	if _, err := analytics.CompleteReportRun(run); err != nil {
		t.Fatal(err)
	}
	schedule.LastRunAt = &finished
	schedule.LastStatus = model.ReportRunSucceeded
	schedule.NextRunAt = now.Add(24 * time.Hour)
	schedule.UpdatedAt = finished
	if _, err := analytics.UpdateScheduledReport(schedule); err != nil {
		t.Fatal(err)
	}
	runs, err := analytics.ListReportRuns(access.ID, owner.ID, 10)
	if err != nil || len(runs) != 1 || runs[0].Status != model.ReportRunSucceeded || runs[0].RowCount != 1 {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
}
