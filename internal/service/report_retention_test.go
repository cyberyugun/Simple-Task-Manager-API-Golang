package service

import (
	"errors"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
	"go-simple-task-api/internal/repository"
)

func TestReportRetentionPolicyFromEnv(t *testing.T) {
	t.Setenv("REPORT_RUN_RETENTION_ENABLED", "true")
	t.Setenv("REPORT_RUN_PAYLOAD_RETENTION_DAYS", "14")
	t.Setenv("REPORT_RUN_DETAIL_RETENTION_DAYS", "180")
	policy, err := NewReportRetentionPolicyFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !policy.Enabled || policy.PayloadRetentionDays != 14 || policy.DetailRetentionDays != 180 {
		t.Fatalf("policy=%+v", policy)
	}

	t.Setenv("REPORT_RUN_PAYLOAD_RETENTION_DAYS", "90")
	t.Setenv("REPORT_RUN_DETAIL_RETENTION_DAYS", "30")
	if _, err := NewReportRetentionPolicyFromEnv(); !errors.Is(err, ErrInvalidReportRetentionPolicy) {
		t.Fatalf("expected invalid retention policy, got %v", err)
	}
}

func TestProcessReportRetentionPurgesAggregatesAndDeletes(t *testing.T) {
	tasks := repository.NewInMemoryTaskRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	access, err := workspaces.Create(41, "Retention", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.NewInMemorySearchAnalyticsRepository(tasks)
	svc := NewSearchAnalyticsService(repo, workspaces)
	if err := svc.SetReportRetentionPolicy(model.ReportRetentionPolicy{
		Enabled: true, PayloadRetentionDays: 7, DetailRetentionDays: 30,
	}); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC)
	schedule, err := repo.CreateScheduledReport(model.ScheduledReport{
		WorkspaceID: access.ID, UserID: 41, Name: "Retention test", Format: model.ReportFormatJSON,
		Frequency: model.ReportFrequencyDaily, Timezone: "UTC", Hour: 8, Active: true,
		NextRunAt: now.Add(24 * time.Hour), LastStatus: model.ReportRunSucceeded,
		CreatedAt: now.AddDate(0, 0, -100), UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	createRun := func(created time.Time, status string, rows int, payload string, duration time.Duration) model.ReportRun {
		finished := created.Add(duration)
		run, err := repo.CreateReportRun(model.ReportRun{
			ScheduleID: schedule.ID, WorkspaceID: access.ID, Status: status, Format: model.ReportFormatJSON,
			RowCount: rows, Payload: []byte(payload), StartedAt: created, FinishedAt: &finished, CreatedAt: created,
		})
		if err != nil {
			t.Fatal(err)
		}
		return run
	}

	oldSucceeded := createRun(now.AddDate(0, 0, -40), model.ReportRunSucceeded, 8, "old-success", 2*time.Second)
	oldFailed := createRun(now.AddDate(0, 0, -40).Add(time.Hour), model.ReportRunFailed, 0, "old-failure", time.Second)
	recent := createRun(now.AddDate(0, 0, -10), model.ReportRunSucceeded, 3, "recent", time.Second)

	result, err := svc.ProcessReportRetention(100, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.PayloadsPurged != 3 || result.RunsAggregated != 2 || result.RunsDeleted != 2 {
		t.Fatalf("result=%+v", result)
	}

	runs, err := repo.ListReportRuns(access.ID, 41, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != recent.ID {
		t.Fatalf("runs=%+v oldSucceeded=%d oldFailed=%d", runs, oldSucceeded.ID, oldFailed.ID)
	}
	if purged, err := repo.PurgeReportRunPayloads(now.AddDate(0, 0, -7), 100, now); err != nil || purged != 0 {
		t.Fatalf("second payload purge=%d err=%v", purged, err)
	}

	aggregates, err := repo.ListReportRunAggregates(access.ID, now.AddDate(0, 0, -365))
	if err != nil {
		t.Fatal(err)
	}
	if len(aggregates) != 2 {
		t.Fatalf("aggregates=%+v", aggregates)
	}
	var succeeded, failed model.ReportRunDailyAggregate
	for _, aggregate := range aggregates {
		switch aggregate.Status {
		case model.ReportRunSucceeded:
			succeeded = aggregate
		case model.ReportRunFailed:
			failed = aggregate
		}
	}
	if succeeded.RunCount != 1 || succeeded.TotalRowCount != 8 || succeeded.PayloadSizeBytes != int64(len("old-success")) || succeeded.DurationMillis != 2000 {
		t.Fatalf("succeeded aggregate=%+v", succeeded)
	}
	if failed.RunCount != 1 || failed.ErrorCount != 1 || failed.PayloadSizeBytes != int64(len("old-failure")) || failed.DurationMillis != 1000 {
		t.Fatalf("failed aggregate=%+v", failed)
	}

	second, err := svc.ProcessReportRetention(100, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if second.PayloadsPurged != 0 || second.RunsAggregated != 0 || second.RunsDeleted != 0 {
		t.Fatalf("second result=%+v", second)
	}
}

func TestProcessReportRetentionDisabled(t *testing.T) {
	tasks := repository.NewInMemoryTaskRepository()
	workspaces := repository.NewInMemoryWorkspaceRepository()
	svc := NewSearchAnalyticsService(repository.NewInMemorySearchAnalyticsRepository(tasks), workspaces)
	result, err := svc.ProcessReportRetention(100, time.Now().UTC())
	if err != nil || result != (model.ReportRetentionResult{}) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
